package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// --- Active-list helpers ---------------------------------------------------

// hasActiveList reports whether the current tab is driven by a list (every tab
// except Now Playing).
func (m Model) hasActiveList() bool { return m.tab != TabNowPlaying }

// activeListPtr returns the list model that owns the current tab's selection.
func (m *Model) activeListPtr() *list.Model {
	switch m.tab {
	case TabSearch:
		return &m.searchList
	case TabLibrary:
		return &m.libraryList
	case TabPlaylists:
		if m.inPlaylist {
			return &m.trackList
		}
		return &m.playlistList
	case TabPage:
		return &m.pageList
	}
	return nil
}

// listToTop / listToBottom jump the active list's selection (g / G).
func (m *Model) listToTop() {
	if l := m.activeListPtr(); l != nil && len(l.Items()) > 0 {
		l.Select(0)
	}
}

func (m *Model) listToBottom() {
	if l := m.activeListPtr(); l != nil {
		if n := len(l.Items()); n > 0 {
			l.Select(n - 1)
		}
	}
}

// listPage moves the active list's selection by one visible page (ctrl+d/ctrl+u).
func (m *Model) listPage(delta int) {
	l := m.activeListPtr()
	if l == nil {
		return
	}
	n := len(l.Items())
	if n == 0 {
		return
	}
	page := l.Height() - 3 // list title + status/pagination chrome
	if page < 1 {
		page = 1
	}
	target := l.Index() + delta*page
	if target < 0 {
		target = 0
	}
	if target >= n {
		target = n - 1
	}
	l.Select(target)
}

// playbackSource is the playlist name to attribute a queued track to, if any.
func (m Model) playbackSource() string {
	switch m.tab {
	case TabPlaylists:
		if m.inPlaylist {
			return m.trackList.Title
		}
	case TabPage:
		if m.pageKind == pagePlaylist {
			return m.pageTitle
		}
	}
	return ""
}

// targetTrack returns the track the user is "on": the playing track on Now
// Playing, or the selected track item on a list tab.
func (m Model) targetTrack() (ytmusic.Track, bool) {
	if m.tab == TabNowPlaying {
		return m.currentTrack()
	}
	if l := m.activeListPtr(); l != nil {
		if t, ok := l.SelectedItem().(trackItem); ok {
			return t.track, true
		}
	}
	return ytmusic.Track{}, false
}

// --- Type-to-filter / jump -------------------------------------------------

// startFilter snapshots the active list and enters type-to-filter mode.
func (m *Model) startFilter() {
	l := m.activeListPtr()
	if l == nil {
		return
	}
	m.filterBase = append([]list.Item(nil), l.Items()...)
	m.filterMode = true
	m.filterInput.SetValue("")
	m.filterInput.Focus()
}

func (m Model) updateFilterMode(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filterMode = false
		m.filterInput.Blur()
		if l := m.activeListPtr(); l != nil {
			l.SetItems(m.filterBase)
			l.Select(0)
		}
		return m, tea.Batch(cmds...)
	case "enter":
		m.filterMode = false
		m.filterInput.Blur()
		if l := m.activeListPtr(); l != nil && len(l.Items()) > 0 {
			l.Select(0)
		}
		return m, tea.Batch(cmds...)
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.applyFilter()
	return m, tea.Batch(append(cmds, cmd)...)
}

// applyFilter rewrites the active list to the items whose FilterValue matches
// the current filter text.
func (m *Model) applyFilter() {
	l := m.activeListPtr()
	if l == nil {
		return
	}
	pattern := m.filterInput.Value()
	filtered := make([]list.Item, 0, len(m.filterBase))
	for _, it := range m.filterBase {
		if _, ok := fuzzyMatch(pattern, it.FilterValue()); ok {
			filtered = append(filtered, it)
		}
	}
	l.SetItems(filtered)
	l.Select(0)
}

// --- Browse pages (artist / album / playlist) ------------------------------

// openBrowseItem pushes a browse page for a tile and kicks off its load.
func (m Model) openBrowseItem(it ytmusic.BrowseItem) (Model, tea.Cmd) {
	m.pageOrigin = m.tab
	m.tab = TabPage
	m.pageTitle = it.Title
	m.pageSubtitle = it.Subtitle
	m.pagePlaylist = ""
	m.pageList.SetItems(nil)
	switch it.Kind {
	case ytmusic.KindArtist:
		m.pageKind = pageArtist
		m.status = "loading artist…"
		return m, m.loadArtistCmd(it.ID)
	case ytmusic.KindPlaylist:
		m.pageKind = pagePlaylist
		m.pagePlaylist = it.ID
		m.status = "loading playlist…"
		return m, m.loadPageTracksCmd(it.ID, it.Title)
	default: // album
		m.pageKind = pageAlbum
		m.status = "loading album…"
		return m, m.loadAlbumCmd(it.ID)
	}
}

func (m Model) loadAlbumCmd(id string) tea.Cmd {
	client := m.ytClient
	return func() tea.Msg {
		page, err := client.GetAlbum(id)
		return pageAlbumMsg{page: page, err: err}
	}
}

func (m Model) loadArtistCmd(id string) tea.Cmd {
	client := m.ytClient
	return func() tea.Msg {
		page, err := client.GetArtist(id)
		return pageArtistMsg{page: page, err: err}
	}
}

func (m Model) loadPageTracksCmd(id, title string) tea.Cmd {
	client := m.ytClient
	return func() tea.Msg {
		tracks, err := client.GetPlaylistTracks(id)
		return pageTracksMsg{tracks: tracks, title: title, playlistID: id, err: err}
	}
}

// goToArtist / goToAlbum push the artist/album page for the target track.
func (m Model) goToArtist() (Model, tea.Cmd) {
	trk, ok := m.targetTrack()
	if !ok {
		m.status = "no track selected"
		return m, nil
	}
	if trk.ArtistID == "" {
		m.status = "no artist link for this track"
		return m, nil
	}
	return m.openBrowseItem(ytmusic.BrowseItem{ID: trk.ArtistID, Title: trk.Artist, Kind: ytmusic.KindArtist})
}

func (m Model) goToAlbum() (Model, tea.Cmd) {
	trk, ok := m.targetTrack()
	if !ok {
		m.status = "no track selected"
		return m, nil
	}
	if trk.AlbumID == "" {
		m.status = "no album link for this track"
		return m, nil
	}
	return m.openBrowseItem(ytmusic.BrowseItem{ID: trk.AlbumID, Title: trk.Album, Kind: ytmusic.KindAlbum})
}

// --- Create playlist -------------------------------------------------------

func (m Model) updateNameMode(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.nameMode = false
		m.nameInput.Blur()
		return m, tea.Batch(cmds...)
	case "enter":
		title := strings.TrimSpace(m.nameInput.Value())
		m.nameMode = false
		m.nameInput.Blur()
		if title == "" {
			m.status = "playlist name required"
			return m, tea.Batch(cmds...)
		}
		m.status = "creating playlist…"
		cmds = append(cmds, m.createPlaylistCmd(title))
		return m, tea.Batch(cmds...)
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, tea.Batch(append(cmds, cmd)...)
}

func (m Model) createPlaylistCmd(title string) tea.Cmd {
	client := m.ytClient
	return func() tea.Msg {
		id, err := client.CreatePlaylist(title)
		return createPlaylistMsg{id: id, title: title, err: err}
	}
}

// --- Add to playlist -------------------------------------------------------

func (m Model) addToPlaylist() (Model, tea.Cmd) {
	trk, ok := m.targetTrack()
	if !ok {
		m.status = "no track selected"
		return m, nil
	}
	if trk.VideoID == "" {
		m.status = "this item has no playable video id"
		return m, nil
	}
	if len(m.playlists) == 0 {
		m.status = "loading playlists…"
		m.pendingPick = true
		m.pendingPickTrk = trk
		if !m.playlistsLoading {
			m.playlistsLoading = true
			return m, m.loadPlaylistsCmd()
		}
		return m, nil
	}
	m.openPicker(trk)
	return m, nil
}

func (m *Model) openPicker(trk ytmusic.Track) {
	m.pickTrack = trk
	m.pickList.Title = "Add to playlist: " + clip(trk.Title, m.width-24)
	m.pickList.SetItems(playlistsToItems(m.playlists))
	m.pickList.Select(0)
	m.pickMode = true
}

func (m Model) updatePickMode(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pickMode = false
		return m, tea.Batch(cmds...)
	case "enter":
		i := m.pickList.Index()
		if i >= 0 && i < len(m.playlists) {
			pl := m.playlists[i]
			trk := m.pickTrack
			m.pickMode = false
			if trk.VideoID == "" {
				m.status = "no track to add"
				return m, tea.Batch(cmds...)
			}
			m.status = "adding to " + pl.Title + "…"
			cmds = append(cmds, m.addToPlaylistItemsCmd(pl.PlaylistID, trk.VideoID))
		}
		return m, tea.Batch(cmds...)
	}
	var cmd tea.Cmd
	m.pickList, cmd = m.pickList.Update(msg)
	return m, tea.Batch(append(cmds, cmd)...)
}

func (m Model) addToPlaylistItemsCmd(playlistID, videoID string) tea.Cmd {
	client := m.ytClient
	return func() tea.Msg {
		status, err := client.AddPlaylistItems(playlistID, []string{videoID})
		return playlistEditMsg{action: "add", status: status, err: err, playlistID: playlistID}
	}
}

// --- Remove from playlist --------------------------------------------------

// playlistContext returns the ID of the playlist currently open, if the tab is
// showing one.
func (m Model) playlistContext() string {
	switch {
	case m.tab == TabPlaylists && m.inPlaylist:
		return m.playlistID
	case m.tab == TabPage && m.pageKind == pagePlaylist:
		return m.pagePlaylist
	}
	return ""
}

// confirmRemoveSelected asks for confirmation before removing the selected
// track from the open playlist.
func (m *Model) confirmRemoveSelected() {
	pid := m.playlistContext()
	if pid == "" {
		m.status = "open a playlist to remove tracks"
		return
	}
	l := m.activeListPtr()
	if l == nil {
		return
	}
	trk, ok := l.SelectedItem().(trackItem)
	if !ok {
		m.status = "select a track first"
		return
	}
	if trk.track.SetVideoID == "" {
		m.status = "can't remove: no setVideoId (not your playlist?)"
		return
	}
	m.confirmMode = true
	m.confirmPrompt = fmt.Sprintf("Remove %q from this playlist? [y/N]", clip(trk.track.Title, 40))
	m.confirmCmd = m.removePlaylistItemCmd(pid, trk.track)
}

func (m Model) removePlaylistItemCmd(playlistID string, trk ytmusic.Track) tea.Cmd {
	client := m.ytClient
	return func() tea.Msg {
		status, err := client.RemovePlaylistItems(playlistID, []ytmusic.Track{trk})
		return playlistEditMsg{
			action:         "remove",
			status:         status,
			err:            err,
			playlistID:     playlistID,
			removedVideoID: trk.VideoID,
		}
	}
}

func (m Model) updateConfirmMode(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		cmd := m.confirmCmd
		m.confirmMode = false
		m.confirmCmd = nil
		m.confirmPrompt = ""
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	default:
		m.confirmMode = false
		m.confirmCmd = nil
		m.confirmPrompt = ""
		m.status = "cancelled"
	}
	return m, tea.Batch(cmds...)
}

// removeTrackFromActivePlaylist drops a removed video from the open playlist's
// in-memory list so the UI reflects the change without a refetch.
func (m *Model) removeTrackFromActivePlaylist(playlistID, videoID string) {
	if playlistID == "" || videoID == "" {
		return
	}
	if m.tab == TabPlaylists && m.inPlaylist && m.playlistID == playlistID {
		removeVideoFromList(&m.trackList, videoID)
		m.playlistTracks = tracksFromItems(m.trackList.Items())
	}
	if m.tab == TabPage && m.pageKind == pagePlaylist && m.pagePlaylist == playlistID {
		removeVideoFromList(&m.pageList, videoID)
	}
}

// removeVideoFromList removes every trackItem with the given videoID.
func removeVideoFromList(l *list.Model, videoID string) {
	if l == nil {
		return
	}
	items := l.Items()
	out := make([]list.Item, 0, len(items))
	for _, it := range items {
		if t, ok := it.(trackItem); ok && t.track.VideoID == videoID {
			continue
		}
		out = append(out, it)
	}
	l.SetItems(out)
}

// --- small string helpers --------------------------------------------------

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func joinNonEmpty(sep string, vals ...string) string {
	var parts []string
	for _, v := range vals {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, sep)
}
