package ytmusic

import (
	"fmt"
	"strings"
)

// Discover browse IDs (anonymous or authenticated). These are the InnerTube
// "rich grid" pages behind the YouTube Music Home and Explore tabs.
const (
	browseHome         = "FEmusic_home"
	browseMoods        = "FEmusic_moods_and_genres"
	browseMoodCategory = "FEmusic_moods_and_genres_category"
	browseCharts       = "FEmusic_charts"
	browseNewReleases  = "FEmusic_new_releases"
)

// GetHome returns the sections of the YouTube Music home feed
// (FEmusic_home): carousels and shelves of playlists, albums, and — on some
// accounts — tracks.
func (c *Client) GetHome() ([]Section, error) { return c.browseSections(browseHome) }

// GetCharts returns the charts sections (FEmusic_charts): daily/weekly charts,
// languages, and top artists.
func (c *Client) GetCharts() ([]Section, error) { return c.browseSections(browseCharts) }

// GetNewReleases returns the new-releases sections (FEmusic_new_releases).
func (c *Client) GetNewReleases() ([]Section, error) { return c.browseSections(browseNewReleases) }

// GetMoods returns the moods & genres landing sections (FEmusic_moods_and_genres).
// Each returned collection is a category tile (BrowseID FEmusic_moods_and_genres_category
// with a Params token) to be opened with GetMoodPlaylists.
func (c *Client) GetMoods() ([]Section, error) { return c.browseSections(browseMoods) }

// GetMoodPlaylists drills into a moods & genres category, returning the
// carousels of playlists it contains.
func (c *Client) GetMoodPlaylists(browseID, params string) ([]Section, error) {
	return c.GetCollection(browseID, params, "")
}

// GetCollection opens any browse tile — a playlist, album, mix, podcast, or a
// moods/genres category — and returns its contents as sections. When the
// response contains tracks (a playlist or album) they are returned as a single
// titled section; otherwise the nested carousels/grids are returned so callers
// can drill further.
func (c *Client) GetCollection(browseID, params, title string) ([]Section, error) {
	if browseID == "" {
		return nil, fmt.Errorf("missing browse id")
	}
	body := map[string]any{"browseId": browseID}
	if params != "" {
		body["params"] = params
	}
	root, err := c.request("browse", body)
	if err != nil {
		return nil, err
	}
	sections := parseSections(root)
	var tracks []Track
	for _, s := range sections {
		tracks = append(tracks, s.Tracks...)
	}
	if len(tracks) > 0 {
		if title == "" {
			title = collectionTitle(root)
		}
		return []Section{{Title: title, Tracks: tracks}}, nil
	}
	return sections, nil
}

// browseSections fetches a browse page and parses its titled sections.
func (c *Client) browseSections(browseID string) ([]Section, error) {
	root, err := c.request("browse", map[string]any{"browseId": browseID})
	if err != nil {
		return nil, err
	}
	return parseSections(root), nil
}

// --- parsing ---------------------------------------------------------------

// parseSections walks the section lists of a browse response and returns the
// titled sections in document order. Empty sections (e.g. the charts
// "Top artists" list, whose rows have no videoId) are dropped.
func parseSections(root any) []Section {
	var out []Section
	for _, node := range sectionListContents(root) {
		s, ok := parseSectionNode(node)
		if !ok {
			continue
		}
		if len(s.Tracks) == 0 && len(s.Collections) == 0 && s.Description == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		// Some responses nest their tracks without a recognisable
		// sectionListRenderer; fall back to the raw renderers.
		tracks := parseTrackItems(findAll(root, "musicResponsiveListItemRenderer"))
		cols := parseCollections(findAll(root, "musicTwoRowItemRenderer"))
		if len(tracks) > 0 || len(cols) > 0 {
			out = append(out, Section{Tracks: tracks, Collections: cols})
		}
	}
	return out
}

// sectionListContents collects the content nodes of every section list we know
// how to find: the single-column layout used by Home/Explore, and the primary
// and secondary columns of a two-column layout used by playlist/album pages.
func sectionListContents(root any) []any {
	var out []any
	for _, tabs := range []any{
		dig(root, "contents", "singleColumnBrowseResultsRenderer", "tabs"),
		dig(root, "contents", "twoColumnBrowseResultsRenderer", "tabs"),
	} {
		for _, tab := range asSlice(tabs) {
			out = append(out, asSlice(dig(tab, "tabRenderer", "content", "sectionListRenderer", "contents"))...)
		}
	}
	out = append(out, asSlice(dig(root,
		"contents", "twoColumnBrowseResultsRenderer", "secondaryContents",
		"sectionListRenderer", "contents"))...)
	return out
}

// parseSectionNode interprets a single section-list content node. Each node is
// a wrapper with exactly one renderer key.
func parseSectionNode(node any) (Section, bool) {
	m, ok := node.(map[string]any)
	if !ok {
		return Section{}, false
	}
	if r, ok := m["musicCarouselShelfRenderer"].(map[string]any); ok {
		title := runsText(dig(r, "header", "musicCarouselShelfBasicHeaderRenderer", "title", "runs"))
		return buildSection(title, asSlice(dig(r, "contents"))), true
	}
	if r, ok := m["musicShelfRenderer"].(map[string]any); ok {
		title := runsText(dig(r, "title", "runs"))
		if title == "" {
			title = runsText(dig(r, "header", "musicHeaderRenderer", "title", "runs"))
		}
		return buildSection(title, asSlice(dig(r, "contents"))), true
	}
	if r, ok := m["musicPlaylistShelfRenderer"].(map[string]any); ok {
		title := runsText(dig(r, "title", "runs"))
		return buildSection(title, asSlice(dig(r, "contents"))), true
	}
	if r, ok := m["gridRenderer"].(map[string]any); ok {
		title := runsText(dig(r, "header", "gridHeaderRenderer", "title", "runs"))
		if title == "" {
			title = runsText(dig(r, "header", "musicHeaderRenderer", "title", "runs"))
		}
		return buildSection(title, asSlice(dig(r, "items"))), true
	}
	if r, ok := m["musicDescriptionShelfRenderer"].(map[string]any); ok {
		title := runsText(dig(r, "header", "runs"))
		if title == "" {
			title = str(dig(r, "header", "simpleText"))
		}
		return Section{Title: title, Description: runsText(dig(r, "description", "runs"))}, true
	}
	return Section{}, false
}

// buildSection extracts the playable tracks and collections from the items of
// a carousel/shelf/grid. Track order and tile order are preserved.
func buildSection(title string, items []any) Section {
	s := Section{Title: title}
	s.Tracks = parseTrackItems(rendererMaps(items, "musicResponsiveListItemRenderer"))
	s.Collections = parseCollections(rendererMaps(items, "musicTwoRowItemRenderer"))
	s.Collections = append(s.Collections,
		parseCollections(rendererMaps(items, "musicNavigationButtonRenderer"))...)
	return s
}

// rendererMaps unwraps the inner renderer map of each list item whose sole
// renderer key matches key. It preserves item order, unlike findAll, whose
// traversal order within a map is undefined.
func rendererMaps(items []any, key string) []any {
	var out []any
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if v, ok := m[key]; ok {
			out = append(out, v)
		}
	}
	return out
}

// parseCollections converts two-row tiles and mood/genre navigation buttons
// into Collections, dropping artist/channel tiles (those are browsed elsewhere).
func parseCollections(items []any) []Collection {
	var out []Collection
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if c, ok := parseCollectionItem(m); ok {
			out = append(out, c)
		}
	}
	return out
}

// parseCollectionItem builds a Collection from a musicTwoRowItemRenderer or
// musicNavigationButtonRenderer node.
func parseCollectionItem(m map[string]any) (Collection, bool) {
	title := itemTitle(m)
	if title == "" {
		title = runsText(dig(m, "buttonText", "runs"))
	}
	if title == "" {
		return Collection{}, false
	}

	be := dig(m, "navigationEndpoint", "browseEndpoint")
	if be == nil {
		be = dig(m, "clickCommand", "browseEndpoint")
	}
	browseID := str(dig(be, "browseId"))
	pageType := str(dig(be, "browseEndpointContextSupportedConfigs",
		"browseEndpointContextMusicConfig", "pageType"))
	// Artist and channel tiles open artist pages, which another view owns.
	if strings.Contains(pageType, "ARTIST") || strings.Contains(pageType, "USER_CHANNEL") {
		return Collection{}, false
	}

	return Collection{
		BrowseID:   browseID,
		Params:     str(dig(be, "params")),
		PlaylistID: collectionPlaylistID(m, browseID),
		Title:      title,
		Subtitle:   runsText(dig(m, "subtitle", "runs")),
		Thumbnail:  itemThumbnail(m, ""),
	}, true
}

// collectionPlaylistID resolves the playlist id of a tile: a "VL"-prefixed
// browse id, or a playlistId found elsewhere in the renderer (e.g. its menu).
func collectionPlaylistID(m map[string]any, browseID string) string {
	if strings.HasPrefix(browseID, "VL") {
		return strings.TrimPrefix(browseID, "VL")
	}
	return findString(m, "playlistId")
}

// collectionTitle reads the header title of a playlist/album browse response.
func collectionTitle(root any) string {
	for _, node := range sectionListContents(root) {
		if t := runsText(dig(node, "musicResponsiveHeaderRenderer", "title", "runs")); t != "" {
			return t
		}
		if t := runsText(dig(node,
			"musicEditablePlaylistDetailHeaderRenderer", "header",
			"musicResponsiveHeaderRenderer", "title", "runs")); t != "" {
			return t
		}
	}
	return ""
}
