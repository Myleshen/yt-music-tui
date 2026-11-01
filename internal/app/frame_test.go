package app

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Myleshen/yt-music-tui/internal/player"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// frameTracks builds a queue whose titles are long enough to overflow a column,
// mixing emoji (two cells in most terminals, one in Go's width tables) with
// Tamil (combining marks that occupy no cells on their own).
func frameTracks(n int) []ytmusic.Track {
	out := make([]ytmusic.Track, n)
	for i := range out {
		out[i] = ytmusic.Track{
			VideoID:  fmt.Sprintf("v%d", i),
			Title:    "🎵 யென்னை இழுக்குதடி — Yennai Izhukkuthadi (Reprise / Extended Club Mix)",
			Artist:   "A. R. Rahman, Dhee, Shreya Ghoshal",
			Album:    "Kadhalikka Neramillai (Original Motion Picture Soundtrack)",
			Duration: "3:48",
		}
	}
	return out
}

func frameModel(w, h int, lyrics, synced bool, tracks int) Model {
	m := testModel()
	m.width, m.height = w, h
	m.mpv = &player.MPV{}
	m.playerState = player.State{Position: 72, Duration: 228}
	m.tab = TabNowPlaying
	m.showLyrics = lyrics
	m.lyricsSynced = synced
	if tracks > 0 {
		m.queue = player.NewQueue(frameTracks(tracks), 0)
	}
	if lyrics {
		for i := 0; i < 60; i++ {
			m.lyrics = append(m.lyrics, lyricLine{time: float64(i) * 3, text: "யென்னை இழுக்குதடி மீன் விழி மீன் விழி மீன் விழி ஓ ஓ ஓ ஓ"})
		}
	}
	m.artCache = map[string]string{}
	// Every list New() sets up is resized on a WindowSizeMsg; the zero value
	// has a nil delegate and panics, so the test model must build them all.
	m.pageList = newList("")
	m.pickList = newList("")
	return m
}

// TestViewFitsTerminal is the regression test for the "jumbled screen" bug: a
// frame taller or wider than the terminal makes the terminal scroll, which
// desynchronises bubbletea's line-based repainting into a permanent mess.
func TestViewFitsTerminal(t *testing.T) {
	sizes := [][2]int{
		{40, 10}, {60, 18}, {80, 24}, {90, 24}, {100, 30},
		{109, 40}, {110, 40}, {120, 45}, {160, 50}, {200, 60},
	}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		for _, lyrics := range []bool{false, true} {
			for _, synced := range []bool{false, true} {
				for _, tracks := range []int{0, 1, 40} {
					name := fmt.Sprintf("%dx%d_lyrics=%v_synced=%v_tracks=%d", w, h, lyrics, synced, tracks)
					m := frameModel(w, h, lyrics, synced, tracks)
					checkFrame(t, name, m.View(), w, h)
				}
			}
		}
	}
}

func TestViewFitsTerminalOtherTabs(t *testing.T) {
	const w, h = 100, 30
	base := frameModel(w, h, false, false, 20)
	for _, tab := range []Tab{TabHome, TabSearch, TabLibrary, TabPlaylists, TabStats} {
		base.tab = tab
		checkFrame(t, "tab="+string(rune('0'+int(tab))), base.View(), w, h)
	}
}

func checkFrame(t *testing.T, name, view string, w, h int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Errorf("%s: frame has %d lines, want exactly %d", name, len(lines), h)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > w {
			t.Errorf("%s: line %d is %d cells wide, want <= %d: %q", name, i, got, w, line)
		}
	}
}

func TestFitFrameTruncatesAndPads(t *testing.T) {
	got := fitFrame("aa\nbbbb\ncc", 3, 4)
	want := "aa\nbbb\ncc\n"
	if got != want {
		t.Errorf("fitFrame = %q, want %q", got, want)
	}
	if n := strings.Count(fitFrame("a\nb\nc", 5, 2), "\n"); n != 1 {
		t.Errorf("fitFrame should drop lines past the height, got %d newlines", n)
	}
}

func TestClipCountsDisplayCells(t *testing.T) {
	// A CJK string is 8 cells but 4 runes: clipping to 5 cells must not leave
	// 5 runes (10 cells) on the line.
	if got := ansi.StringWidth(clip("漢字漢字", 5)); got > 5 {
		t.Errorf("clip left %d cells, want <= 5", got)
	}
	if got := clip("abcdef", 4); got != "abc…" {
		t.Errorf("clip = %q, want abc…", got)
	}
	if got := clip("abc", 3); got != "abc" {
		t.Errorf("clip should not touch a string that fits, got %q", got)
	}
}

func TestPadBlockDropsLinesAndClampsWidth(t *testing.T) {
	got := padBlock("aaaa\na\nBBBB\nBBBB", 2, 2)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("padBlock returned %d lines, want 2", len(lines))
	}
	for _, line := range lines {
		if ansi.StringWidth(line) != 2 {
			t.Errorf("line %q is %d cells, want 2", line, ansi.StringWidth(line))
		}
	}
}

func TestNowPlayingBodyNeverExceedsHeight(t *testing.T) {
	// Tall art on a short terminal is what overflowed the frame (and scrolled
	// the terminal) before the art was gated on the available height.
	for _, sz := range [][2]int{{160, 20}, {160, 24}, {160, 30}, {160, 50}, {120, 20}, {100, 18}, {200, 60}} {
		m := frameModel(sz[0], sz[1], true, true, 10)
		cur, _ := m.queue.Current()
		m.artCache[artKey(cur.VideoID, sz[0])] = strings.Repeat("███\n", 20)
		if got := lipgloss.Height(m.nowPlayingView(sz[1])); got > sz[1] {
			t.Errorf("%dx%d: now playing body is %d lines, want <= %d", sz[0], sz[1], got, sz[1])
		}
	}
}

func TestWindowSizeMsgRequestsRepaint(t *testing.T) {
	// No queue, so the only possible command is the repaint itself (a queued
	// track would also schedule an album-art render).
	m := frameModel(100, 30, false, false, 0)
	got, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd == nil {
		t.Fatal("a size change must clear the screen before the next frame")
	}
	if mm := got.(Model); mm.width != 120 || mm.height != 40 {
		t.Errorf("size not applied: %dx%d", mm.width, mm.height)
	}
	// An unchanged size must not force a repaint (it would flash on every
	// stray SIGWINCH).
	m2 := frameModel(120, 40, false, false, 0)
	if _, cmd := m2.Update(tea.WindowSizeMsg{Width: 120, Height: 40}); cmd != nil {
		t.Error("an identical size should not trigger a clear")
	}
}

func TestPanelsFitTheirHeightBudget(t *testing.T) {
	m := frameModel(160, 45, true, true, 40) // a long queue and long lyrics
	for _, h := range []int{3, 4, 5, 8, 10, 12, 20, 33} {
		if got := lipgloss.Height(m.upNextPanel(40, h)); got > h {
			t.Errorf("upNextPanel(%d) = %d lines, want <= %d", h, got, h)
		}
		if got := lipgloss.Height(m.lyricsPanel(40, h)); got > h {
			t.Errorf("lyricsPanel(%d) = %d lines, want <= %d", h, got, h)
		}
	}
}
