package app

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// The tab header occupies two rows (the tab bar and its divider) at the very
// top of the screen; every content view starts below it.
const tabHeaderRows = 2

// listTitleRows is the one-row title a bubbletea list always renders.
const listTitleRows = 1

// listItemRows is the vertical stride of a list row: the default delegate draws
// two lines (title + description) plus one blank spacing line.
const listItemRows = 3

// updateMouse handles mouse input: the wheel scrolls the active list (or the
// lyrics), and a left click on a list row selects and plays it.
func (m Model) updateMouse(msg tea.MouseMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		return m.handleWheel(msg.Button == tea.MouseButtonWheelDown, cmds)
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		return m.handleClick(msg.Y)
	}
	return m, tea.Batch(cmds...)
}

// handleWheel scrolls the currently focused list. On Now Playing it scrolls the
// lyrics when shown and otherwise moves the Up Next cursor.
func (m Model) handleWheel(down bool, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch {
	case m.paletteOpen:
		m.moveOverlayCursor(down, len(m.paletteMatches), &m.paletteCursor)
	case m.showHelp:
		m.moveOverlayCursor(down, len(m.helpMatches), &m.helpCursor)
	case m.searchMode:
		if down {
			if m.searchCursor < len(m.searchOptions)-1 {
				m.searchCursor++
			}
		} else if m.searchCursor > 0 {
			m.searchCursor--
		} else {
			m.searchCursor = -1
		}
	case m.tab == TabNowPlaying:
		if m.showLyrics {
			if down {
				m.scrollLyrics(3)
			} else {
				m.scrollLyrics(-3)
			}
		} else {
			m.moveUpNext(down)
		}
	case m.tab == TabSearch:
		m.curlList(down, &m.searchList)
	case m.tab == TabLibrary:
		m.curlList(down, &m.libraryList)
	case m.tab == TabPlaylists:
		if m.inPlaylist {
			m.curlList(down, &m.trackList)
		} else {
			m.curlList(down, &m.playlistList)
		}
	}
	return m, tea.Batch(cmds...)
}

// curlList moves a list cursor up or down.
func (m *Model) curlList(down bool, l *list.Model) {
	if down {
		l.CursorDown()
	} else {
		l.CursorUp()
	}
}

// moveOverlayCursor nudges an overlay (palette/help) cursor, clamped.
func (m *Model) moveOverlayCursor(down bool, n int, cursor *int) {
	if down {
		if *cursor < n-1 {
			*cursor++
		}
	} else if *cursor > 0 {
		*cursor--
	}
}

// handleClick maps a left click at absolute row y to a list row and plays it.
// Overlays and the Up Next panel are keyboard/wheel driven.
func (m Model) handleClick(y int) (tea.Model, tea.Cmd) {
	if y < tabHeaderRows {
		return m, nil
	}
	switch m.tab {
	case TabSearch:
		if m.searchMode {
			return m, nil // clicking a suggestion is out of scope; use ↑/↓
		}
		top := tabHeaderRows + 2 // the search input line and its divider
		if i, ok := listRowIndex(y, top, listTitleRows, listItemRows,
			m.searchList.Paginator.PerPage, m.searchList.Paginator.Page, len(m.searchResults)); ok {
			m.searchList.Select(i)
			return m.handleSelect()
		}
	case TabLibrary:
		if i, ok := listRowIndex(y, tabHeaderRows, listTitleRows, listItemRows,
			m.libraryList.Paginator.PerPage, m.libraryList.Paginator.Page, len(m.libraryTracks)); ok {
			m.libraryList.Select(i)
			return m.handleSelect()
		}
	case TabPlaylists:
		if m.inPlaylist {
			if i, ok := listRowIndex(y, tabHeaderRows, listTitleRows, listItemRows,
				m.trackList.Paginator.PerPage, m.trackList.Paginator.Page, len(m.playlistTracks)); ok {
				m.trackList.Select(i)
				return m.handleSelect()
			}
		} else if i, ok := listRowIndex(y, tabHeaderRows, listTitleRows, listItemRows,
			m.playlistList.Paginator.PerPage, m.playlistList.Paginator.Page, len(m.playlists)); ok {
			m.playlistList.Select(i)
			return m.handleSelect()
		}
	}
	return m, nil
}

// listRowIndex maps an absolute click row to an item index within a bubbletea
// list rendering. listTop is the absolute row of the list's top; the list draws
// a one-row title followed by items of itemRows rows each. It returns ok=false
// when the click lands outside the on-screen items (padding, title, borders).
func listRowIndex(y, listTop, titleRows, itemRows, perPage, page, count int) (int, bool) {
	if perPage < 1 || itemRows < 1 {
		return 0, false
	}
	rel := y - listTop - titleRows
	if rel < 0 {
		return 0, false
	}
	row := rel / itemRows
	if row < 0 || row >= perPage {
		return 0, false
	}
	idx := page*perPage + row
	if idx < 0 || idx >= count {
		return 0, false
	}
	return idx, true
}
