package app

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// --- Up Next queue editing -----------------------------------------------

// clampUpNextCursor keeps the Up Next cursor within the number of upcoming
// tracks after a mutation.
func (m *Model) clampUpNextCursor() {
	if m.queue == nil {
		m.upNextCursor = 0
		return
	}
	n := m.queue.UpcomingCount()
	if n <= 0 {
		m.upNextCursor = 0
		return
	}
	if m.upNextCursor >= n {
		m.upNextCursor = n - 1
	}
	if m.upNextCursor < 0 {
		m.upNextCursor = 0
	}
}

// saveRatingsCmd returns a command that persists the current ratings, or nil
// when there is no store to persist to.
func (m Model) saveRatingsCmd() tea.Cmd {
	if m.store == nil {
		return nil
	}
	snapshot := make(map[string]string, len(m.ratings))
	for id, r := range m.ratings {
		snapshot[id] = string(r)
	}
	st := m.store
	return func() tea.Msg {
		if err := st.SaveRatings(snapshot); err != nil {
			slog.Warn("save ratings", "err", err)
		}
		return nil
	}
}

// reorderUpNext moves the Up Next selection one position earlier (down=false)
// or later (down=true) in the queue's playback order. It keeps the cursor on
// the moved track and never moves the currently playing track.
func (m *Model) reorderUpNext(down bool) []tea.Cmd {
	if m.queue == nil {
		return nil
	}
	idx, ok := m.upNextTargetIndex()
	if !ok {
		return nil
	}
	moved := m.queue.MoveUp(idx)
	if down {
		moved = m.queue.MoveDown(idx)
	}
	if !moved {
		return nil
	}
	if down {
		m.upNextCursor++
	} else if m.upNextCursor > 0 {
		m.upNextCursor--
	}
	m.clampUpNextCursor()
	m.prefetchNeighbors()
	cmds := []tea.Cmd{m.showOSD("↕  reordered queue")}
	if c := m.saveStateCmd(); c != nil {
		cmds = append(cmds, c)
	}
	return cmds
}

// removeUpNext deletes the track selected in the Up Next panel.
func (m *Model) removeUpNext() []tea.Cmd {
	if m.queue == nil {
		return nil
	}
	idx, ok := m.upNextTargetIndex()
	if !ok {
		return nil
	}
	tr, _ := m.queue.TrackAt(idx)
	if !m.queue.Remove(idx) {
		return nil
	}
	m.clampUpNextCursor()
	m.prefetchNeighbors()
	name := tr.Title
	if name == "" {
		name = "track"
	}
	cmds := []tea.Cmd{m.showOSD("🗑  removed " + clip(name, 24))}
	if c := m.saveStateCmd(); c != nil {
		cmds = append(cmds, c)
	}
	return cmds
}

// clearUpcoming drops every track after the current one, leaving playback
// untouched.
func (m *Model) clearUpcoming() []tea.Cmd {
	if m.queue == nil {
		return nil
	}
	n := m.queue.ClearUpcoming()
	if n == 0 {
		return nil
	}
	m.upNextCursor = 0
	cmds := []tea.Cmd{m.showOSD(fmt.Sprintf("🧹  cleared %d upcoming", n))}
	if c := m.saveStateCmd(); c != nil {
		cmds = append(cmds, c)
	}
	return cmds
}

// --- Rating (like / dislike) ---------------------------------------------

// ratingOf returns the known rating for a videoID, defaulting to indifferent.
func (m Model) ratingOf(videoID string) ytmusic.Rating {
	if m.ratings != nil {
		if r, ok := m.ratings[videoID]; ok {
			return r
		}
	}
	return ytmusic.RatingIndifferent
}

// setRating records a rating in the in-memory map (indifferent clears it).
func (m *Model) setRating(videoID string, r ytmusic.Rating) {
	if videoID == "" {
		return
	}
	if r == ytmusic.RatingIndifferent {
		delete(m.ratings, videoID)
		return
	}
	if m.ratings == nil {
		m.ratings = make(map[string]ytmusic.Rating)
	}
	m.ratings[videoID] = r
}

// seedLiked marks every track in tracks as liked unless its rating is already
// known, so an explicit unlike is never overwritten by a library snapshot.
func (m *Model) seedLiked(tracks []ytmusic.Track) {
	if len(tracks) == 0 {
		return
	}
	if m.ratings == nil {
		m.ratings = make(map[string]ytmusic.Rating)
	}
	for _, t := range tracks {
		if t.VideoID == "" {
			continue
		}
		if _, ok := m.ratings[t.VideoID]; ok {
			continue
		}
		m.ratings[t.VideoID] = ytmusic.RatingLike
	}
}

// ratingIcon returns the indicator glyph for a rating.
func ratingIcon(r ytmusic.Rating) string {
	switch r {
	case ytmusic.RatingLike:
		return "♥"
	case ytmusic.RatingDislike:
		return "✖"
	default:
		return "♡"
	}
}

// toggleRating flips the current track between the given rating and
// indifferent (like -> unlike, or dislike -> clear), applying the change
// optimistically and rolling it back if the API call fails.
func (m *Model) toggleRating(kind ytmusic.Rating) []tea.Cmd {
	t, ok := m.currentTrack()
	if !ok || t.VideoID == "" {
		return nil
	}
	if m.ytClient == nil || !m.ytClient.Authenticated() {
		return []tea.Cmd{m.showToast("⚠  sign in to rate tracks")}
	}
	prev := m.ratingOf(t.VideoID)
	next := ytmusic.RatingIndifferent
	msgText := "♡  rating cleared"
	switch kind {
	case ytmusic.RatingLike:
		if prev != ytmusic.RatingLike {
			next = ytmusic.RatingLike
			msgText = "♥  liked"
		}
	case ytmusic.RatingDislike:
		if prev != ytmusic.RatingDislike {
			next = ytmusic.RatingDislike
			msgText = "✖  disliked"
		}
	}
	m.setRating(t.VideoID, next)

	client := m.ytClient
	videoID := t.VideoID
	cmds := []tea.Cmd{m.showOSD(msgText)}
	cmds = append(cmds, func() tea.Msg {
		return ratingMsg{videoID: videoID, prev: prev, err: client.SetRating(videoID, next)}
	})
	if c := m.saveRatingsCmd(); c != nil {
		cmds = append(cmds, c)
	}
	return cmds
}

// --- Multi-select ---------------------------------------------------------

// activeTrackSource returns the track slice backing the active list and its
// highlighted row index. ok is false on tabs that are not lists.
func (m Model) activeTrackSource() (tracks []ytmusic.Track, index int, ok bool) {
	switch m.tab {
	case TabSearch:
		return m.searchResults, m.searchList.Index(), true
	case TabLibrary:
		return m.libraryTracks, m.libraryList.Index(), true
	case TabPlaylists:
		if m.inPlaylist {
			return m.playlistTracks, m.trackList.Index(), true
		}
	}
	return nil, 0, false
}

// activeList returns a pointer to the list model for the active tab, or nil.
func (m *Model) activeList() *list.Model {
	switch m.tab {
	case TabSearch:
		return &m.searchList
	case TabLibrary:
		return &m.libraryList
	case TabPlaylists:
		if m.inPlaylist {
			return &m.trackList
		}
	}
	return nil
}

// toggleSelect enters or leaves multi-select mode for the active list.
func (m *Model) toggleSelect() {
	if m.selectMode {
		m.exitSelect()
		return
	}
	if _, _, ok := m.activeTrackSource(); !ok {
		return
	}
	m.selectMode = true
	m.marks = make(map[int]bool)
	m.refreshActiveListItems()
	m.status = "select: space mark · a add to queue · esc cancel"
}

// exitSelect leaves multi-select mode and clears any marks.
func (m *Model) exitSelect() {
	if !m.selectMode && len(m.marks) == 0 {
		return
	}
	m.selectMode = false
	m.marks = make(map[int]bool)
	m.refreshActiveListItems()
	if strings.HasPrefix(m.status, "select") {
		m.status = ""
	}
}

// toggleMark toggles the mark on the highlighted row and moves down one.
func (m *Model) toggleMark() tea.Cmd {
	l := m.activeList()
	if l == nil {
		return nil
	}
	i := l.Index()
	if m.marks == nil {
		m.marks = make(map[int]bool)
	}
	if m.marks[i] {
		delete(m.marks, i)
	} else {
		m.marks[i] = true
	}
	m.refreshActiveListItems()
	l.CursorDown()
	m.status = fmt.Sprintf("select: %d marked · a add to queue · esc cancel", len(m.marks))
	return nil
}

// commitSelect adds every marked row to the queue. With nothing playing it
// starts a new queue from the selection instead.
func (m *Model) commitSelect() []tea.Cmd {
	tracks, _, ok := m.activeTrackSource()
	marked := len(m.marks)
	if !ok || marked == 0 {
		m.exitSelect()
		return nil
	}
	var selected []ytmusic.Track
	for i := range tracks {
		if m.marks[i] {
			selected = append(selected, tracks[i])
		}
	}
	m.exitSelect()
	if len(selected) == 0 {
		return nil
	}

	// Nothing playing: start a queue from the selection.
	if m.mpv == nil || m.queue == nil {
		sel := selected
		return []tea.Cmd{func() tea.Msg {
			return m.startPlayback(sel, 0, "", 0, false, nil)
		}}
	}

	added := m.queue.Append(selected)
	cmds := []tea.Cmd{m.showOSD(fmt.Sprintf("＋  added %d to queue", added))}
	if c := m.saveStateCmd(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.prefetchTracksCmd(selected); c != nil {
		cmds = append(cmds, c)
	}
	return cmds
}

// refreshActiveListItems rebuilds the active list's items so marks render,
// preserving the highlighted row.
func (m *Model) refreshActiveListItems() {
	tracks, _, ok := m.activeTrackSource()
	if !ok {
		return
	}
	items := tracksToItemsMarked(tracks, m.marks)
	switch m.tab {
	case TabSearch:
		i := m.searchList.Index()
		m.searchList.SetItems(items)
		m.searchList.Select(i)
	case TabLibrary:
		i := m.libraryList.Index()
		m.libraryList.SetItems(items)
		m.libraryList.Select(i)
	case TabPlaylists:
		if m.inPlaylist {
			i := m.trackList.Index()
			m.trackList.SetItems(items)
			m.trackList.Select(i)
		}
	}
}

// tracksToItemsMarked is tracksToItems with a check mark on marked rows.
func tracksToItemsMarked(tracks []ytmusic.Track, marks map[int]bool) []list.Item {
	items := make([]list.Item, len(tracks))
	for i, t := range tracks {
		items[i] = trackItem{track: t, marked: marks[i]}
	}
	return items
}
