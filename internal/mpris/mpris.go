// Package mpris exposes a minimal MPRIS (org.mpris.MediaPlayer2) D-Bus service
// so media keys, playerctl, and desktop media widgets can control ytm.
//
// The service lives on its own goroutine(s) owned by the dbus connection and
// translates remote method calls into Commands pushed to the TUI through the
// send callback. Playback state flows the other way: the TUI calls Update with
// a Snapshot on every tick and the exported properties (and PropertiesChanged
// signals) are refreshed. Registration failures (no session bus, name already
// taken) are reported as errors so the caller can log and carry on — MPRIS is
// always optional and must never crash the player.
package mpris

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

// D-Bus identifiers for the service.
const (
	BusName     = "org.mpris.MediaPlayer2.ytm"
	ObjectPath  = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	rootIface   = "org.mpris.MediaPlayer2"
	playerIface = "org.mpris.MediaPlayer2.Player"

	// NoTrack is the MPRIS sentinel track id used when nothing is playing.
	NoTrack = dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack")
)

// Action identifies a control request coming from the bus.
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

// Command is an MPRIS control request relayed to the TUI.
type Command struct {
	Action   Action
	Offset   time.Duration // Seek: relative offset
	Position time.Duration // SetPosition: absolute position
	TrackID  string        // SetPosition: target track id
	Volume   float64       // SetVolume: 0.0–1.0
	Shuffle  bool          // SetShuffle
	Loop     string        // SetLoop: None / Track / Playlist
}

// Snapshot is the playback state the TUI publishes to MPRIS.
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

// Service is a running MPRIS service. A nil *Service is safe to use: every
// method no-ops, so callers can treat "MPRIS unavailable" uniformly.
type Service struct {
	conn   *dbus.Conn
	owned  bool   // true when the connection is ours to close
	name   string // registered bus name (released on Close)
	props  *prop.Properties
	root   *rootObject
	player *playerObject

	mu   sync.Mutex
	send func(Command)
	last *Snapshot
}

// New connects to the session bus and registers the MPRIS name and object.
// It returns an error (rather than panicking) when no bus is available or the
// name is already owned, letting the caller log and continue.
func New() (*Service, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("mpris: connect session bus: %w", err)
	}
	s, err := NewWithConn(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	s.owned = true
	return s, nil
}

// NewWithConn registers the service on an already-connected bus. The caller
// retains ownership of conn (Close will not close it).
func NewWithConn(conn *dbus.Conn) (*Service, error) {
	return register(conn, BusName)
}

// register performs the actual bus-name acquisition and object export. The
// name is a parameter so tests can register under a unique name.
func register(conn *dbus.Conn, name string) (*Service, error) {
	if conn == nil {
		return nil, fmt.Errorf("mpris: nil connection")
	}
	reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("mpris: request name %s: %w", name, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner && reply != dbus.RequestNameReplyAlreadyOwner {
		return nil, fmt.Errorf("mpris: name %s already owned by another process", name)
	}

	s := &Service{conn: conn, name: name}
	s.root = &rootObject{svc: s}
	s.player = &playerObject{svc: s}

	props, err := prop.Export(conn, ObjectPath, propertyMap(s))
	if err != nil {
		_, _ = conn.ReleaseName(name)
		return nil, fmt.Errorf("mpris: export properties: %w", err)
	}
	s.props = props

	if err := conn.Export(s.root, ObjectPath, rootIface); err != nil {
		_, _ = conn.ReleaseName(name)
		return nil, fmt.Errorf("mpris: export root: %w", err)
	}
	// SeekOffset is exported under its MPRIS name "Seek".
	if err := conn.ExportWithMap(s.player, map[string]string{"SeekOffset": "Seek"}, ObjectPath, playerIface); err != nil {
		_, _ = conn.ReleaseName(name)
		return nil, fmt.Errorf("mpris: export player: %w", err)
	}
	slog.Info("MPRIS registered", "name", name, "path", ObjectPath)
	return s, nil
}

// SetSend installs the callback used to relay control commands to the TUI. It
// may be called after New (once the TUI program exists); commands that arrive
// before it is set are dropped.
func (s *Service) SetSend(send func(Command)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.send = send
	s.mu.Unlock()
}

func (s *Service) dispatch(cmd Command) {
	if s == nil {
		return
	}
	s.mu.Lock()
	send := s.send
	s.mu.Unlock()
	if send != nil {
		send(cmd)
	}
}

// Update publishes a fresh playback snapshot. It only touches properties whose
// value actually changed so PropertiesChanged stays quiet for idle ticks.
func (s *Service) Update(snap Snapshot) {
	if s == nil || s.props == nil {
		return
	}
	s.mu.Lock()
	prev := s.last
	cur := snap
	s.last = &cur
	s.mu.Unlock()

	status := "Stopped"
	if snap.HasTrack {
		if snap.Playing {
			status = "Playing"
		} else {
			status = "Paused"
		}
	}
	loop := "None"
	if snap.Loop {
		loop = "Track"
	}

	if prev == nil || prev.Playing != snap.Playing || prev.HasTrack != snap.HasTrack {
		s.props.SetMust(playerIface, "PlaybackStatus", status)
	}
	if prev == nil || prev.Volume != snap.Volume {
		s.props.SetMust(playerIface, "Volume", clampUnit(snap.Volume))
	}
	if prev == nil || prev.Shuffle != snap.Shuffle {
		s.props.SetMust(playerIface, "Shuffle", snap.Shuffle)
	}
	if prev == nil || prev.Loop != snap.Loop {
		s.props.SetMust(playerIface, "LoopStatus", loop)
	}
	if prev == nil || prev.CanNext != snap.CanNext {
		s.props.SetMust(playerIface, "CanGoNext", snap.CanNext)
	}
	if prev == nil || prev.CanPrev != snap.CanPrev {
		s.props.SetMust(playerIface, "CanGoPrevious", snap.CanPrev)
	}
	if prev == nil || prev.Position != snap.Position {
		s.props.SetMust(playerIface, "Position", int64(snap.Position.Microseconds()))
	}
	if prev == nil || !metadataEqual(prev, &snap) {
		s.props.SetMust(playerIface, "Metadata", metadata(snap))
	}
}

// Close releases the bus name and (when owned) the connection.
func (s *Service) Close() {
	if s == nil {
		return
	}
	if s.conn != nil {
		name := s.name
		if name == "" {
			name = BusName
		}
		if _, err := s.conn.ReleaseName(name); err != nil {
			slog.Debug("mpris: release name failed", "err", err)
		}
		if s.owned {
			if err := s.conn.Close(); err != nil {
				slog.Debug("mpris: close failed", "err", err)
			}
		}
	}
}

// --- MPRIS method objects --------------------------------------------------

type rootObject struct{ svc *Service }

// Raise is a no-op: ytm has no way to raise its terminal window.
func (r *rootObject) Raise() *dbus.Error { return nil }

// Quit asks the TUI to exit.
func (r *rootObject) Quit() *dbus.Error {
	r.svc.dispatch(Command{Action: ActionQuit})
	return nil
}

type playerObject struct{ svc *Service }

func (p *playerObject) Next() *dbus.Error {
	p.svc.dispatch(Command{Action: ActionNext})
	return nil
}

func (p *playerObject) Previous() *dbus.Error {
	p.svc.dispatch(Command{Action: ActionPrevious})
	return nil
}

func (p *playerObject) Pause() *dbus.Error {
	p.svc.dispatch(Command{Action: ActionPause})
	return nil
}

func (p *playerObject) PlayPause() *dbus.Error {
	p.svc.dispatch(Command{Action: ActionPlayPause})
	return nil
}

func (p *playerObject) Stop() *dbus.Error {
	p.svc.dispatch(Command{Action: ActionStop})
	return nil
}

func (p *playerObject) Play() *dbus.Error {
	p.svc.dispatch(Command{Action: ActionPlay})
	return nil
}

// SeekOffset implements MPRIS Player.Seek. It is named SeekOffset (and remapped
// to "Seek" on the bus) so it does not collide with the io.Seeker convention
// that `go vet` checks for.
func (p *playerObject) SeekOffset(offset int64) *dbus.Error {
	p.svc.dispatch(Command{Action: ActionSeek, Offset: time.Duration(offset) * time.Microsecond})
	return nil
}

func (p *playerObject) SetPosition(trackID dbus.ObjectPath, position int64) *dbus.Error {
	p.svc.dispatch(Command{
		Action:   ActionSetPosition,
		TrackID:  string(trackID),
		Position: time.Duration(position) * time.Microsecond,
	})
	return nil
}

func (p *playerObject) OpenUri(uri string) *dbus.Error {
	// ytm only plays tracks from its own queue; ignore external URIs.
	return nil
}

// --- properties ------------------------------------------------------------

func noTrackMetadata() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(NoTrack),
	}
}

func propertyMap(s *Service) prop.Map {
	return prop.Map{
		rootIface: {
			"CanQuit":             {Value: false, Emit: prop.EmitConst},
			"CanRaise":            {Value: false, Emit: prop.EmitConst},
			"HasTrackList":        {Value: false, Emit: prop.EmitConst},
			"Identity":            {Value: "ytm", Emit: prop.EmitConst},
			"DesktopEntry":        {Value: "ytm", Emit: prop.EmitConst},
			"SupportedUriSchemes": {Value: []string{}, Emit: prop.EmitConst},
			"SupportedMimeTypes":  {Value: []string{}, Emit: prop.EmitConst},
		},
		playerIface: {
			"PlaybackStatus": {Value: "Stopped", Emit: prop.EmitTrue},
			"LoopStatus": {
				Value: "None", Writable: true, Emit: prop.EmitTrue,
				Callback: func(c *prop.Change) *dbus.Error {
					if v, ok := c.Value.(string); ok {
						s.dispatch(Command{Action: ActionSetLoop, Loop: v})
					}
					return nil
				},
			},
			"Rate": {Value: 1.0, Writable: true, Emit: prop.EmitTrue,
				Callback: func(c *prop.Change) *dbus.Error { return nil },
			},
			"Shuffle": {
				Value: false, Writable: true, Emit: prop.EmitTrue,
				Callback: func(c *prop.Change) *dbus.Error {
					if v, ok := c.Value.(bool); ok {
						s.dispatch(Command{Action: ActionSetShuffle, Shuffle: v})
					}
					return nil
				},
			},
			"Volume": {
				Value: 1.0, Writable: true, Emit: prop.EmitTrue,
				Callback: func(c *prop.Change) *dbus.Error {
					if v, ok := c.Value.(float64); ok {
						s.dispatch(Command{Action: ActionSetVolume, Volume: v})
					}
					return nil
				},
			},
			"Metadata": {Value: noTrackMetadata(), Emit: prop.EmitTrue},
			// Position has no change signal (it would fire every tick); clients
			// read it on demand via Get.
			"Position":      {Value: int64(0), Emit: prop.EmitFalse},
			"MinimumRate":   {Value: 1.0, Emit: prop.EmitConst},
			"MaximumRate":   {Value: 1.0, Emit: prop.EmitConst},
			"CanGoNext":     {Value: false, Emit: prop.EmitTrue},
			"CanGoPrevious": {Value: false, Emit: prop.EmitTrue},
			"CanPlay":       {Value: true, Emit: prop.EmitConst},
			"CanPause":      {Value: true, Emit: prop.EmitConst},
			"CanSeek":       {Value: true, Emit: prop.EmitConst},
			"CanControl":    {Value: true, Emit: prop.EmitConst},
		},
	}
}

// metadata builds the MPRIS Metadata map. The title is "Title - Artist" to
// match how the track is displayed everywhere else in ytm.
func metadata(snap Snapshot) map[string]dbus.Variant {
	if !snap.HasTrack {
		return noTrackMetadata()
	}
	m := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(trackPath(snap.VideoID)),
	}
	if snap.Duration > 0 {
		m["mpris:length"] = dbus.MakeVariant(int64(snap.Duration.Microseconds()))
	}
	title := snap.Title
	if snap.Artist != "" {
		if title != "" {
			title = snap.Title + " - " + snap.Artist
		} else {
			title = snap.Artist
		}
	}
	if title != "" {
		m["xesam:title"] = dbus.MakeVariant(title)
	}
	if snap.Artist != "" {
		m["xesam:artist"] = dbus.MakeVariant([]string{snap.Artist})
	}
	if snap.Album != "" {
		m["xesam:album"] = dbus.MakeVariant(snap.Album)
	}
	if snap.ArtURL != "" {
		m["mpris:artUrl"] = dbus.MakeVariant(snap.ArtURL)
	}
	return m
}

// trackPath turns a videoID into a valid MPRIS track object path. D-Bus object
// path elements may only contain [A-Za-z0-9_], so anything else is replaced.
func trackPath(videoID string) dbus.ObjectPath {
	if videoID == "" {
		return NoTrack
	}
	var b strings.Builder
	for _, r := range videoID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return dbus.ObjectPath("/org/mpris/MediaPlayer2/Track/" + b.String())
}

func metadataEqual(a, b *Snapshot) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.VideoID == b.VideoID && a.Title == b.Title &&
		a.Artist == b.Artist && a.Album == b.Album && a.ArtURL == b.ArtURL &&
		a.Duration == b.Duration
}

func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
