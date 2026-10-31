package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// paletteAction is one runnable command shown in the command palette and the
// searchable help screen. When run is nil the action is executed by dispatching
// key through the normal key handler, so palette commands behave exactly like
// their keyboard shortcuts (including context guards).
type paletteAction struct {
	name    string
	section string
	hint    string
	key     string
	run     func(m *Model) []tea.Cmd
}

// paletteActions builds the full, ordered command list from the current key
// map, so a rebound key is reflected in both the palette and the help screen.
// Only actions that actually exist are listed.
func (m Model) paletteActions() []paletteAction {
	k := m.keys
	var acts []paletteAction
	add := func(name, section string, b key.Binding) {
		if kk, hint := keyInfo(b); kk != "" {
			acts = append(acts, paletteAction{name: name, section: section, key: kk, hint: hint})
		}
	}

	// Tabs.
	add("Go to Now Playing", "Tabs", k.Tab0)
	add("Go to Search", "Tabs", k.Tab1)
	add("Go to Library", "Tabs", k.Tab2)
	add("Go to Playlists", "Tabs", k.Tab3)
	add("Go Home (discover)", "Tabs", k.Home)

	// Playback.
	add("Play / Pause", "Playback", k.PlayPause)
	add("Next track", "Playback", k.Next)
	add("Previous track", "Playback", k.Prev)
	add("Toggle shuffle", "Playback", k.Shuffle)
	add("Toggle loop (single track)", "Playback", k.Loop)
	add("Seek forward", "Playback", k.SeekFwd)
	add("Seek back", "Playback", k.SeekBack)
	add("Volume up", "Playback", k.VolumeUp)
	add("Volume down", "Playback", k.VolumeDown)
	add("Like / unlike track", "Playback", k.Like)
	add("Dislike / clear rating", "Playback", k.Dislike)

	// Lyrics.
	add("Toggle lyrics", "Lyrics", k.Lyrics)
	add("Toggle lyrics auto-follow", "Lyrics", k.LyricsFollow)
	add("Scroll lyrics up", "Lyrics", k.LyricsScrollUp)
	add("Scroll lyrics down", "Lyrics", k.LyricsScrollDown)
	add("Lyrics to top", "Lyrics", k.Top)
	add("Lyrics to bottom", "Lyrics", k.Bottom)

	// Navigation.
	add("Search", "Navigation", k.Search)
	add("Play selection", "Navigation", k.Select)
	add("Move down", "Navigation", k.Down)
	add("Move up", "Navigation", k.Up)
	add("Page down", "Navigation", k.PageDown)
	add("Page up", "Navigation", k.PageUp)
	add("Back", "Navigation", k.Back)
	add("Type to filter / jump", "Navigation", k.TypeToFilter)

	// Search filters.
	add("Next search filter", "Search", k.FilterNext)
	add("Previous search filter", "Search", k.FilterPrev)

	// Browse.
	add("Go to artist", "Browse", k.GoArtist)
	add("Go to album", "Browse", k.GoAlbum)

	// Queue editing (Now Playing · Up Next).
	add("Move track up", "Queue", k.QueueMoveUp)
	add("Move track down", "Queue", k.QueueMoveDown)
	add("Remove track from queue", "Queue", k.QueueRemove)
	add("Clear upcoming tracks", "Queue", k.ClearUpcoming)

	// Library / playlist editing.
	add("Add to playlist", "Playlist", k.AddToPlaylist)
	add("Remove from playlist", "Playlist", k.RemoveFromList)
	add("Create playlist", "Playlist", k.CreatePlaylist)
	add("Toggle select mode", "Multi-select", k.SelectMode)
	add("Add selected to queue", "Multi-select", k.BulkAdd)

	// Extras.
	add("Start / stop pomodoro", "Extras", k.Pomodoro)

	// Other.
	add("Toggle help", "Other", k.Help)
	add("Quit", "Other", k.Quit)

	return acts
}

// keyInfo returns the first key of a binding (for dispatch) and a friendly hint
// listing all its keys. A space key is shown as "space" so the help reads well.
func keyInfo(b key.Binding) (first, hint string) {
	ks := b.Keys()
	if len(ks) == 0 {
		return "", ""
	}
	pretty := make([]string, len(ks))
	for i, k := range ks {
		pretty[i] = prettyKey(k)
	}
	return ks[0], strings.Join(pretty, " / ")
}

func prettyKey(k string) string {
	if k == " " {
		return "space"
	}
	return k
}

// keyMsgFrom turns a binding key string into a KeyMsg that key.Matches accepts.
// Single runes (including the space bar) become key runes; the named keys the
// app binds are mapped to their KeyType.
func keyMsgFrom(k string) (tea.KeyMsg, bool) {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}, true
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}, true
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}, true
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}, true
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}, true
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}, true
	}
	if len([]rune(k)) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}, true
	}
	return tea.KeyMsg{}, false
}

// runAction executes a palette action, returning any resulting command.
func (m *Model) runAction(a paletteAction) tea.Cmd {
	if a.run != nil {
		return tea.Batch(a.run(m)...)
	}
	km, ok := keyMsgFrom(a.key)
	if !ok {
		return nil
	}
	updated, cmd := m.updateNormalMode(km, nil)
	*m = updated.(Model)
	return cmd
}

// --- fuzzy matching --------------------------------------------------------

// fuzzyMatch reports whether pattern is a subsequence of target
// (case-insensitively) and returns a relevance score (higher is better).
// Contiguous runs and matches at word boundaries score higher, and shorter
// targets win ties.
func fuzzyMatch(pattern, target string) (int, bool) {
	p := []rune(strings.ToLower(pattern))
	if len(p) == 0 {
		return 0, true
	}
	t := []rune(strings.ToLower(target))
	ti := 0
	score := 0
	prev := -2
	for _, pr := range p {
		found := -1
		for i := ti; i < len(t); i++ {
			if t[i] == pr {
				found = i
				break
			}
		}
		if found < 0 {
			return 0, false
		}
		score += 10
		if found == prev+1 {
			score += 15 // contiguous with the previous match
		}
		if found == 0 || isWordBoundary(t[found-1]) {
			score += 10
		}
		prev = found
		ti = found + 1
	}
	return score - len(t), true
}

func isWordBoundary(r rune) bool {
	return r == ' ' || r == '/' || r == '-' || r == '_' || r == ':'
}

// filterActions returns the actions matching pattern (fuzzy across name, hint
// and section), best match first. An empty pattern returns the input unchanged.
func filterActions(actions []paletteAction, pattern string) []paletteAction {
	if strings.TrimSpace(pattern) == "" {
		return actions
	}
	type scored struct {
		a paletteAction
		s int
	}
	var matches []scored
	for _, a := range actions {
		hay := a.name + " " + a.hint + " " + a.section
		if s, ok := fuzzyMatch(pattern, hay); ok {
			matches = append(matches, scored{a, s})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].s > matches[j].s })
	out := make([]paletteAction, len(matches))
	for i, m := range matches {
		out[i] = m.a
	}
	return out
}

// windowRange returns the [start,end) slice of a list of size total that keeps
// cursor visible in a window of at most rows, along with the slice itself.
func windowRange(cursor, total, rows int) (int, int) {
	if rows < 1 {
		rows = 1
	}
	if total <= rows {
		return 0, total
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= total {
		cursor = total - 1
	}
	start := cursor - rows/2
	if start < 0 {
		start = 0
	}
	if start > total-rows {
		start = total - rows
	}
	return start, start + rows
}

// --- command palette -------------------------------------------------------

func newPaletteInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.Placeholder = "type a command…"
	ti.CharLimit = 60
	return ti
}

// openPalette shows the command palette with every action listed.
func (m *Model) openPalette() {
	if m.paletteInput.Prompt == "" {
		m.paletteInput = newPaletteInput() // bare Model (e.g. in tests)
	}
	m.paletteOpen = true
	m.paletteInput.SetValue("")
	m.paletteInput.Focus()
	m.paletteMatches = m.paletteActions()
	m.paletteCursor = 0
}

func (m *Model) closePalette() {
	m.paletteOpen = false
	m.paletteInput.Blur()
}

// updatePalette handles keys while the command palette is open.
func (m Model) updatePalette(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+k":
		m.closePalette()
		return m, tea.Batch(cmds...)
	case "enter":
		if m.paletteCursor >= 0 && m.paletteCursor < len(m.paletteMatches) {
			action := m.paletteMatches[m.paletteCursor]
			m.closePalette()
			if cmd := m.runAction(action); cmd != nil {
				return m, cmd
			}
		}
		return m, tea.Batch(cmds...)
	case "up", "ctrl+p":
		if m.paletteCursor > 0 {
			m.paletteCursor--
		}
		return m, tea.Batch(cmds...)
	case "down", "ctrl+n":
		if m.paletteCursor < len(m.paletteMatches)-1 {
			m.paletteCursor++
		}
		return m, tea.Batch(cmds...)
	}

	var cmd tea.Cmd
	m.paletteInput, cmd = m.paletteInput.Update(msg)
	m.paletteMatches = filterActions(m.paletteActions(), m.paletteInput.Value())
	if m.paletteCursor >= len(m.paletteMatches) {
		m.paletteCursor = len(m.paletteMatches) - 1
	}
	if m.paletteCursor < 0 {
		m.paletteCursor = 0
	}
	return m, tea.Batch(append(cmds, cmd)...)
}

func (m Model) paletteView() string {
	if m.width <= 0 || m.height <= 0 {
		return "command palette"
	}
	boxW := m.width - 8
	if boxW > 64 {
		boxW = 64
	}
	if boxW < 24 {
		boxW = 24
	}
	innerW := boxW - 4

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colAccentNP).Render("Commands"))
	b.WriteString("\n")
	b.WriteString(m.paletteInput.View())
	b.WriteString("\n")

	maxRows := m.height - 8
	if maxRows < 3 {
		maxRows = 3
	}
	start, end := windowRange(m.paletteCursor, len(m.paletteMatches), maxRows)
	if len(m.paletteMatches) == 0 {
		b.WriteString("\n" + styleMuted.Render("no matching commands"))
	}
	for i := start; i < end; i++ {
		a := m.paletteMatches[i]
		label := fmt.Sprintf("%-28s %s", clip(a.name, 28), styleMuted.Render(a.hint))
		if i == m.paletteCursor {
			b.WriteString("\n" + lipgloss.NewStyle().
				Bold(true).Foreground(colSelFg).Background(colSelBg).
				Render(" "+lipgloss.NewStyle().Width(innerW-2).Render(label)+" "))
		} else {
			b.WriteString("\n " + clip(label, innerW-2))
		}
	}
	b.WriteString("\n")
	b.WriteString(styleMuted.Render("↑/↓ move · enter run · esc close"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccentNP).
		Padding(1, 2).
		Width(boxW).
		Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// --- searchable help -------------------------------------------------------

// openHelp shows the help screen with the full action list.
func (m *Model) openHelp() {
	m.showHelp = true
	m.helpFilter = ""
	m.helpMatches = m.paletteActions()
	m.helpCursor = 0
}

func (m *Model) closeHelp() {
	m.showHelp = false
	m.helpFilter = ""
	m.helpMatches = nil
	m.helpCursor = 0
}

func (m *Model) refilterHelp() {
	m.helpMatches = filterActions(m.paletteActions(), m.helpFilter)
	if m.helpCursor >= len(m.helpMatches) {
		m.helpCursor = len(m.helpMatches) - 1
	}
	if m.helpCursor < 0 {
		m.helpCursor = 0
	}
}

// updateHelp handles keys while the searchable help screen is open: printable
// runes filter the list, up/down move the selection, and enter runs it.
func (m Model) updateHelp(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "esc":
		m.closeHelp()
		return m, tea.Batch(cmds...)
	case "up", "ctrl+p":
		if m.helpCursor > 0 {
			m.helpCursor--
		}
		return m, tea.Batch(cmds...)
	case "down", "ctrl+n":
		if m.helpCursor < len(m.helpMatches)-1 {
			m.helpCursor++
		}
		return m, tea.Batch(cmds...)
	case "enter":
		if m.helpCursor >= 0 && m.helpCursor < len(m.helpMatches) {
			action := m.helpMatches[m.helpCursor]
			m.closeHelp()
			if cmd := m.runAction(action); cmd != nil {
				return m, cmd
			}
		}
		return m, tea.Batch(cmds...)
	case "backspace":
		if m.helpFilter != "" {
			r := []rune(m.helpFilter)
			m.helpFilter = string(r[:len(r)-1])
			m.refilterHelp()
		}
		return m, tea.Batch(cmds...)
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		m.helpFilter += string(msg.Runes)
		m.refilterHelp()
	}
	return m, tea.Batch(cmds...)
}

func (m Model) helpView() string {
	bold := lipgloss.NewStyle().Bold(true)
	accent := lipgloss.NewStyle().Bold(true).Foreground(colAccentNP)

	var b strings.Builder
	b.WriteString(bold.Render("ytm  —  keyboard shortcuts"))
	b.WriteString("  ")
	filterLabel := m.helpFilter
	if filterLabel == "" {
		filterLabel = styleMuted.Render("type to filter")
	} else {
		filterLabel = accent.Render(filterLabel)
	}
	b.WriteString(styleMuted.Render("filter: ") + filterLabel)
	b.WriteString("\n\n")

	if len(m.helpMatches) == 0 {
		b.WriteString(styleMuted.Render("no matching shortcuts"))
	}

	// Group by section, preserving the first-seen order.
	var order []string
	bySection := map[string][]int{}
	for i, a := range m.helpMatches {
		if _, ok := bySection[a.section]; !ok {
			order = append(order, a.section)
		}
		bySection[a.section] = append(bySection[a.section], i)
	}
	firstSection := true
	for _, section := range order {
		if !firstSection {
			b.WriteString("\n")
		}
		firstSection = false
		b.WriteString(accent.Render(section) + "\n")
		for _, idx := range bySection[section] {
			a := m.helpMatches[idx]
			row := fmt.Sprintf("  %-22s %s", a.hint, a.name)
			if idx == m.helpCursor {
				b.WriteString(lipgloss.NewStyle().
					Bold(true).Foreground(colSelFg).Background(colSelBg).
					Render(row) + "\n")
			} else {
				b.WriteString(row + "\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(styleMuted.Render("↑/↓ move · enter run · esc close"))
	return lipgloss.NewStyle().Padding(1, 3).Render(b.String())
}
