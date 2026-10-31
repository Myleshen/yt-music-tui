package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// The Home (Discover) tab is a stack of scrollable, sectioned views. Each view
// is a titled list of sections; a section is either a run of playable tracks or
// a run of playable collections (playlists, albums, moods). Selecting a track
// plays it from its section; selecting a collection drills into a new view,
// which Esc pops back from.

// entryKind distinguishes a playable track row from a collection row.
type entryKind int

const (
	entryTrack entryKind = iota
	entryCollection
)

// homeEntry is one selectable row of a section.
type homeEntry struct {
	kind       entryKind
	trackIdx   int // index into the section's track list (entryTrack)
	track      ytmusic.Track
	collection ytmusic.Collection
}

// homeSection is a titled group within a view.
type homeSection struct {
	title   string
	desc    string
	tracks  []ytmusic.Track // the playable queue for this section
	entries []homeEntry
}

// homeRow points at either a section header (entry < 0) or a section entry.
type homeRow struct {
	section int
	entry   int
}

// homeView is one screen of the Home tab: a title plus its sections, a cursor
// over the selectable rows, and a scroll offset.
type homeView struct {
	title    string
	sections []homeSection
	rows     []homeRow
	cursor   int
	offset   int
}

// newHomeView builds a view from client sections, flattening each section's
// tracks and collections into selectable entries while remembering the full
// track list for playback.
func newHomeView(title string, sections []ytmusic.Section) homeView {
	v := homeView{title: title}
	for _, s := range sections {
		hs := homeSection{title: s.Title, desc: s.Description, tracks: s.Tracks}
		for i, t := range s.Tracks {
			hs.entries = append(hs.entries, homeEntry{kind: entryTrack, trackIdx: i, track: t})
		}
		for _, c := range s.Collections {
			hs.entries = append(hs.entries, homeEntry{kind: entryCollection, collection: c})
		}
		si := len(v.sections)
		v.sections = append(v.sections, hs)
		if hs.title != "" || hs.desc != "" {
			v.rows = append(v.rows, homeRow{section: si, entry: -1})
		}
		for i := range hs.entries {
			v.rows = append(v.rows, homeRow{section: si, entry: i})
		}
	}
	v.cursor = v.firstSelectable()
	return v
}

// firstSelectable returns the index of the first entry row, or 0.
func (v homeView) firstSelectable() int {
	for i, r := range v.rows {
		if r.entry >= 0 {
			return i
		}
	}
	return 0
}

// selected returns the section and entry under the cursor.
func (v homeView) selected() (homeSection, homeEntry, bool) {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return homeSection{}, homeEntry{}, false
	}
	r := v.rows[v.cursor]
	if r.entry < 0 || r.section >= len(v.sections) {
		return homeSection{}, homeEntry{}, false
	}
	s := v.sections[r.section]
	if r.entry >= len(s.entries) {
		return homeSection{}, homeEntry{}, false
	}
	return s, s.entries[r.entry], true
}

// move shifts the cursor to the next/previous selectable row, skipping headers.
func (v *homeView) move(down bool) {
	if down {
		for i := v.cursor + 1; i < len(v.rows); i++ {
			if v.rows[i].entry >= 0 {
				v.cursor = i
				return
			}
		}
		return
	}
	for i := v.cursor - 1; i >= 0; i-- {
		if v.rows[i].entry >= 0 {
			v.cursor = i
			return
		}
	}
}

// --- Model integration -----------------------------------------------------

// currentHome returns the active view (the top of the stack), or an empty one.
func (m Model) currentHome() homeView {
	if len(m.homeViews) == 0 {
		return homeView{}
	}
	return m.homeViews[len(m.homeViews)-1]
}

// moveHome moves the cursor in the active view.
func (m *Model) moveHome(down bool) {
	if len(m.homeViews) == 0 {
		return
	}
	m.homeViews[len(m.homeViews)-1].move(down)
}

// popHome returns from a drilled-in collection to the previous view. The feed
// (index 0) is never popped.
func (m *Model) popHome() {
	if len(m.homeViews) > 1 {
		m.homeViews = m.homeViews[:len(m.homeViews)-1]
		m.status = ""
	}
}

// homeTabView renders the active Home view to the given content height.
func (m Model) homeTabView(height int) string {
	if len(m.homeViews) == 0 {
		msg := "loading home…"
		if !m.homeLoading && !m.homeLoaded {
			msg = "press 0 to load home"
		}
		if !m.homeLoading && m.homeLoaded {
			msg = "nothing to show"
		}
		return vertCenter("  "+styleMuted.Render(msg), height)
	}
	v := m.currentHome()
	return renderHomeView(v, m.width, height)
}

// renderHomeView lays out a Home view: a header line, then the visible rows,
// scrolling so the cursor is always on screen.
func renderHomeView(v homeView, width, height int) string {
	accent := accentForTab(TabHome)
	header := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("  " + v.title)
	hint := styleMuted.Render("j/k move · enter play/open · esc back")
	if width >= 60 {
		gap := width - lipgloss.Width(header) - lipgloss.Width(hint) - 2
		if gap < 1 {
			gap = 1
		}
		header = header + strings.Repeat(" ", gap) + hint
	}

	rowsH := height - 2 // header line + a blank spacer
	if rowsH < 1 {
		rowsH = 1
	}

	// Scroll so the cursor stays visible.
	first := v.offset
	if v.cursor < first {
		first = v.cursor
	}
	if v.cursor >= first+rowsH {
		first = v.cursor - rowsH + 1
	}
	if first < 0 {
		first = 0
	}
	last := first + rowsH
	if last > len(v.rows) {
		last = len(v.rows)
	}

	lines := []string{header, ""}
	for i := first; i < last; i++ {
		r := v.rows[i]
		if r.entry < 0 {
			lines = append(lines, renderSectionHeader(v.sections[r.section]))
			continue
		}
		s := v.sections[r.section]
		lines = append(lines, renderHomeEntry(s.entries[r.entry], width, i == v.cursor))
	}
	if last < len(v.rows) {
		lines = append(lines, styleMuted.Render(fmt.Sprintf("   … %d more", len(v.rows)-last)))
	}
	return strings.Join(lines, "\n")
}

// renderSectionHeader renders a section title (with its item count) and, for a
// description shelf, a muted description line.
func renderSectionHeader(s homeSection) string {
	label := "  " + lipgloss.NewStyle().Bold(true).Foreground(colAccentPlay).Render(s.title)
	if n := len(s.entries); n > 0 {
		label += styleMuted.Render(fmt.Sprintf("   %d", n))
	}
	if s.desc != "" {
		label += "\n  " + styleMuted.Render(s.desc)
	}
	return label
}

// renderHomeEntry renders a single selectable row: a track (title + artist and
// duration) or a collection (title + subtitle), aligned into two columns and
// highlighted when selected.
func renderHomeEntry(e homeEntry, width int, selected bool) string {
	var title, desc, marker string
	switch e.kind {
	case entryTrack:
		title = e.track.Title
		desc = e.track.Artist
		if e.track.Duration != "" {
			if desc != "" {
				desc += "  ·  "
			}
			desc += e.track.Duration
		}
		marker = "♪ "
	case entryCollection:
		title = e.collection.Title
		desc = e.collection.Subtitle
		marker = "▸ "
	}

	const markerW = 2
	avail := width - markerW - 2
	if avail < 10 {
		avail = 10
	}
	tw := avail * 3 / 5
	if tw < 8 {
		tw = 8
	}
	dw := avail - tw

	title = clip(title, tw)
	desc = clip(desc, dw)
	tpad := strings.Repeat(" ", maxInt(0, tw-lipgloss.Width(title)))
	dpad := strings.Repeat(" ", maxInt(0, dw-lipgloss.Width(desc)))

	if selected {
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(colSelFg).
			Background(colSelBg).
			Render(">" + marker + title + tpad + desc + dpad)
	}
	return " " + styleMuted.Render(marker) + title + tpad + styleMuted.Render(desc) + dpad
}
