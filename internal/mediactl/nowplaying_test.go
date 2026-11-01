package mediactl

import (
	"testing"
	"time"
)

func TestNowPlayingFor(t *testing.T) {
	if _, ok := nowPlayingFor(Snapshot{}); ok {
		t.Fatal("a snapshot without a track must not advertise now-playing info")
	}

	info, ok := nowPlayingFor(Snapshot{
		HasTrack: true,
		Title:    "Song",
		Artist:   "Artist",
		Album:    "Album",
		ArtURL:   "file:///home/u/.config/ytmusic-tui/art/v2/a.jpg",
		Playing:  true,
		Position: 42 * time.Second,
		Duration: 3 * time.Minute,
	})
	if !ok {
		t.Fatal("a snapshot with a track must advertise now-playing info")
	}
	if info.Title != "Song" || info.Artist != "Artist" || info.Album != "Album" {
		t.Errorf("unexpected metadata: %+v", info)
	}
	if info.Rate != 1 {
		t.Errorf("Rate = %d, want 1 while playing", info.Rate)
	}
	if info.ArtPath != "/home/u/.config/ytmusic-tui/art/v2/a.jpg" {
		t.Errorf("ArtPath = %q", info.ArtPath)
	}

	// Paused tracks keep a zero rate.
	info, _ = nowPlayingFor(Snapshot{HasTrack: true, Title: "T"})
	if info.Rate != 0 {
		t.Errorf("Rate = %d, want 0 while paused", info.Rate)
	}

	// A missing title falls back to the artist.
	info, _ = nowPlayingFor(Snapshot{HasTrack: true, Artist: "Only Artist"})
	if info.Title != "Only Artist" {
		t.Errorf("Title = %q, want the artist as fallback", info.Title)
	}

	// Elapsed is bounded by the duration.
	info, _ = nowPlayingFor(Snapshot{HasTrack: true, Duration: time.Minute, Position: 2 * time.Minute})
	if info.Elapsed != time.Minute {
		t.Errorf("Elapsed = %v, want it clamped to the duration", info.Elapsed)
	}
}

func TestArtPathFromURL(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"file:///home/u/art.jpg", "/home/u/art.jpg"},
		{"file:///home/u/a%20b.jpg", "/home/u/a b.jpg"},
		{"https://example.com/a.jpg", ""},
		{"not a url", ""},
	} {
		if got := artPathFromURL(c.in); got != c.want {
			t.Errorf("artPathFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNeedsInfoRefresh(t *testing.T) {
	base := Snapshot{HasTrack: true, Title: "T", VideoID: "v", Duration: time.Minute, Position: 10 * time.Second}
	if !needsInfoRefresh(nil, &base) {
		t.Error("the first snapshot should always be published")
	}
	if !needsInfoRefresh(&base, nil) {
		t.Error("a nil snapshot should always be published")
	}
	// Sub-second position drift is not worth a round trip to the main thread.
	cur := base
	cur.Position = 10*time.Second + 400*time.Millisecond
	if needsInfoRefresh(&base, &cur) {
		t.Error("sub-second position drift should not trigger a refresh")
	}
	cur.Position = 12 * time.Second
	if !needsInfoRefresh(&base, &cur) {
		t.Error("a position jump of >=1s should trigger a refresh")
	}
	cur = base
	cur.Playing = true
	if !needsInfoRefresh(&base, &cur) {
		t.Error("a play/pause change should trigger a refresh")
	}
	cur = base
	cur.Title = "Other"
	if !needsInfoRefresh(&base, &cur) {
		t.Error("a track change should trigger a refresh")
	}
}
