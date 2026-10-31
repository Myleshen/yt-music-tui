package ytmusic

import (
	"fmt"
	"testing"
)

// twoRowTile builds a musicTwoRowItemRenderer fixture for a playlist/album
// tile. pageType may be empty to omit the page type.
func twoRowTile(title, subtitle, browseID, pageType, thumb string) string {
	pt := ""
	if pageType != "" {
		pt = fmt.Sprintf(`"browseEndpointContextSupportedConfigs": {"browseEndpointContextMusicConfig": {"pageType": %q}}`, pageType)
	}
	thumbJSON := ""
	if thumb != "" {
		thumbJSON = fmt.Sprintf(`"thumbnailRenderer": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
			{"url": %q, "width": 60}, {"url": %q, "width": 544}]}}},`, thumb+"/s.jpg", thumb+"/l.jpg")
	}
	return fmt.Sprintf(`{"musicTwoRowItemRenderer": {
		%[1]s
		"title": {"runs": [{"text": %[2]q}]},
		"subtitle": {"runs": [{"text": %[3]q}]},
		"navigationEndpoint": {"browseEndpoint": {"browseId": %[4]q, %[5]s}}
	}}`, thumbJSON, title, subtitle, browseID, pt)
}

// carousel wraps items in a musicCarouselShelfRenderer with a header title.
func carousel(title string, items ...string) string {
	joined := ""
	for i, it := range items {
		if i > 0 {
			joined += ","
		}
		joined += it
	}
	return fmt.Sprintf(`{"musicCarouselShelfRenderer": {
		"header": {"musicCarouselShelfBasicHeaderRenderer": {"title": {"runs": [{"text": %q}]}}},
		"contents": [%s]
	}}`, title, joined)
}

// sectionList wraps section nodes in a single-column section list.
func sectionList(nodes ...string) string {
	joined := ""
	for i, n := range nodes {
		if i > 0 {
			joined += ","
		}
		joined += n
	}
	return fmt.Sprintf(`{"contents": {"singleColumnBrowseResultsRenderer": {"tabs": [
		{"tabRenderer": {"content": {"sectionListRenderer": {"contents": [%s]}}}}
	]}}}`, joined)
}

// TestParseSectionsCarousel checks a Home-style carousel parses into one titled
// section of collections, preserving tile order and dropping artist tiles.
func TestParseSectionsCarousel(t *testing.T) {
	root := mustJSON(t, sectionList(carousel("Listen again",
		twoRowTile("Chill Hits", "Playlist • YT Music", "VLPLchill", "MUSIC_PAGE_TYPE_PLAYLIST", "https://img/chill"),
		twoRowTile("Some Artist", "Artist", "UCartist", "MUSIC_PAGE_TYPE_ARTIST", ""),
		twoRowTile("New Album", "Album • Someone", "MPREb_abc", "MUSIC_PAGE_TYPE_ALBUM", "https://img/album"),
	)))

	sections := parseSections(root)
	if len(sections) != 1 {
		t.Fatalf("got %d sections, want 1: %+v", len(sections), sections)
	}
	s := sections[0]
	if s.Title != "Listen again" {
		t.Errorf("title = %q, want %q", s.Title, "Listen again")
	}
	if len(s.Tracks) != 0 {
		t.Errorf("tracks = %d, want 0", len(s.Tracks))
	}
	if len(s.Collections) != 2 {
		t.Fatalf("collections = %d, want 2 (artist dropped): %+v", len(s.Collections), s.Collections)
	}
	first := s.Collections[0]
	if first.Title != "Chill Hits" || first.BrowseID != "VLPLchill" || first.PlaylistID != "PLchill" {
		t.Errorf("first collection = %+v", first)
	}
	if first.Subtitle != "Playlist • YT Music" {
		t.Errorf("subtitle = %q", first.Subtitle)
	}
	if first.Thumbnail != "https://img/chill/l.jpg" {
		t.Errorf("thumbnail = %q, want largest", first.Thumbnail)
	}
	if s.Collections[1].BrowseID != "MPREb_abc" || s.Collections[1].PlaylistID != "" {
		t.Errorf("album collection = %+v", s.Collections[1])
	}
}

// TestParseSectionsShelf checks a musicShelfRenderer parses into tracks.
func TestParseSectionsShelf(t *testing.T) {
	root := mustJSON(t, sectionList(
		`{"musicShelfRenderer": {"title": {"runs": [{"text": "Songs"}]}, "contents": [`+
			trackItem("Song A", "Artist A", "vidA")+`,`+
			trackItem("Song B", "Artist B", "vidB")+`]}}`))

	sections := parseSections(root)
	if len(sections) != 1 {
		t.Fatalf("got %d sections, want 1", len(sections))
	}
	s := sections[0]
	if s.Title != "Songs" {
		t.Errorf("title = %q", s.Title)
	}
	if len(s.Tracks) != 2 || s.Tracks[0].VideoID != "vidA" || s.Tracks[1].VideoID != "vidB" {
		t.Errorf("tracks = %+v", s.Tracks)
	}
}

// TestParseSectionsGrid checks a grid of mood/genre navigation buttons parses
// into collections carrying the category browse id and params.
func TestParseSectionsGrid(t *testing.T) {
	root := mustJSON(t, sectionList(
		`{"gridRenderer": {"header": {"gridHeaderRenderer": {"title": {"runs": [{"text": "Genres"}]}}}, "items": [`+
			`{"musicNavigationButtonRenderer": {"buttonText": {"runs": [{"text": "Chill"}]}, "clickCommand": {"browseEndpoint": {"browseId": "FEmusic_moods_and_genres_category", "params": "ggMPOg1uX1JO"}}}},`+
			`{"musicNavigationButtonRenderer": {"buttonText": {"runs": [{"text": "Workout"}]}, "clickCommand": {"browseEndpoint": {"browseId": "FEmusic_moods_and_genres_category", "params": "ggMPOg1uX1dv"}}}}`+
			`]}}`))

	sections := parseSections(root)
	if len(sections) != 1 {
		t.Fatalf("got %d sections, want 1", len(sections))
	}
	s := sections[0]
	if s.Title != "Genres" {
		t.Errorf("title = %q", s.Title)
	}
	if len(s.Collections) != 2 {
		t.Fatalf("collections = %d, want 2", len(s.Collections))
	}
	if s.Collections[0].Title != "Chill" || s.Collections[0].BrowseID != "FEmusic_moods_and_genres_category" || s.Collections[0].Params != "ggMPOg1uX1JO" {
		t.Errorf("first mood = %+v", s.Collections[0])
	}
}

// TestParseSectionsDescription checks a description shelf yields a text section.
func TestParseSectionsDescription(t *testing.T) {
	root := mustJSON(t, sectionList(
		`{"musicDescriptionShelfRenderer": {"header": {"runs": [{"text": "About"}]}, "description": {"runs": [{"text": "Hello "}, {"text": "world"}]}}}`))

	sections := parseSections(root)
	if len(sections) != 1 {
		t.Fatalf("got %d sections, want 1", len(sections))
	}
	if sections[0].Title != "About" || sections[0].Description != "Hello world" {
		t.Errorf("description section = %+v", sections[0])
	}
}

// TestParseSectionsPlaylistSecondary checks a two-column playlist page: the
// header is skipped and the secondary playlist shelf becomes a track section.
func TestParseSectionsPlaylistSecondary(t *testing.T) {
	root := mustJSON(t, `{"contents": {"twoColumnBrowseResultsRenderer": {
		"tabs": [{"tabRenderer": {"content": {"sectionListRenderer": {"contents": [
			{"musicEditablePlaylistDetailHeaderRenderer": {"header": {"musicResponsiveHeaderRenderer": {"title": {"runs": [{"text": "My Mix"}]}}}}}
		]}}}}],
		"secondaryContents": {"sectionListRenderer": {"contents": [
			{"musicPlaylistShelfRenderer": {"contents": [`+
		trackItem("Song A", "Artist A", "vidA")+`,`+
		trackItem("Song B", "Artist B", "vidB")+`]}}
		]}}
	}}}`)

	sections := parseSections(root)
	if len(sections) != 1 {
		t.Fatalf("got %d sections, want 1: %+v", len(sections), sections)
	}
	if len(sections[0].Tracks) != 2 {
		t.Fatalf("tracks = %d, want 2", len(sections[0].Tracks))
	}
	if got := collectionTitle(root); got != "My Mix" {
		t.Errorf("collectionTitle = %q, want My Mix", got)
	}
}
