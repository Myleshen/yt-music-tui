package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/Myleshen/yt-music-tui/internal/player"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

func mkTracks(n int) []ytmusic.Track {
	tracks := make([]ytmusic.Track, n)
	for i := range tracks {
		tracks[i] = ytmusic.Track{VideoID: "v" + string(rune('A'+i)), Title: "T"}
	}
	return tracks
}

func TestMoveUpNext_Clamps(t *testing.T) {
	m := Model{queue: player.NewQueue(mkTracks(5), 1)} // upcoming = 3

	// Up at the top is a no-op.
	m.moveUpNext(false)
	if m.upNextCursor != 0 {
		t.Fatalf("cursor moved above 0: %d", m.upNextCursor)
	}

	// Down moves one at a time and stops at the last upcoming track.
	m.moveUpNext(true)
	m.moveUpNext(true)
	if m.upNextCursor != 2 {
		t.Fatalf("expected cursor 2, got %d", m.upNextCursor)
	}
	m.moveUpNext(true)
	if m.upNextCursor != 2 {
		t.Fatalf("cursor should clamp at last upcoming, got %d", m.upNextCursor)
	}

	// Back up.
	m.moveUpNext(false)
	if m.upNextCursor != 1 {
		t.Fatalf("expected cursor 1, got %d", m.upNextCursor)
	}
}

func TestMoveUpNext_NoUpcoming(t *testing.T) {
	m := Model{queue: player.NewQueue(mkTracks(3), 2)} // last track
	m.moveUpNext(true)
	if m.upNextCursor != 0 {
		t.Fatalf("cursor should stay 0 with nothing upcoming, got %d", m.upNextCursor)
	}
}

func TestUpNextTargetIndex(t *testing.T) {
	m := Model{queue: player.NewQueue(mkTracks(5), 1)}

	// cursor 0 -> first upcoming (absolute index 2).
	if idx, ok := m.upNextTargetIndex(); !ok || idx != 2 {
		t.Fatalf("target = (%d,%v), want (2,true)", idx, ok)
	}
	m.moveUpNext(true)
	if idx, ok := m.upNextTargetIndex(); !ok || idx != 3 {
		t.Fatalf("target = (%d,%v), want (3,true)", idx, ok)
	}
	m.moveUpNext(true)
	if idx, ok := m.upNextTargetIndex(); !ok || idx != 4 {
		t.Fatalf("target = (%d,%v), want (4,true)", idx, ok)
	}

	// At the end of the queue there is no target.
	m = Model{queue: player.NewQueue(mkTracks(3), 2)}
	if _, ok := m.upNextTargetIndex(); ok {
		t.Error("expected no Up Next target at end of queue")
	}

	// No queue at all.
	if _, ok := (Model{}).upNextTargetIndex(); ok {
		t.Error("expected no Up Next target without a queue")
	}
}

// newNowPlayingModel builds a wide Now Playing model with art, lyrics and a
// multi-track queue so the layout can be asserted.
func newNowPlayingModel(width int, showLyrics bool) Model {
	tracks := makeTracks()
	tracks[0].Title = "Current Song"
	tracks[0].Artist = "Current Artist"
	return Model{
		width:       width,
		height:      44,
		tab:         TabNowPlaying,
		mpv:         &player.MPV{}, // non-nil sentinel: a track is "playing"
		queue:       player.NewQueue(tracks, 0),
		showLyrics:  showLyrics,
		lyrics:      []lyricLine{{time: -1, text: "first lyric line"}, {time: -1, text: "second lyric line"}},
		playerState: player.State{Playing: true, Position: 5, Duration: 200, Volume: 50},
		artCache: map[string]string{
			artKey(tracks[0].VideoID, width): "ART_BLOCK",
		},
	}
}

func makeTracks() []ytmusic.Track {
	return []ytmusic.Track{
		{VideoID: "v0", Title: "Current Song", Artist: "Current Artist", Album: "Album"},
		{VideoID: "v1", Title: "Next One", Artist: "A1"},
		{VideoID: "v2", Title: "Next Two", Artist: "A2"},
		{VideoID: "v3", Title: "Next Three", Artist: "A3"},
	}
}

func TestNowPlayingView_WideShowsAllPanels(t *testing.T) {
	m := newNowPlayingModel(upNextMinWidth, true)
	out := m.nowPlayingView(44)

	for _, want := range []string{"ART_BLOCK", "Current Song", "Current Artist", "Up Next", "Lyrics", "first lyric line"} {
		if !strings.Contains(out, want) {
			t.Errorf("wide Now Playing missing %q\n%s", want, out)
		}
	}
	// Up Next marks the current track and lists upcoming ones.
	if !strings.Contains(out, "Next One") {
		t.Errorf("wide Now Playing missing upcoming track\n%s", out)
	}
	// Nothing may overflow the terminal width (catches a mis-sized column,
	// e.g. when the art width is not subtracted from the info column).
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("line exceeds terminal width (%d > %d): %q", w, m.width, line)
		}
	}
}

func TestNowPlayingView_WideLyricsHiddenKeepsUpNext(t *testing.T) {
	m := newNowPlayingModel(120, false)
	out := m.nowPlayingView(44)

	if strings.Contains(out, "♪  Lyrics") {
		t.Errorf("lyrics panel should be hidden when showLyrics=false\n%s", out)
	}
	if !strings.Contains(out, "Up Next") {
		t.Errorf("Up Next must remain visible when lyrics are hidden\n%s", out)
	}
}

func TestNowPlayingView_NarrowPrioritisesUpNext(t *testing.T) {
	m := newNowPlayingModel(100, false) // below upNextMinWidth
	out := m.nowPlayingView(44)

	if !strings.Contains(out, "Up Next") {
		t.Errorf("narrow Now Playing must show Up Next\n%s", out)
	}
	if strings.Contains(out, "ART_BLOCK") {
		t.Errorf("narrow Now Playing should not render art\n%s", out)
	}
	// Lyrics are still toggleable on narrow terminals.
	m.showLyrics = true
	out = m.nowPlayingView(44)
	if !strings.Contains(out, "♪  Lyrics") {
		t.Errorf("narrow Now Playing must show lyrics when toggled on\n%s", out)
	}
}
