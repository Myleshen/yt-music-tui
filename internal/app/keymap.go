package app

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// KeyMap defines all keyboard shortcuts.
type KeyMap struct {
	// Navigation
	Up   key.Binding
	Down key.Binding

	// Tabs
	Tab0 key.Binding
	Tab1 key.Binding
	Tab2 key.Binding
	Tab3 key.Binding
	Home key.Binding
	Tab4 key.Binding

	// Actions
	Search     key.Binding
	Select     key.Binding
	PlayPause  key.Binding
	Next       key.Binding
	Prev       key.Binding
	VolumeUp   key.Binding
	VolumeDown key.Binding
	SeekFwd    key.Binding
	SeekBack   key.Binding
	Loop       key.Binding

	// Shuffle is implemented; Pomodoro backs the extras features.
	Shuffle  key.Binding
	Pomodoro key.Binding

	// Sleep opens the sleep-timer menu; Theme cycles built-in palettes.
	Sleep key.Binding
	Theme key.Binding

	// Queue editing (Now Playing · Up Next).
	QueueMoveUp   key.Binding
	QueueMoveDown key.Binding
	QueueRemove   key.Binding
	ClearUpcoming key.Binding

	// Rating.
	Like    key.Binding
	Dislike key.Binding

	// Multi-select (search/library/playlist lists).
	SelectMode key.Binding
	BulkAdd    key.Binding

	// Playlist import/export.
	Export key.Binding
	Import key.Binding

	// Lyrics
	Lyrics key.Binding
	Top    key.Binding
	Bottom key.Binding
	// LyricsScrollUp/Down page the lyrics viewport. They are separate from
	// Up/Down (j/k) because those now drive the Now Playing "Up Next" cursor.
	LyricsScrollUp   key.Binding
	LyricsScrollDown key.Binding
	// LyricsFollow toggles auto-scrolling to the active synced-lyric line.
	LyricsFollow key.Binding

	// Search filters
	FilterNext key.Binding
	FilterPrev key.Binding

	// List navigation
	PageDown key.Binding
	PageUp   key.Binding

	// Browse a track's artist/album
	GoArtist key.Binding
	GoAlbum  key.Binding

	// Library editing
	AddToPlaylist  key.Binding
	RemoveFromList key.Binding
	CreatePlaylist key.Binding
	TypeToFilter   key.Binding

	// UI
	Help    key.Binding
	Back    key.Binding
	Quit    key.Binding
	Palette key.Binding
}

// DefaultKeyMap returns vim-style key bindings.
var DefaultKeyMap = KeyMap{
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k/↑", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j/↓", "down"),
	),
	Tab0: key.NewBinding(
		key.WithKeys("1"),
		key.WithHelp("1", "now playing"),
	),
	Tab1: key.NewBinding(
		key.WithKeys("2"),
		key.WithHelp("2", "search"),
	),
	Tab2: key.NewBinding(
		key.WithKeys("3"),
		key.WithHelp("3", "library"),
	),
	Tab3: key.NewBinding(
		key.WithKeys("4"),
		key.WithHelp("4", "playlists"),
	),
	Home: key.NewBinding(
		key.WithKeys("0", "h"),
		key.WithHelp("0", "home"),
	),
	Tab4: key.NewBinding(
		key.WithKeys("5"),
		key.WithHelp("5", "stats"),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "search"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "play"),
	),
	PlayPause: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("space", "play/pause"),
	),
	Next: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "next"),
	),
	Prev: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "prev"),
	),
	VolumeUp: key.NewBinding(
		key.WithKeys("+", "="),
		key.WithHelp("+", "vol up"),
	),
	VolumeDown: key.NewBinding(
		key.WithKeys("-"),
		key.WithHelp("-", "vol down"),
	),
	SeekFwd: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("→", "+5s"),
	),
	SeekBack: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←", "-5s"),
	),
	Loop: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "loop song"),
	),
	// Shuffle defaults to "s"; rebind via the [keys] config section.
	Shuffle: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "shuffle"),
	),
	// Pomodoro starts/stops the focus timer; rebind via the [keys] config.
	// (feat/polish left this unbound as "reserved"; main implements it, so the
	// implemented binding wins.)
	Pomodoro: key.NewBinding(
		key.WithKeys("P"),
		key.WithHelp("P", "pomodoro start/stop"),
	),
	// Sleep opens the sleep-timer menu (uppercase S; lowercase s is shuffle).
	Sleep: key.NewBinding(
		key.WithKeys("S"),
		key.WithHelp("S", "sleep timer"),
	),
	// Theme cycles the built-in palettes at runtime.
	Theme: key.NewBinding(
		key.WithKeys("T"),
		key.WithHelp("T", "cycle theme"),
	),
	QueueMoveUp: key.NewBinding(
		key.WithKeys("K"),
		key.WithHelp("K", "move track up"),
	),
	QueueMoveDown: key.NewBinding(
		key.WithKeys("J"),
		key.WithHelp("J", "move track down"),
	),
	QueueRemove: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "remove track"),
	),
	ClearUpcoming: key.NewBinding(
		key.WithKeys("X"),
		key.WithHelp("X", "clear upcoming"),
	),
	Like: key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "like / unlike"),
	),
	Dislike: key.NewBinding(
		key.WithKeys("L"),
		key.WithHelp("L", "dislike / clear"),
	),
	SelectMode: key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "select mode"),
	),
	BulkAdd: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "add selected to queue"),
	),
	// Export writes the current queue/playlist as M3U + CSV.
	Export: key.NewBinding(
		key.WithKeys("E"),
		key.WithHelp("E", "export queue/playlist"),
	),
	// Import reads ~/.config/ytmusic-tui/import.m3u into the queue.
	Import: key.NewBinding(
		key.WithKeys("I"),
		key.WithHelp("I", "import m3u"),
	),
	Lyrics: key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "lyrics"),
	),
	Top: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "top of lyrics"),
	),
	Bottom: key.NewBinding(
		key.WithKeys("G"),
		key.WithHelp("G", "bottom of lyrics"),
	),
	LyricsScrollUp: key.NewBinding(
		key.WithKeys("["),
		key.WithHelp("[", "lyrics up"),
	),
	LyricsScrollDown: key.NewBinding(
		key.WithKeys("]"),
		key.WithHelp("]", "lyrics down"),
	),
	FilterNext: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "next filter"),
	),
	FilterPrev: key.NewBinding(
		key.WithKeys("F"),
		key.WithHelp("F", "prev filter"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "page down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("ctrl+u", "page up"),
	),
	GoArtist: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "go to artist"),
	),
	GoAlbum: key.NewBinding(
		key.WithKeys("A"),
		key.WithHelp("A", "go to album"),
	),
	AddToPlaylist: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "add to playlist"),
	),
	RemoveFromList: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "remove from playlist"),
	),
	CreatePlaylist: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "create playlist"),
	),
	TypeToFilter: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "type to filter"),
	),
	// LyricsFollow shares "f" with FilterNext. Dispatch is contextual (see
	// updateNormalMode): on Now Playing with the lyrics panel open it toggles
	// auto-follow, while the Search tab uses "f" to cycle the result filter.
	LyricsFollow: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "follow lyrics"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Palette: key.NewBinding(
		key.WithKeys("ctrl+k", ":"),
		key.WithHelp("ctrl+k", "commands"),
	),
}

// actionFields maps a config [keys] action name to the KeyMap field it rebinds.
// The map is rebuilt per call because it holds pointers into a local copy of
// the KeyMap.
func actionFields(km *KeyMap) map[string]*key.Binding {
	return map[string]*key.Binding{
		"up":                 &km.Up,
		"down":               &km.Down,
		"tab0":               &km.Tab0,
		"tab1":               &km.Tab1,
		"tab2":               &km.Tab2,
		"tab3":               &km.Tab3,
		"home":               &km.Home,
		"tab4":               &km.Tab4,
		"search":             &km.Search,
		"select":             &km.Select,
		"play_pause":         &km.PlayPause,
		"next":               &km.Next,
		"prev":               &km.Prev,
		"volume_up":          &km.VolumeUp,
		"volume_down":        &km.VolumeDown,
		"seek_forward":       &km.SeekFwd,
		"seek_back":          &km.SeekBack,
		"loop":               &km.Loop,
		"shuffle":            &km.Shuffle,
		"pomodoro":           &km.Pomodoro,
		"sleep":              &km.Sleep,
		"theme":              &km.Theme,
		"queue_move_up":      &km.QueueMoveUp,
		"queue_move_down":    &km.QueueMoveDown,
		"queue_remove":       &km.QueueRemove,
		"clear_upcoming":     &km.ClearUpcoming,
		"like":               &km.Like,
		"dislike":            &km.Dislike,
		"select_mode":        &km.SelectMode,
		"bulk_add":           &km.BulkAdd,
		"export":             &km.Export,
		"import":             &km.Import,
		"lyrics":             &km.Lyrics,
		"top":                &km.Top,
		"bottom":             &km.Bottom,
		"lyrics_scroll_up":   &km.LyricsScrollUp,
		"lyrics_scroll_down": &km.LyricsScrollDown,
		"lyrics_follow":      &km.LyricsFollow,
		"help":               &km.Help,
		"back":               &km.Back,
		"quit":               &km.Quit,
		"palette":            &km.Palette,
	}
}

// buildKeyMap returns DefaultKeyMap with the configured overrides applied. The
// action names are normalised (trimmed, lowercased, dashes to underscores).
// Actions that are not recognised are returned in unknown (sorted) and
// otherwise ignored, so a stale config never prevents startup.
func buildKeyMap(overrides map[string][]string) (KeyMap, []string) {
	km := DefaultKeyMap
	if len(overrides) == 0 {
		return km, nil
	}
	fields := actionFields(&km)
	var unknown []string
	for action, rawKeys := range overrides {
		name := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(action), "-", "_"))
		binding, ok := fields[name]
		if !ok {
			unknown = append(unknown, action)
			continue
		}
		keys := sanitizeKeys(rawKeys)
		if len(keys) == 0 {
			continue
		}
		help := binding.Help()
		*binding = key.NewBinding(
			key.WithKeys(keys...),
			key.WithHelp(keys[0], help.Desc),
		)
	}
	sort.Strings(unknown)
	return km, unknown
}

// sanitizeKeys cleans configured key strings: it drops empties and duplicates,
// and accepts a couple of friendly aliases. A whitespace-only value is kept as
// the space bar (a single space), not trimmed away.
func sanitizeKeys(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, r := range raw {
		k := strings.TrimSpace(r)
		if k == "" {
			if r == "" {
				continue
			}
			k = " " // whitespace-only means the space bar
		}
		switch strings.ToLower(k) {
		case "space":
			k = " "
		case "return":
			k = "enter"
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}
