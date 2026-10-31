package ytmusic

import (
	"reflect"
	"testing"
)

// TestSearchParams checks every filter maps to a distinct, non-empty InnerTube
// params value and that the songs value is the one the client has always sent.
func TestSearchParams(t *testing.T) {
	if got := searchParams(FilterSongs); got != songsFilter {
		t.Errorf("songs params = %q, want %q", got, songsFilter)
	}
	seen := map[string]SearchFilter{}
	for _, f := range searchFilterOrder {
		p := searchParams(f)
		if p == "" {
			t.Errorf("filter %s has empty params", f.Label())
		}
		if other, ok := seen[p]; ok {
			t.Errorf("filters %s and %s share params %q", other.Label(), f.Label(), p)
		}
		seen[p] = f
	}
}

// TestSearchFilterCycle checks Next/Prev wrap around the cycle order.
func TestSearchFilterCycle(t *testing.T) {
	order := searchFilterOrder
	for i, f := range order {
		wantNext := order[(i+1)%len(order)]
		wantPrev := order[((i-1)%len(order)+len(order))%len(order)]
		if got := f.Next(); got != wantNext {
			t.Errorf("%s.Next() = %s, want %s", f.Label(), got.Label(), wantNext.Label())
		}
		if got := f.Prev(); got != wantPrev {
			t.Errorf("%s.Prev() = %s, want %s", f.Label(), got.Label(), wantPrev.Label())
		}
	}
	if !FilterSongs.ReturnsTracks() || !FilterVideos.ReturnsTracks() {
		t.Error("songs/videos should return tracks")
	}
	if FilterAlbums.ReturnsTracks() || FilterArtists.ReturnsTracks() || FilterPlaylists.ReturnsTracks() {
		t.Error("album/artist/playlist filters should return browse items")
	}
}

// browseRow builds a musicResponsiveListItemRenderer for a browse tile with a
// top-level navigationEndpoint and title/subtitle runs.
func browseRow(title, subtitle, browseID, pageType string) map[string]any {
	return map[string]any{
		"navigationEndpoint": map[string]any{
			"browseEndpoint": map[string]any{
				"browseId": browseID,
				"browseEndpointContextSupportedConfigs": map[string]any{
					"browseEndpointContextMusicConfig": map[string]any{"pageType": pageType},
				},
			},
		},
		"flexColumns": []any{
			map[string]any{"musicResponsiveListItemFlexColumnRenderer": map[string]any{
				"text": map[string]any{"runs": []any{map[string]any{"text": title}}},
			}},
			map[string]any{"musicResponsiveListItemFlexColumnRenderer": map[string]any{
				"text": map[string]any{"runs": []any{map[string]any{"text": subtitle}}},
			}},
		},
	}
}

// TestParseBrowseItems covers album/artist/playlist tile parsing.
func TestParseBrowseItems(t *testing.T) {
	items := []any{
		browseRow("Album X", "Artist • 2020", "MPREb_album", "MUSIC_PAGE_TYPE_ALBUM"),
		browseRow("Artist Y", "1.2M subscribers", "UCartist", "MUSIC_PAGE_TYPE_ARTIST"),
		browseRow("My Playlist", "Auto playlist", "VLPL123", "MUSIC_PAGE_TYPE_PLAYLIST"),
	}

	albums := parseBrowseItems(items, KindAlbum)
	if len(albums) != 1 || albums[0].ID != "MPREb_album" || albums[0].Title != "Album X" {
		t.Fatalf("album parse = %+v, want one MPREb_album/Album X", albums)
	}
	if albums[0].Subtitle != "Artist • 2020" {
		t.Errorf("album subtitle = %q", albums[0].Subtitle)
	}

	artists := parseBrowseItems(items, KindArtist)
	if len(artists) != 1 || artists[0].ID != "UCartist" || artists[0].Kind != KindArtist {
		t.Fatalf("artist parse = %+v", artists)
	}

	playlists := parseBrowseItems(items, KindPlaylist)
	if len(playlists) != 1 || playlists[0].ID != "VLPL123" {
		t.Fatalf("playlist parse = %+v", playlists)
	}
}

// TestParseBrowseItemsIgnoresNestedArtistLink ensures a song row (whose only
// browse endpoint is the artist's, nested in a metadata column) is not mistaken
// for an album tile.
func TestParseBrowseItemsIgnoresNestedArtistLink(t *testing.T) {
	song := mustJSON(t, `{
		"musicResponsiveListItemRenderer": {
			"playlistItemData": {"videoId": "vidA"},
			"flexColumns": [
				{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Song A"}]}}},
				{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Artist A",
					"navigationEndpoint": {"browseEndpoint": {"browseId": "UCnested",
						"browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig":
							{"pageType": "MUSIC_PAGE_TYPE_ARTIST"}}}}}]}}}
			]
		}
	}`)
	if got := parseBrowseItems([]any{song["musicResponsiveListItemRenderer"]}, KindAlbum); len(got) != 0 {
		t.Errorf("song row parsed as album: %+v", got)
	}
}

// TestParseTrackItemsIDs checks artist/album browse IDs and setVideoId are
// captured from a playlist row.
func TestParseTrackItemsIDs(t *testing.T) {
	item := mustJSON(t, `{
		"musicResponsiveListItemRenderer": {
			"playlistItemData": {"videoId": "vidA"},
			"menu": {"menuRenderer": {"items": [
				{"menuServiceItemRenderer": {"serviceEndpoint": {"playlistEditEndpoint": {
					"actions": [{"setVideoId": "SET1", "removedVideoId": "vidA"}]
				}}}}
			]}},
			"flexColumns": [
				{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Song A"}]}}},
				{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [
					{"text": "Artist A", "navigationEndpoint": {"browseEndpoint": {"browseId": "UC123",
						"browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig":
							{"pageType": "MUSIC_PAGE_TYPE_ARTIST"}}}}},
					{"text": " • "},
					{"text": "Album A", "navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_a",
						"browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig":
							{"pageType": "MUSIC_PAGE_TYPE_ALBUM"}}}}}
				]}}}
			]
		}
	}`)
	got := parseTrackItems([]any{item["musicResponsiveListItemRenderer"]})
	if len(got) != 1 {
		t.Fatalf("got %d tracks, want 1", len(got))
	}
	tr := got[0]
	if tr.ArtistID != "UC123" {
		t.Errorf("artistId = %q, want UC123", tr.ArtistID)
	}
	if tr.AlbumID != "MPREb_a" {
		t.Errorf("albumId = %q, want MPREb_a", tr.AlbumID)
	}
	if tr.Album != "Album A" {
		t.Errorf("album = %q, want Album A", tr.Album)
	}
	if tr.SetVideoID != "SET1" {
		t.Errorf("setVideoId = %q, want SET1", tr.SetVideoID)
	}
}

// TestEditPlaylistBody checks the ADD request body shape and VL stripping.
func TestEditPlaylistBody(t *testing.T) {
	body, err := editPlaylistBody("VLPL123", []string{"v1", "v2"})
	if err != nil {
		t.Fatalf("editPlaylistBody: %v", err)
	}
	if body["playlistId"] != "PL123" {
		t.Errorf("playlistId = %v, want PL123", body["playlistId"])
	}
	want := []any{
		map[string]any{"action": "ACTION_ADD_VIDEO", "addedVideoId": "v1", "dedupeOption": "DEDUPE_OPTION_SKIP"},
		map[string]any{"action": "ACTION_ADD_VIDEO", "addedVideoId": "v2", "dedupeOption": "DEDUPE_OPTION_SKIP"},
	}
	if !reflect.DeepEqual(body["actions"], want) {
		t.Errorf("actions = %#v, want %#v", body["actions"], want)
	}

	if _, err := editPlaylistBody("", []string{"v1"}); err == nil {
		t.Error("empty playlist id should error")
	}
	if _, err := editPlaylistBody("PL1", nil); err == nil {
		t.Error("no video ids should error")
	}
}

// TestRemovePlaylistBody checks the REMOVE request body shape.
func TestRemovePlaylistBody(t *testing.T) {
	body, err := removePlaylistBody("PL123", []playlistRemoval{
		{VideoID: "v1", SetVideoID: "SET1"},
		{VideoID: "v2", SetVideoID: ""}, // skipped: no setVideoId
	})
	if err != nil {
		t.Fatalf("removePlaylistBody: %v", err)
	}
	want := []any{
		map[string]any{"action": "ACTION_REMOVE_VIDEO", "removedVideoId": "v1", "setVideoId": "SET1"},
	}
	if !reflect.DeepEqual(body["actions"], want) {
		t.Errorf("actions = %#v, want %#v", body["actions"], want)
	}
	if _, err := removePlaylistBody("PL1", []playlistRemoval{{VideoID: "v1"}}); err == nil {
		t.Error("removal without setVideoId should error")
	}
}

// TestParseAlbumHeader checks album header title/artist/year extraction,
// including that a leading type label ("Album •") is not mistaken for artist.
func TestParseAlbumHeader(t *testing.T) {
	root := mustJSON(t, `{
		"musicResponsiveHeaderRenderer": {
			"title": {"runs": [{"text": "Great Album"}]},
			"subtitle": {"runs": [
				{"text": "Album"}, {"text": " • "}, {"text": "The Artist"}, {"text": " • "}, {"text": "2021"}
			]}
		}
	}`)
	a := parseAlbumHeader(root, "MPREb_x")
	if a.Title != "Great Album" || a.Artist != "The Artist" || a.Year != "2021" {
		t.Errorf("album header = %+v", a)
	}
}

// TestParseArtistAlbumsOnlyTiles checks only two-row album tiles are returned.
func TestParseArtistAlbumsOnlyTiles(t *testing.T) {
	root := mustJSON(t, `{
		"musicCarouselShelfRenderer": {"contents": [
			{"musicTwoRowItemRenderer": {
				"navigationEndpoint": {"browseEndpoint": {"browseId": "MPREb_one",
					"browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig":
						{"pageType": "MUSIC_PAGE_TYPE_ALBUM"}}}},
				"title": {"runs": [{"text": "Album One"}]}
			}}
		]},
		"musicShelfRenderer": {"contents": [
			`+trackItem("Song A", "Artist A", "vidA")+`
		]}
	}`)
	albums := parseArtistAlbums(root)
	if len(albums) != 1 || albums[0].ID != "MPREb_one" || albums[0].Title != "Album One" {
		t.Fatalf("artist albums = %+v, want one MPREb_one/Album One", albums)
	}
}
