package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Myleshen/yt-music-tui/internal/player"
)

// testModel builds a Model with the components New() would set up, so key
// dispatch (which forwards to the active list) has something to update.
func testModel() Model {
	return Model{
		keys:         DefaultKeyMap,
		tab:          TabNowPlaying,
		searchInput:  textinput.New(),
		searchList:   newList(""),
		libraryList:  newList("Liked Songs"),
		playlistList: newList("Playlists"),
		trackList:    newList(""),
		paletteInput: newPaletteInput(),
		lyricsCache:  map[string]Lyrics{},
	}
}

func TestFuzzyMatch(t *testing.T) {
	if _, ok := fuzzyMatch("", "anything"); !ok {
		t.Error("empty pattern should match everything")
	}
	if _, ok := fuzzyMatch("nx", "next"); !ok {
		t.Error("subsequence 'nx' should match 'next'")
	}
	if _, ok := fuzzyMatch("zz", "next"); ok {
		t.Error("'zz' should not match 'next'")
	}
	if _, ok := fuzzyMatch("NEXT", "next"); !ok {
		t.Error("matching should be case-insensitive")
	}
	// Subsequence matching used by type-to-filter/jump in lists.
	if _, ok := fuzzyMatch("abc", "a-b-c"); !ok {
		t.Error("'abc' should match 'a-b-c'")
	}
	if _, ok := fuzzyMatch("hey", "Hey Jude"); !ok {
		t.Error("'hey' should match 'Hey Jude'")
	}
	if _, ok := fuzzyMatch("xyz", "jazz"); ok {
		t.Error("'xyz' should not match 'jazz'")
	}
	// A contiguous match at a word start scores higher than a scattered one.
	contig, _ := fuzzyMatch("next", "next track")
	scatter, _ := fuzzyMatch("next", "never extra time")
	if contig <= scatter {
		t.Errorf("contiguous score %d should beat scattered %d", contig, scatter)
	}
}

func TestFilterActions(t *testing.T) {
	m := Model{keys: DefaultKeyMap}
	all := m.paletteActions()
	if len(all) == 0 {
		t.Fatal("expected a non-empty action list")
	}
	// An empty filter returns everything.
	if got := filterActions(all, ""); len(got) != len(all) {
		t.Errorf("empty filter returned %d, want %d", len(got), len(all))
	}
	got := filterActions(all, "shuffle")
	if len(got) == 0 || got[0].name != "Toggle shuffle" {
		t.Fatalf("shuffle filter = %+v", got)
	}
	got = filterActions(all, "zzzznope")
	if len(got) != 0 {
		t.Errorf("nonsense filter matched %+v", got)
	}
}

func TestKeyMsgFrom(t *testing.T) {
	if km, ok := keyMsgFrom("n"); !ok || km.String() != "n" {
		t.Errorf("keyMsgFrom(n) = (%v,%v)", km, ok)
	}
	if km, ok := keyMsgFrom(" "); !ok || km.String() != " " {
		t.Errorf("keyMsgFrom(space) = (%v,%v)", km, ok)
	}
	if km, ok := keyMsgFrom("up"); !ok || km.Type != tea.KeyUp {
		t.Errorf("keyMsgFrom(up) = (%v,%v)", km, ok)
	}
	if _, ok := keyMsgFrom("ctrl+k"); ok {
		t.Error("multi-char named key should not be dispatchable directly")
	}
}

func TestWindowRange(t *testing.T) {
	if s, e := windowRange(0, 3, 5); s != 0 || e != 3 {
		t.Errorf("all-visible = (%d,%d), want (0,3)", s, e)
	}
	if s, e := windowRange(10, 20, 5); e-s != 5 || !(s <= 10 && 10 < e) {
		t.Errorf("cursor must stay in window: (%d,%d)", s, e)
	}
	if s, _ := windowRange(0, 20, 5); s != 0 {
		t.Errorf("top window start = %d, want 0", s)
	}
	if _, e := windowRange(19, 20, 5); e != 20 {
		t.Errorf("bottom window end = %d, want 20", e)
	}
}

func TestPaletteFilterAndRun(t *testing.T) {
	m := testModel()
	m.openPalette()
	if !m.paletteOpen || len(m.paletteMatches) == 0 {
		t.Fatal("openPalette should populate matches")
	}
	for _, r := range "library" {
		updated, _ := m.updatePalette(keyMsg(string(r)), nil)
		m = updated.(Model)
	}
	if len(m.paletteMatches) == 0 || m.paletteMatches[0].name != "Go to Library" {
		t.Fatalf("filtered matches = %+v", m.paletteMatches)
	}
	updated, _ := m.updatePalette(keyMsg("enter"), nil)
	m = updated.(Model)
	if m.paletteOpen {
		t.Error("palette should close after running a command")
	}
	if m.tab != TabLibrary {
		t.Errorf("tab = %v, want TabLibrary", m.tab)
	}
}

func TestHelpFilterAndRun(t *testing.T) {
	m := testModel()
	m.openHelp()
	for _, r := range "shuffle" {
		updated, _ := m.updateHelp(keyMsg(string(r)), nil)
		m = updated.(Model)
	}
	if len(m.helpMatches) == 0 || m.helpMatches[0].name != "Toggle shuffle" {
		t.Fatalf("filtered help = %+v", m.helpMatches)
	}
	updated, _ := m.updateHelp(keyMsg("enter"), nil)
	m = updated.(Model)
	if m.showHelp {
		t.Error("help should close after running a command")
	}
	if m.helpFilter != "" {
		t.Errorf("filter should reset on close, got %q", m.helpFilter)
	}
}

func TestSearchOptions_HistoryVsSuggestions(t *testing.T) {
	m := testModel()
	m.searchHistory = []string{"radiohead", "bjork"}
	var cmds []tea.Cmd

	m.searchInput.SetValue("")
	m.refreshSearchOptions(&cmds)
	if !m.searchHistMode || len(m.searchOptions) != 2 {
		t.Fatalf("empty box should show history, got histMode=%v options=%v", m.searchHistMode, m.searchOptions)
	}
	if len(cmds) != 0 {
		t.Errorf("empty box should not schedule a suggestions fetch, got %d cmds", len(cmds))
	}

	m.searchInput.SetValue("radio")
	m.refreshSearchOptions(&cmds)
	if m.searchHistMode {
		t.Error("non-empty box should switch off history mode")
	}
	if len(cmds) == 0 {
		t.Error("typing should schedule a debounced suggestions fetch")
	}
	if m.searchCursor != -1 {
		t.Errorf("cursor should reset to -1 on typing, got %d", m.searchCursor)
	}
}

func TestStartSearch_RecordsHistory(t *testing.T) {
	m := testModel()
	m.searchHistory = []string{"old"}
	updated, _ := m.startSearch("  New Query  ", nil)
	m = updated.(Model)
	if m.searchMode {
		t.Error("startSearch should leave search-entry mode")
	}
	if len(m.searchHistory) == 0 || m.searchHistory[0] != "New Query" {
		t.Errorf("history = %v, want the trimmed query first", m.searchHistory)
	}
}

func TestLyricsPanel_SyncedHighlight(t *testing.T) {
	m := Model{
		width: 120, height: 44, tab: TabNowPlaying, mpv: &player.MPV{},
		showLyrics: true, lyricsSynced: true, lyricsFollow: true,
		lyrics: []lyricLine{
			{time: 0, text: "alpha"},
			{time: 5, text: "bravo"},
			{time: 10, text: "charlie"},
		},
		playerState: player.State{Position: 5.5},
	}
	out := m.lyricsPanel(60, 20)
	if !strings.Contains(out, "▶ bravo") {
		t.Errorf("active synced line should be marked\n%s", out)
	}
	if strings.Contains(out, "▶ alpha") || strings.Contains(out, "▶ charlie") {
		t.Errorf("only the active line should be marked\n%s", out)
	}

	// Plain lyrics never highlight.
	m.lyricsSynced = false
	out = m.lyricsPanel(60, 20)
	if strings.Contains(out, "▶") {
		t.Errorf("plain lyrics must not highlight\n%s", out)
	}
}

func TestLyricsPanel_AutoScrollKeepsActiveVisible(t *testing.T) {
	var lines []lyricLine
	for i := 0; i < 40; i++ {
		lines = append(lines, lyricLine{time: float64(i), text: fmt.Sprintf("line%02d", i)})
	}
	m := Model{
		width: 120, height: 44, tab: TabNowPlaying, mpv: &player.MPV{},
		showLyrics: true, lyricsSynced: true, lyricsFollow: true,
		lyrics: lines, playerState: player.State{Position: 30},
	}
	out := m.lyricsPanel(60, 12)
	if !strings.Contains(out, "line30") {
		t.Errorf("auto-scroll should keep the active line visible\n%s", out)
	}

	// With follow off, the manual scroll position wins.
	m.lyricsFollow = false
	m.lyricsScroll = 0
	out = m.lyricsPanel(60, 12)
	if !strings.Contains(out, "line00") || strings.Contains(out, "line30") {
		t.Errorf("manual scroll should override follow\n%s", out)
	}
}

func TestScrollLyrics_DisablesFollow(t *testing.T) {
	m := Model{
		lyricsSynced: true, lyricsFollow: true,
		lyrics:      []lyricLine{{time: 0, text: "a"}, {time: 1, text: "b"}, {time: 2, text: "c"}},
		playerState: player.State{Position: 2},
	}
	m.scrollLyrics(1)
	if m.lyricsFollow {
		t.Error("manual scroll must disable auto-follow")
	}
	if m.lyricsScroll < 0 {
		t.Errorf("scroll out of range: %d", m.lyricsScroll)
	}
}
