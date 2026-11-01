package mediactl

import (
	"net/url"
	"time"
)

// nowPlayingInfo is the platform-neutral payload the macOS backend renders into
// MPNowPlayingInfoCenter. It lives here rather than in the darwin-only file so
// the mapping from a Snapshot is unit-tested on every platform.
type nowPlayingInfo struct {
	Title    string
	Artist   string
	Album    string
	Duration time.Duration
	Elapsed  time.Duration
	Rate     int // 0 while paused, 1 while playing
	ArtPath  string
}

// nowPlayingFor projects a Snapshot onto what the system's now-playing UI shows.
// It reports false when there is no track to advertise, in which case the
// backend clears the info centre instead.
func nowPlayingFor(snap Snapshot) (nowPlayingInfo, bool) {
	if !snap.HasTrack {
		return nowPlayingInfo{}, false
	}
	info := nowPlayingInfo{
		Title:    snap.Title,
		Artist:   snap.Artist,
		Album:    snap.Album,
		Duration: snap.Duration,
		Elapsed:  snap.Position,
		ArtPath:  artPathFromURL(snap.ArtURL),
	}
	// Some tracks arrive without a title; the artist still identifies them.
	if info.Title == "" {
		info.Title = info.Artist
	}
	if snap.Playing {
		info.Rate = 1
	}
	if info.Duration < 0 {
		info.Duration = 0
	}
	if info.Elapsed < 0 {
		info.Elapsed = 0
	}
	if info.Duration > 0 && info.Elapsed > info.Duration {
		info.Elapsed = info.Duration
	}
	return info, true
}

// artPathFromURL turns a file:// URI (as produced by the TUI) into a plain
// filesystem path. Anything that is not a file URI yields "".
func artPathFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" || u.Path == "" {
		return ""
	}
	return u.Path
}

// needsInfoRefresh reports whether a snapshot is different enough from the last
// published one to be worth another trip to the main thread. Position is
// compared at one-second granularity: the system extrapolates the progress bar
// from the playback rate, so publishing every tick would only add bus traffic.
func needsInfoRefresh(prev, cur *Snapshot) bool {
	if prev == nil || cur == nil {
		return true
	}
	if prev.HasTrack != cur.HasTrack || prev.Playing != cur.Playing {
		return true
	}
	if !metadataEqual(prev, cur) {
		return true
	}
	delta := prev.Position - cur.Position
	if delta < 0 {
		delta = -delta
	}
	return delta >= time.Second
}
