package app

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

func TestTracksFromItemsAndIndex(t *testing.T) {
	items := []list.Item{
		browseItem{ytmusic.BrowseItem{ID: "a", Kind: ytmusic.KindAlbum}},
		trackItem{track: ytmusic.Track{VideoID: "v1", Title: "One"}},
		trackItem{track: ytmusic.Track{VideoID: "v2", Title: "Two"}},
	}
	tracks := tracksFromItems(items)
	if len(tracks) != 2 || tracks[0].VideoID != "v1" || tracks[1].VideoID != "v2" {
		t.Fatalf("tracksFromItems = %+v", tracks)
	}
	// Index is into the track-only slice, not the raw item slice.
	if idx := indexOfTrack(items, "v2"); idx != 1 {
		t.Errorf("indexOfTrack(v2) = %d, want 1", idx)
	}
	if idx := indexOfTrack(items, "nope"); idx != -1 {
		t.Errorf("indexOfTrack(nope) = %d, want -1", idx)
	}
}

func TestOpenBrowseItemSetsPage(t *testing.T) {
	cases := []struct {
		kind   string
		wantPK pageKind
	}{
		{ytmusic.KindAlbum, pageAlbum},
		{ytmusic.KindArtist, pageArtist},
		{ytmusic.KindPlaylist, pagePlaylist},
	}
	for _, tc := range cases {
		m := Model{tab: TabLibrary, pageList: newList("")}
		got, cmd := m.openBrowseItem(ytmusic.BrowseItem{ID: "id1", Title: "T", Kind: tc.kind})
		if got.tab != TabPage {
			t.Errorf("%s: tab = %v, want TabPage", tc.kind, got.tab)
		}
		if got.pageKind != tc.wantPK {
			t.Errorf("%s: pageKind = %v, want %v", tc.kind, got.pageKind, tc.wantPK)
		}
		if got.pageOrigin != TabLibrary {
			t.Errorf("%s: pageOrigin = %v, want TabLibrary", tc.kind, got.pageOrigin)
		}
		if cmd == nil {
			t.Errorf("%s: expected a load command", tc.kind)
		}
	}
}

func TestHandleSelectBrowseItemOpensPage(t *testing.T) {
	m := Model{tab: TabSearch, searchList: newList(""), pageList: newList("")}
	m.searchList.SetItems(browseToItems([]ytmusic.BrowseItem{
		{ID: "MPREb_x", Title: "Album", Kind: ytmusic.KindAlbum},
	}))
	m.searchList.Select(0)

	got, cmd := m.handleSelect()
	if got.tab != TabPage || got.pageKind != pageAlbum {
		t.Fatalf("handleSelect browse = tab %v kind %v, want TabPage/album", got.tab, got.pageKind)
	}
	if cmd == nil {
		t.Error("expected an album load command")
	}
}

func TestSearchResultMsgPopulatesBrowseList(t *testing.T) {
	m := Model{tab: TabSearch, searchList: newList("")}
	updated, _ := m.Update(searchResultMsg{
		filter: ytmusic.FilterAlbums,
		items: []ytmusic.BrowseItem{
			{ID: "MPREb_1", Title: "A", Kind: ytmusic.KindAlbum},
			{ID: "MPREb_2", Title: "B", Kind: ytmusic.KindAlbum},
		},
	})
	um := updated.(Model)
	if um.searchFilter != ytmusic.FilterAlbums {
		t.Errorf("searchFilter = %v, want albums", um.searchFilter)
	}
	if n := len(um.searchList.Items()); n != 2 {
		t.Errorf("search list items = %d, want 2", n)
	}
}

func TestRemoveVideoFromList(t *testing.T) {
	l := newList("")
	l.SetItems([]list.Item{
		trackItem{track: ytmusic.Track{VideoID: "v1", Title: "One"}},
		trackItem{track: ytmusic.Track{VideoID: "v2", Title: "Two"}},
		trackItem{track: ytmusic.Track{VideoID: "v3", Title: "Three"}},
	})
	removeVideoFromList(&l, "v2")
	items := l.Items()
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	for _, it := range items {
		if it.(trackItem).track.VideoID == "v2" {
			t.Error("v2 was not removed")
		}
	}
}

func TestListNavigation(t *testing.T) {
	m := Model{tab: TabLibrary, libraryList: newList("")}
	tracks := make([]ytmusic.Track, 100)
	for i := range tracks {
		tracks[i] = ytmusic.Track{VideoID: string(rune('A' + i%26)), Title: "T"}
	}
	m.libraryList.SetItems(tracksToItems(tracks))
	m.libraryList.SetHeight(12)

	m.listToBottom()
	if got := m.libraryList.Index(); got != 99 {
		t.Errorf("listToBottom index = %d, want 99", got)
	}
	m.listToTop()
	if got := m.libraryList.Index(); got != 0 {
		t.Errorf("listToTop index = %d, want 0", got)
	}
	m.listPage(1)
	if got := m.libraryList.Index(); got <= 0 || got >= 100 {
		t.Errorf("listPage(1) index = %d, want within (0,100)", got)
	}
	first := m.libraryList.Index()
	m.listPage(-1)
	if got := m.libraryList.Index(); got != 0 {
		t.Errorf("listPage(-1) index = %d, want 0", got)
	}
	if first == 0 {
		t.Error("page down did not move the cursor")
	}
}

func TestPlaybackSource(t *testing.T) {
	m := Model{tab: TabPage, pageKind: pagePlaylist, pageTitle: "My List"}
	if got := m.playbackSource(); got != "My List" {
		t.Errorf("page playlist source = %q, want My List", got)
	}
	m = Model{tab: TabPlaylists, inPlaylist: true, trackList: func() list.Model {
		l := newList("Chill")
		return l
	}()}
	if got := m.playbackSource(); got != "Chill" {
		t.Errorf("playlists-tab source = %q, want Chill", got)
	}
	if got := (Model{tab: TabLibrary}).playbackSource(); got != "" {
		t.Errorf("library source = %q, want empty", got)
	}
}
