// Package mediactl exposes the operating system's "now playing" surface to the
// TUI and relays remote control requests back to it.
//
// The backend is selected at build time:
//
//   - Linux and the BSDs use MPRIS (org.mpris.MediaPlayer2) over the D-Bus
//     session bus, which is what media keys, playerctl and desktop widgets
//     speak.
//   - macOS uses MediaPlayer's MPNowPlayingInfoCenter and
//     MPRemoteCommandCenter, reached through the Objective-C runtime, which is
//     what Control Center, the media keys and Bluetooth/AirPods controls use.
//   - any other platform gets a stub that reports the feature as unavailable.
//
// Every backend is optional. New returns an error (never panics) when the
// platform support is missing, so the caller can log it and run without.
package mediactl

import "time"

// Action identifies a control request coming from the OS.
type Action int

const (
	ActionPlay Action = iota
	ActionPause
	ActionPlayPause
	ActionStop
	ActionNext
	ActionPrevious
	ActionSeek
	ActionSetPosition
	ActionSetVolume
	ActionSetShuffle
	ActionSetLoop
	ActionQuit
)

// Command is a remote control request relayed to the TUI.
type Command struct {
	Action   Action
	Offset   time.Duration // Seek: relative offset
	Position time.Duration // SetPosition: absolute position
	TrackID  string        // SetPosition: target track id
	Volume   float64       // SetVolume: 0.0–1.0
	Shuffle  bool          // SetShuffle
	Loop     string        // SetLoop: None / Track / Playlist
}

// Snapshot is the playback state the TUI publishes to the OS.
type Snapshot struct {
	HasTrack bool
	VideoID  string
	Title    string
	Artist   string
	Album    string
	ArtURL   string // file:// URI for cover art; empty when unavailable
	Playing  bool
	Position time.Duration
	Duration time.Duration
	Volume   float64 // 0.0–1.0
	Shuffle  bool
	Loop     bool
	CanNext  bool
	CanPrev  bool
}

// Service is a running "now playing" backend. Implementations must be safe for
// concurrent use: Update and SetSend are called from the TUI goroutine while
// commands arrive from the OS on whichever goroutine the platform uses.
//
// A nil Service is not usable as an interface value, so callers that may not
// have a backend must check for nil before calling.
type Service interface {
	// SetSend installs the callback used to relay control commands to the TUI.
	// Commands that arrive before it is set are dropped.
	SetSend(send func(Command))
	// Update publishes a fresh playback snapshot.
	Update(Snapshot)
	// Close releases the OS resources held by the backend.
	Close()
}

// Runner is implemented by backends that need to own the process main thread.
//
// AppKit (and therefore MPRemoteCommandCenter, and therefore the media keys)
// only delivers events on the main run loop, so the macOS backend has to take
// over the main goroutine. Callers should type-assert a Service to Runner and,
// when it matches, run the TUI on another goroutine and call RunLoop from the
// main one; StopLoop must be called once the TUI has finished so RunLoop can
// return.
type Runner interface {
	// RunLoop blocks the calling (main) goroutine, servicing OS events, until
	// StopLoop is called.
	RunLoop()
	// StopLoop asks RunLoop to return. It is safe to call from any goroutine
	// and is idempotent.
	StopLoop()
}

// clampUnit bounds a 0.0–1.0 volume.
func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// metadataEqual reports whether two snapshots describe the same track, so
// backends can skip republishing unchanged metadata.
func metadataEqual(a, b *Snapshot) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.VideoID == b.VideoID && a.Title == b.Title &&
		a.Artist == b.Artist && a.Album == b.Album && a.ArtURL == b.ArtURL &&
		a.Duration == b.Duration
}
