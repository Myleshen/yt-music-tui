package ytmusic

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Internal YouTube Music ("InnerTube") endpoints and client identity.
// WEB_REMIX is the YouTube Music web client.
const (
	apiBase       = "https://music.youtube.com/youtubei/v1/"
	apiKey        = "AIzaSyC9XL3ZjWddXya6X74dJoCTL-WEYFDNX30"
	clientName    = "WEB_REMIX"
	clientVersion = "1.20240101.00.00"
	clientID      = "67"
	origin        = "https://music.youtube.com"
	userAgent     = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	// songsFilter selects the "Songs" tab on search.
	songsFilter = "EgWKAQIIAWoKEAkQBRAKEAMQBA%3D%3D"

	// browse IDs for the authenticated library.
	browseLibraryPlaylists = "FEmusic_liked_playlists"
	browseLikedSongs       = "VLLM"

	// InnerTube endpoints for playlist mutation.
	endpointPlaylistCreate = "playlist/create"
	endpointEditPlaylist   = "browse/edit_playlist"
)

// SearchFilter selects which result kind a search returns.
type SearchFilter int

const (
	// FilterSongs returns individual songs (the default).
	FilterSongs SearchFilter = iota
	// FilterAlbums returns album tiles.
	FilterAlbums
	// FilterArtists returns artist tiles.
	FilterArtists
	// FilterPlaylists returns playlist tiles.
	FilterPlaylists
	// FilterVideos returns music videos.
	FilterVideos
)

// searchFilterOrder is the cycle order offered in the UI.
var searchFilterOrder = []SearchFilter{
	FilterSongs, FilterAlbums, FilterArtists, FilterPlaylists, FilterVideos,
}

// filterParams maps each filter to its InnerTube search params value. These are
// the standard YouTube Music filter protobufs (base64, URL-encoded). Every value
// shares the same trailing bytes as the long-standing songs filter — only the
// filter selector differs — so behaviour matches the known-good songs search.
var filterParams = map[SearchFilter]string{
	FilterSongs:     songsFilter,
	FilterAlbums:    "EgWKAQIYAWoKEAkQBRAKEAMQBA%3D%3D",
	FilterArtists:   "EgWKAQIgAWoKEAkQBRAKEAMQBA%3D%3D",
	FilterPlaylists: "EgWKAQIoAWoKEAkQBRAKEAMQBA%3D%3D",
	FilterVideos:    "EgWKAQIQAWoKEAkQBRAKEAMQBA%3D%3D",
}

// searchParams returns the InnerTube params value for a filter (pure).
func searchParams(f SearchFilter) string { return filterParams[f] }

// Next returns the filter after f in the cycle order.
func (f SearchFilter) Next() SearchFilter { return f.step(1) }

// Prev returns the filter before f in the cycle order.
func (f SearchFilter) Prev() SearchFilter { return f.step(-1) }

func (f SearchFilter) step(delta int) SearchFilter {
	idx := 0
	for i, cand := range searchFilterOrder {
		if cand == f {
			idx = i
			break
		}
	}
	n := len(searchFilterOrder)
	idx = ((idx+delta)%n + n) % n
	return searchFilterOrder[idx]
}

// Label is a short human label for the filter.
func (f SearchFilter) Label() string {
	switch f {
	case FilterAlbums:
		return "Albums"
	case FilterArtists:
		return "Artists"
	case FilterPlaylists:
		return "Playlists"
	case FilterVideos:
		return "Videos"
	default:
		return "Songs"
	}
}

// ReturnsTracks reports whether the filter yields track rows rather than
// navigable browse tiles.
func (f SearchFilter) ReturnsTracks() bool {
	return f == FilterSongs || f == FilterVideos
}

// browseKind maps a tile-returning filter to its BrowseItem kind.
func (f SearchFilter) browseKind() string {
	switch f {
	case FilterAlbums:
		return KindAlbum
	case FilterArtists:
		return KindArtist
	case FilterPlaylists:
		return KindPlaylist
	case FilterVideos:
		return KindVideo
	default:
		return KindAlbum
	}
}

// Client is a native Go YouTube Music client. It speaks the InnerTube API
// directly — no Python, no external processes. When cookie is non-empty the
// client authenticates with a SAPISIDHASH header derived from the pasted
// browser cookie.
type Client struct {
	hc     *http.Client
	cookie string // raw "Cookie:" header value (empty = anonymous)
	hl     string
	gl     string
	// baseURL is the InnerTube base (defaults to apiBase). It is a field so
	// tests can point the client at an httptest server.
	baseURL string
}

var (
	sapisidRe = regexp.MustCompile(`(?:^|;\s*)SAPISID=([^;]+)`)
	durRe     = regexp.MustCompile(`^\d+:\d{2}(?::\d{2})?$`)
	// countRe matches a track count like "50 songs", "12 tracks", etc.
	countRe = regexp.MustCompile(`(\d+)\s*(?:song|track|video|episode)s?`)
	// runSepRe splits "Artist • 2020" style header runs.
	runSepRe = regexp.MustCompile(`[•·]`)
	// yearRe matches a four-digit release year.
	yearRe = regexp.MustCompile(`^(19|20)\d{2}$`)
)

// maxItems caps how many entries pagination will accumulate. It is a safety
// valve against a malformed or looping continuation chain.
const maxItems = 5000

// NewClient builds a client. cookie is the browser Cookie header (may be empty
// for anonymous search-only use).
func NewClient(cookie string) (*Client, error) {
	return &Client{
		hc:      &http.Client{Timeout: 20 * time.Second},
		cookie:  strings.TrimSpace(cookie),
		hl:      "en",
		gl:      "US",
		baseURL: apiBase,
	}, nil
}

// Authenticated reports whether the client has credentials.
func (c *Client) Authenticated() bool { return c.cookie != "" }

// Ping is a lightweight health check (kept for API compatibility).
func (c *Client) Ping() error { return nil }

// Close releases resources (no-op; retained for API compatibility).
func (c *Client) Close() {}

// --- transport -------------------------------------------------------------

func (c *Client) request(endpoint string, body map[string]any) (map[string]any, error) {
	if body == nil {
		body = map[string]any{}
	}
	body["context"] = map[string]any{
		"client": map[string]any{
			"clientName":    clientName,
			"clientVersion": clientVersion,
			"hl":            c.hl,
			"gl":            c.gl,
		},
		"user": map[string]any{"lockedSafetyMode": false},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	base := c.baseURL
	if base == "" {
		base = apiBase
	}
	url := fmt.Sprintf("%s%s?key=%s&prettyPrint=false", base, endpoint, apiKey)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-YouTube-Client-Name", clientID)
	req.Header.Set("X-YouTube-Client-Version", clientVersion)
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", origin+"/")

	if c.cookie != "" {
		req.Header.Set("Cookie", c.cookie)
		req.Header.Set("X-Origin", origin)
		req.Header.Set("X-Goog-AuthUser", "0")
		if auth := c.sapisidHash(); auth != "" {
			req.Header.Set("Authorization", auth)
		}
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("innertube %s: HTTP %d", endpoint, resp.StatusCode)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("innertube %s: decode: %w", endpoint, err)
	}
	return root, nil
}

// sapisidHash builds the SAPISIDHASH authorization header from the cookie.
func (c *Client) sapisidHash() string {
	m := sapisidRe.FindStringSubmatch(c.cookie)
	if len(m) != 2 {
		return ""
	}
	sapisid := m[1]
	ts := time.Now().Unix()
	sum := sha1.Sum([]byte(fmt.Sprintf("%d %s %s", ts, sapisid, origin)))
	return fmt.Sprintf("SAPISIDHASH %d_%x", ts, sum)
}

// --- public API (mirrors the old bridge surface) ---------------------------

// Search returns songs matching query. It is the songs-filtered shorthand for
// SearchFiltered.
func (c *Client) Search(query string) ([]Track, error) {
	tracks, _, err := c.SearchFiltered(query, FilterSongs)
	return tracks, err
}

// SearchFiltered returns results for a query under the given filter. Track
// filters (songs/videos) populate the first return value; tile filters
// (albums/artists/playlists) populate the second.
func (c *Client) SearchFiltered(query string, filter SearchFilter) ([]Track, []BrowseItem, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil, nil
	}
	slog.Debug("search", "query", query, "filter", filter.Label())
	root, err := c.request("search", map[string]any{"query": query, "params": searchParams(filter)})
	if err != nil {
		return nil, nil, err
	}
	items := findAll(root, "musicResponsiveListItemRenderer")
	if filter.ReturnsTracks() {
		tracks := parseTrackItems(items)
		slog.Debug("search complete", "query", query, "filter", filter.Label(), "results", len(tracks))
		return tracks, nil, nil
	}
	browse := parseBrowseItems(items, filter.browseKind())
	slog.Debug("search complete", "query", query, "filter", filter.Label(), "results", len(browse))
	return nil, browse, nil
}

// GetPlaylists returns the user's library playlists (all pages).
func (c *Client) GetPlaylists() ([]Playlist, error) {
	if !c.Authenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	root, err := c.request("browse", map[string]any{"browseId": browseLibraryPlaylists})
	if err != nil {
		return nil, err
	}
	return c.collectPlaylists(root)
}

// GetPlaylistTracks returns the tracks of a playlist by ID (all pages).
func (c *Client) GetPlaylistTracks(playlistID string) ([]Track, error) {
	if playlistID == "" {
		return nil, fmt.Errorf("missing playlist id")
	}
	browseID := playlistID
	if !strings.HasPrefix(browseID, "VL") {
		browseID = "VL" + browseID
	}
	root, err := c.request("browse", map[string]any{"browseId": browseID})
	if err != nil {
		return nil, err
	}
	return c.collectTracks(root)
}

// GetLikedSongs returns the user's liked songs (all pages).
func (c *Client) GetLikedSongs() ([]Track, error) {
	if !c.Authenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	root, err := c.request("browse", map[string]any{"browseId": browseLikedSongs})
	if err != nil {
		return nil, err
	}
	return c.collectTracks(root)
}

// ratingEndpoint maps a rating to its InnerTube endpoint. YouTube Music does
// not use a single "like/rate" endpoint: each state has its own path. The
// naive "like/rate" returns HTTP 404 (verified live), while these work.
func ratingEndpoint(rating Rating) (string, error) {
	switch rating {
	case RatingLike:
		return "like/like", nil
	case RatingDislike:
		return "like/dislike", nil
	case RatingIndifferent:
		return "like/removelike", nil
	default:
		return "", fmt.Errorf("invalid rating %q", rating)
	}
}

// ratingRequest builds the endpoint and JSON body for a rating call. The body
// shape is {"target":{"videoId":"<id>"}} with a shared context added by
// request(); the rating itself is encoded in the endpoint. It is a pure
// function so the request construction can be unit-tested.
func ratingRequest(videoID string, rating Rating) (endpoint string, body map[string]any, err error) {
	if strings.TrimSpace(videoID) == "" {
		return "", nil, fmt.Errorf("missing video id")
	}
	endpoint, err = ratingEndpoint(rating)
	if err != nil {
		return "", nil, err
	}
	return endpoint, map[string]any{
		"target": map[string]any{"videoId": videoID},
	}, nil
}

// SetRating likes, dislikes, or clears the rating of a track. It requires an
// authenticated session.
func (c *Client) SetRating(videoID string, rating Rating) error {
	if !c.Authenticated() {
		return fmt.Errorf("not authenticated")
	}
	endpoint, body, err := ratingRequest(videoID, rating)
	if err != nil {
		return err
	}
	slog.Debug("set rating", "videoID", videoID, "rating", string(rating))
	_, err = c.request(endpoint, body)
	return err
}

// GetSearchSuggestions returns live search suggestions for a partial query
// using the InnerTube "music/get_search_suggestions" endpoint. Each suggestion
// is the full query to run (taken from its searchEndpoint.query), falling back
// to the concatenated display runs when no endpoint carries a query. Blank
// input yields no request. Results are capped at maxSuggestions, deduped, and
// never include the input verbatim (the UI already shows what was typed).
func (c *Client) GetSearchSuggestions(query string) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	root, err := c.request("music/get_search_suggestions", map[string]any{"input": query})
	if err != nil {
		return nil, err
	}
	return parseSuggestions(root), nil
}

// maxSuggestions caps how many suggestions are returned to the UI.
const maxSuggestions = 10

// parseSuggestions extracts suggestion queries from a music/get_search_suggestions
// response. It is exported to a helper so it can be tested without the network.
func parseSuggestions(root map[string]any) []string {
	var out []string
	seen := map[string]bool{}
	for _, node := range findAll(root, "searchSuggestionRenderer") {
		m, ok := node.(map[string]any)
		if !ok {
			continue
		}
		text := str(dig(m, "navigationEndpoint", "searchEndpoint", "query"))
		if text == "" {
			text = runsText(dig(m, "suggestion", "runs"))
		}
		text = strings.TrimSpace(text)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, text)
		if len(out) >= maxSuggestions {
			break
		}
	}
	return out
}

// GetRadio returns YouTube Music's radio/mix for the given seed videoID: a
// never-ending stream of related tracks. It uses the InnerTube "next" endpoint
// with the mix playlist id (RDAMVM<videoID>). Results are deduped by videoID;
// pagination is intentionally not followed because a radio mix is endless and
// callers only need a fresh batch to append.
func (c *Client) GetRadio(videoID string) ([]Track, error) {
	if videoID == "" {
		return nil, fmt.Errorf("missing video id")
	}
	root, err := c.request("next", map[string]any{
		"videoId":     videoID,
		"playlistId":  "RDAMVM" + videoID,
		"isAudioOnly": true,
	})
	if err != nil {
		return nil, err
	}
	tracks := parsePlaylistPanelItems(findAll(root, "playlistPanelVideoRenderer"))
	slog.Debug("radio fetched", "seed", videoID, "tracks", len(tracks))
	return tracks, nil
}

// GetAlbum returns a short album summary plus its playable tracks.
func (c *Client) GetAlbum(albumID string) (AlbumPage, error) {
	var page AlbumPage
	if albumID == "" {
		return page, fmt.Errorf("missing album id")
	}
	root, err := c.request("browse", map[string]any{"browseId": albumID})
	if err != nil {
		return page, err
	}
	page.Album = parseAlbumHeader(root, albumID)
	tracks, _ := c.collectTracks(root) // pagination is best-effort
	page.Tracks = tracks
	return page, nil
}

// GetArtist returns a short artist summary, their top songs, and their albums.
func (c *Client) GetArtist(artistID string) (ArtistPage, error) {
	var page ArtistPage
	if artistID == "" {
		return page, fmt.Errorf("missing artist id")
	}
	root, err := c.request("browse", map[string]any{"browseId": artistID})
	if err != nil {
		return page, err
	}
	page.Artist = parseArtistHeader(root, artistID)
	tracks, _ := c.collectTracks(root) // pagination is best-effort
	page.Tracks = tracks
	page.Albums = parseArtistAlbums(root)
	return page, nil
}

// CreatePlaylist creates a new private playlist and returns its ID.
func (c *Client) CreatePlaylist(title string) (string, error) {
	if !c.Authenticated() {
		return "", fmt.Errorf("not authenticated")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("playlist title is required")
	}
	root, err := c.request(endpointPlaylistCreate, map[string]any{
		"title":         title,
		"description":   "",
		"privacyStatus": "PRIVATE",
	})
	if err != nil {
		return "", err
	}
	if id := findString(root, "playlistId"); id != "" {
		return id, nil
	}
	if status := findString(root, "status"); status != "" {
		return "", fmt.Errorf("create playlist: %s", status)
	}
	return "", fmt.Errorf("create playlist: no playlist id in response")
}

// AddPlaylistItems adds one or more videos to an existing playlist. It returns
// the server status (e.g. "STATUS_SUCCEEDED").
func (c *Client) AddPlaylistItems(playlistID string, videoIDs []string) (string, error) {
	if !c.Authenticated() {
		return "", fmt.Errorf("not authenticated")
	}
	body, err := editPlaylistBody(playlistID, videoIDs)
	if err != nil {
		return "", err
	}
	root, err := c.request(endpointEditPlaylist, body)
	if err != nil {
		return "", err
	}
	return editStatus(root), nil
}

// RemovePlaylistItems removes videos from an existing playlist. Each entry must
// carry a SetVideoID (the per-membership ID YouTube assigns); entries without
// one are skipped.
func (c *Client) RemovePlaylistItems(playlistID string, tracks []Track) (string, error) {
	if !c.Authenticated() {
		return "", fmt.Errorf("not authenticated")
	}
	var removals []playlistRemoval
	for _, t := range tracks {
		if t.SetVideoID != "" && t.VideoID != "" {
			removals = append(removals, playlistRemoval{VideoID: t.VideoID, SetVideoID: t.SetVideoID})
		}
	}
	if len(removals) == 0 {
		return "", fmt.Errorf("no removable tracks (missing setVideoId — do you own this playlist?)")
	}
	body, err := removePlaylistBody(playlistID, removals)
	if err != nil {
		return "", err
	}
	root, err := c.request(endpointEditPlaylist, body)
	if err != nil {
		return "", err
	}
	return editStatus(root), nil
}

// editStatus extracts a human-readable status from an edit_playlist response.
func editStatus(root map[string]any) string {
	if s := findString(root, "status"); s != "" {
		return s
	}
	return "ok"
}

// playlistRemoval identifies one playlist membership to remove.
type playlistRemoval struct {
	VideoID    string
	SetVideoID string
}

// editPlaylistBody builds the ADD request body for browse/edit_playlist. It is a
// pure function so the wire shape can be unit-tested.
func editPlaylistBody(playlistID string, videoIDs []string) (map[string]any, error) {
	playlistID = strings.TrimPrefix(playlistID, "VL")
	if playlistID == "" {
		return nil, fmt.Errorf("missing playlist id")
	}
	actions := make([]any, 0, len(videoIDs))
	for _, vid := range videoIDs {
		if vid == "" {
			continue
		}
		actions = append(actions, map[string]any{
			"action":       "ACTION_ADD_VIDEO",
			"addedVideoId": vid,
			"dedupeOption": "DEDUPE_OPTION_SKIP",
		})
	}
	if len(actions) == 0 {
		return nil, fmt.Errorf("no valid video ids to add")
	}
	return map[string]any{"playlistId": playlistID, "actions": actions}, nil
}

// removePlaylistBody builds the REMOVE request body for browse/edit_playlist.
func removePlaylistBody(playlistID string, removals []playlistRemoval) (map[string]any, error) {
	playlistID = strings.TrimPrefix(playlistID, "VL")
	if playlistID == "" {
		return nil, fmt.Errorf("missing playlist id")
	}
	actions := make([]any, 0, len(removals))
	for _, r := range removals {
		if r.VideoID == "" || r.SetVideoID == "" {
			continue
		}
		actions = append(actions, map[string]any{
			"action":         "ACTION_REMOVE_VIDEO",
			"removedVideoId": r.VideoID,
			"setVideoId":     r.SetVideoID,
		})
	}
	if len(actions) == 0 {
		return nil, fmt.Errorf("no removable tracks")
	}
	return map[string]any{"playlistId": playlistID, "actions": actions}, nil
}

// --- pagination ------------------------------------------------------------

// browseContinuation requests the next page for a browse continuation token.
func (c *Client) browseContinuation(token string) (map[string]any, error) {
	return c.request("browse", map[string]any{"continuation": token})
}

// collectTracks parses every track across an initial browse response and all of
// its continuation pages, deduping by videoID. A failure part-way through
// pagination is logged and the tracks collected so far are returned: a single
// flaky continuation page should not discard an otherwise good first page.
func (c *Client) collectTracks(initial map[string]any) ([]Track, error) {
	seen := map[string]bool{}
	var out []Track
	err := walkContinuations(initial, c.browseContinuation, func(root map[string]any) bool {
		for _, t := range parseTrackItems(findAll(root, "musicResponsiveListItemRenderer")) {
			if seen[t.VideoID] {
				continue
			}
			seen[t.VideoID] = true
			out = append(out, t)
			if len(out) >= maxItems {
				return false
			}
		}
		return true
	})
	if err != nil {
		slog.Warn("track pagination stopped early", "collected", len(out), "err", err)
	}
	return out, nil
}

// collectPlaylists parses every playlist across an initial browse response and
// all of its continuation pages, deduping by playlistID. See collectTracks for
// the best-effort error handling rationale.
func (c *Client) collectPlaylists(initial map[string]any) ([]Playlist, error) {
	seen := map[string]bool{}
	var out []Playlist
	err := walkContinuations(initial, c.browseContinuation, func(root map[string]any) bool {
		for _, p := range parsePlaylists(root) {
			if p.PlaylistID == "" || seen[p.PlaylistID] {
				continue
			}
			seen[p.PlaylistID] = true
			out = append(out, p)
			if len(out) >= maxItems {
				return false
			}
		}
		return true
	})
	if err != nil {
		slog.Warn("playlist pagination stopped early", "collected", len(out), "err", err)
	}
	return out, nil
}

// continuationFetcher retrieves the page for a continuation token.
type continuationFetcher func(token string) (map[string]any, error)

// walkContinuations visits initial and then every continuation page, stopping
// when no new token is available, a token repeats (loop guard), or collect
// returns false (e.g. an item cap was hit). A fetch error aborts the walk and is
// returned; items collected so far are kept by the caller.
func walkContinuations(initial map[string]any, fetch continuationFetcher, collect func(root map[string]any) bool) error {
	if initial == nil {
		return nil
	}
	seen := map[string]bool{}
	root := initial
	for {
		if !collect(root) {
			return nil
		}
		token := continuationToken(root)
		if token == "" || seen[token] {
			return nil
		}
		seen[token] = true
		next, err := fetch(token)
		if err != nil {
			return err
		}
		if next == nil {
			return nil
		}
		root = next
	}
}

// continuationToken extracts the next-page token from a browse response, if
// one is present. It checks the common shapes first and falls back to a generic
// search for a "continuation" string.
func continuationToken(root any) string {
	// Preferred shape: continuations[].nextContinuationData.continuation.
	for _, c := range asSlice(dig(root, "continuations")) {
		if tok := str(dig(c, "nextContinuationData", "continuation")); tok != "" {
			return tok
		}
	}
	// Same shape nested deeper (e.g. continuationContents.sectionListContinuation).
	for _, c := range findAll(root, "nextContinuationData") {
		if tok := str(dig(c, "continuation")); tok != "" {
			return tok
		}
	}
	// Alternative shape: continuationEndpoint.continuationCommand.token.
	for _, c := range findAll(root, "continuationCommand") {
		if tok := str(dig(c, "token")); tok != "" {
			return tok
		}
	}
	// Last resort: any "continuation" string in the tree.
	return findString(root, "continuation")
}

// --- parsing ---------------------------------------------------------------

// parseTrackItems converts musicResponsiveListItemRenderer nodes into Tracks.
func parseTrackItems(items []any) []Track {
	var out []Track
	seen := map[string]bool{}
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		vid := findString(m, "videoId")
		if vid == "" || seen[vid] {
			continue
		}
		runs := flexRuns(m)
		if len(runs) == 0 {
			continue
		}
		seen[vid] = true

		t := Track{
			VideoID:   vid,
			Title:     str(runs[0]["text"]),
			URL:       "https://music.youtube.com/watch?v=" + vid,
			Thumbnail: itemThumbnail(m, vid),
		}
		var artists []string
		for _, r := range runs[1:] {
			text := str(r["text"])
			switch pageType(r) {
			case "MUSIC_PAGE_TYPE_ARTIST":
				if text != "" && text != "&" {
					artists = append(artists, text)
					if t.ArtistID == "" {
						t.ArtistID = runBrowseID(r)
					}
				}
			case "MUSIC_PAGE_TYPE_ALBUM":
				if t.Album == "" {
					t.Album = text
					t.AlbumID = runBrowseID(r)
				}
			}
			if t.Duration == "" && durRe.MatchString(text) {
				t.Duration = text
			}
		}
		t.Artist = strings.Join(dedupe(artists), ", ")
		t.SetVideoID = itemSetVideoID(m)
		out = append(out, t)
	}
	return out
}

// parsePlaylistPanelItems converts the playlistPanelVideoRenderer nodes of a
// "next" (radio/mix) response into Tracks, deduping by videoID. The seed track
// is usually included; callers dedupe against the existing queue.
func parsePlaylistPanelItems(items []any) []Track {
	var out []Track
	seen := map[string]bool{}
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		vid := str(m["videoId"])
		if vid == "" || seen[vid] {
			continue
		}
		title := runsText(dig(m, "title", "runs"))
		if title == "" {
			continue
		}
		seen[vid] = true
		artist := runsText(dig(m, "longBylineText", "runs"))
		if artist == "" {
			artist = runsText(dig(m, "shortBylineText", "runs"))
		}
		out = append(out, Track{
			VideoID:   vid,
			Title:     title,
			Artist:    artist,
			Duration:  runsText(dig(m, "lengthText", "runs")),
			URL:       "https://music.youtube.com/watch?v=" + vid,
			Thumbnail: itemThumbnail(m, vid),
		})
	}
	return out
}

// parseBrowseItems converts musicResponsiveListItemRenderer / musicTwoRowItemRenderer
// nodes into navigable BrowseItems of the given kind. Items whose browse page
// type contradicts the requested kind are skipped so an artist link nested in a
// song row cannot masquerade as an album.
func parseBrowseItems(items []any, kind string) []BrowseItem {
	var out []BrowseItem
	seen := map[string]bool{}
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id, pageType := itemBrowse(m, kind)
		if id == "" || seen[id] {
			continue
		}
		if pageType != "" && !pageTypeMatches(kind, pageType) {
			continue
		}
		title := itemTitle(m)
		if title == "" {
			continue
		}
		seen[id] = true
		out = append(out, BrowseItem{
			ID:        id,
			Title:     title,
			Subtitle:  itemSubtitle(m),
			Kind:      kind,
			Thumbnail: itemThumbnail(m, ""),
		})
	}
	return out
}

// pageTypeMatches reports whether a browse pageType belongs to the given kind.
func pageTypeMatches(kind, pageType string) bool {
	switch kind {
	case KindAlbum:
		return pageType == "MUSIC_PAGE_TYPE_ALBUM"
	case KindArtist:
		return pageType == "MUSIC_PAGE_TYPE_ARTIST"
	case KindPlaylist:
		return pageType == "MUSIC_PAGE_TYPE_PLAYLIST" ||
			pageType == "MUSIC_PAGE_TYPE_USER_CHANNEL" ||
			strings.HasPrefix(pageType, "MUSIC_PAGE_TYPE_PLAYLIST")
	default:
		return true
	}
}

// itemBrowse returns the primary browse ID and page type of a tile. It reads the
// item's own navigationEndpoint (and the title run's), never the artist links
// nested inside a song row's metadata columns.
func itemBrowse(m map[string]any, kind string) (id, pageType string) {
	endpoints := []any{dig(m, "navigationEndpoint", "browseEndpoint")}
	for _, r := range asSlice(dig(m, "title", "runs")) {
		endpoints = append(endpoints, dig(r, "navigationEndpoint", "browseEndpoint"))
	}
	for _, e := range endpoints {
		bm, ok := e.(map[string]any)
		if !ok {
			continue
		}
		bid := str(bm["browseId"])
		if bid == "" {
			continue
		}
		pt := str(dig(bm,
			"browseEndpointContextSupportedConfigs",
			"browseEndpointContextMusicConfig", "pageType"))
		return bid, pt
	}
	// Playlists sometimes expose only a playlistId field.
	if kind == KindPlaylist {
		if pid := findString(m, "playlistId"); pid != "" {
			return normalisePlaylistID(pid), ""
		}
	}
	return "", ""
}

// normalisePlaylistID ensures a playlist id carries the "VL" browse prefix.
func normalisePlaylistID(id string) string {
	if id == "" || strings.HasPrefix(id, "VL") || strings.HasPrefix(id, "RD") {
		return id
	}
	return "VL" + id
}

// parseAlbumHeader extracts the album title/artist/year from a browse response.
func parseAlbumHeader(root any, albumID string) Album {
	a := Album{ID: albumID}
	h := firstMap(root, "musicResponsiveHeaderRenderer", "musicDetailHeaderRenderer", "musicImmersiveHeaderRenderer")
	if h != nil {
		a.Title = itemTitle(h)
		a.Artist, a.Year = subtitleArtistYear(h)
	}
	if a.Title == "" {
		if rm, ok := root.(map[string]any); ok {
			a.Title = itemTitle(rm)
		}
	}
	return a
}

// parseArtistHeader extracts the artist name/subscriber line from a browse
// response.
func parseArtistHeader(root any, artistID string) Artist {
	ar := Artist{ID: artistID}
	h := firstMap(root, "musicImmersiveHeaderRenderer", "musicVisualHeaderRenderer", "musicResponsiveHeaderRenderer", "musicDetailHeaderRenderer")
	if h != nil {
		ar.Name = itemTitle(h)
		if ar.Name == "" {
			ar.Name = findString(h, "title")
		}
		ar.Subscribers = itemSubtitle(h)
	}
	if ar.Name == "" {
		ar.Name = findString(root, "title")
	}
	return ar
}

// parseArtistAlbums pulls album tiles out of an artist page. Only two-row
// carousel tiles are considered, so song rows are never mistaken for albums.
func parseArtistAlbums(root any) []BrowseItem {
	return parseBrowseItems(findAll(root, "musicTwoRowItemRenderer"), KindAlbum)
}

// firstMap returns the first occurrence of any of the given keys as a map.
func firstMap(v any, keys ...string) map[string]any {
	for _, key := range keys {
		for _, it := range findAll(v, key) {
			if m, ok := it.(map[string]any); ok {
				return m
			}
		}
	}
	return nil
}

// subtitleArtistYear reads "Artist • 2020" style runs from a header. Item-type
// labels that YouTube prepends (e.g. "Album •") are ignored so the artist is
// not mistaken for the type.
func subtitleArtistYear(m map[string]any) (artist, year string) {
	runs := asSlice(dig(m, "subtitle", "runs"))
	for _, col := range flexRuns(m) {
		runs = append(runs, col)
	}
	var parts []string
	for _, r := range runs {
		text := str(dig(r, "text"))
		for _, p := range runSepRe.Split(text, -1) {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
	}
	for _, p := range parts {
		switch {
		case year == "" && yearRe.MatchString(p):
			year = p
		case isTypeLabel(p):
			// skip "Album", "Single", "EP", …
		case artist == "":
			artist = p
		}
	}
	return artist, year
}

// isTypeLabel reports whether p is a YouTube Music item-type label rather than
// an artist name.
func isTypeLabel(p string) bool {
	switch strings.ToLower(p) {
	case "album", "single", "ep", "song", "video", "playlist", "compilation",
		"soundtrack", "live", "mixtape", "podcast", "episode", "audiobook",
		"album • single", "auto playlist":
		return true
	}
	return false
}

// itemSubtitle returns the secondary text of a tile/row ("Album • Artist • 2020").
func itemSubtitle(m map[string]any) string {
	if runs := asSlice(dig(m, "subtitle", "runs")); len(runs) > 0 {
		return runsText(runs)
	}
	runs := flexRuns(m)
	if len(runs) > 1 {
		parts := make([]any, 0, len(runs)-1)
		for _, r := range runs[1:] {
			parts = append(parts, r)
		}
		return runsText(parts)
	}
	return ""
}

// runsText joins the "text" fields of text runs with no separator (YouTube
// supplies its own separators as runs). It accepts either a []any of runs or the
// raw value holding them, so it serves both the radio and library parsers.
func runsText(v any) string {
	var b strings.Builder
	for _, r := range asSlice(v) {
		b.WriteString(str(dig(r, "text")))
	}
	return strings.TrimSpace(b.String())
}

// runBrowseID returns the browse ID attached to a text run, if any.
func runBrowseID(run map[string]any) string {
	return str(dig(run, "navigationEndpoint", "browseEndpoint", "browseId"))
}

// itemSetVideoID extracts the per-membership setVideoId from an item's
// playlistEditEndpoint menu action (present on playlist rows only).
func itemSetVideoID(m map[string]any) string {
	for _, pe := range findAll(m, "playlistEditEndpoint") {
		actions := asSlice(dig(pe, "actions"))
		if len(actions) == 0 {
			continue
		}
		if id := str(dig(actions[0], "setVideoId")); id != "" {
			return id
		}
	}
	return ""
}

// parsePlaylists converts library playlist rows/grid tiles into Playlists.
func parsePlaylists(root any) []Playlist {
	var out []Playlist
	seen := map[string]bool{}
	for _, key := range []string{"musicTwoRowItemRenderer", "musicResponsiveListItemRenderer"} {
		for _, it := range findAll(root, key) {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			id := findString(m, "playlistId")
			if id == "" {
				if bid := findString(m, "browseId"); strings.HasPrefix(bid, "VL") {
					id = strings.TrimPrefix(bid, "VL")
				}
			}
			if id == "" || seen[id] {
				continue
			}
			title := itemTitle(m)
			if title == "" {
				continue
			}
			seen[id] = true
			out = append(out, Playlist{
				PlaylistID: id,
				Title:      title,
				Count:      itemCount(m),
			})
		}
	}
	return out
}

// itemThumbnail returns the largest thumbnail URL for a list item, falling back
// to YouTube's standard hqdefault image for the video.
func itemThumbnail(m map[string]any, videoID string) string {
	if th := dig(m, "thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"); th != nil {
		if u := lastThumbURL(th); u != "" {
			return u
		}
	}
	for _, th := range findAll(m, "thumbnails") {
		if u := lastThumbURL(th); u != "" {
			return u
		}
	}
	if videoID != "" {
		return "https://i.ytimg.com/vi/" + videoID + "/hqdefault.jpg"
	}
	return ""
}

// lastThumbURL picks the last (largest) entry's URL from a thumbnails array.
func lastThumbURL(v any) string {
	entries := asSlice(v)
	if len(entries) == 0 {
		return ""
	}
	last, _ := entries[len(entries)-1].(map[string]any)
	return str(last["url"])
}

// flexRuns flattens all text runs across the flex columns of a list item.
func flexRuns(m map[string]any) []map[string]any {
	var runs []map[string]any
	for _, col := range asSlice(m["flexColumns"]) {
		cm, _ := col.(map[string]any)
		fr, _ := cm["musicResponsiveListItemFlexColumnRenderer"].(map[string]any)
		text, _ := fr["text"].(map[string]any)
		for _, r := range asSlice(text["runs"]) {
			if rm, ok := r.(map[string]any); ok {
				runs = append(runs, rm)
			}
		}
	}
	return runs
}

// itemTitle pulls the title from a two-row tile or a list row.
func itemTitle(m map[string]any) string {
	if runs := asSlice(dig(m, "title", "runs")); len(runs) > 0 {
		if rm, ok := runs[0].(map[string]any); ok && str(rm["text"]) != "" {
			return str(rm["text"])
		}
	}
	if s := str(dig(m, "title", "simpleText")); s != "" {
		return s
	}
	if runs := flexRuns(m); len(runs) > 0 {
		return str(runs[0]["text"])
	}
	return ""
}

// itemCount finds a track count in an item's subtitle or flex columns. It
// matches "<n> song|track|video|episode" anywhere across the text runs, since
// YouTube sometimes splits the number and unit across separate runs. When no
// such phrase exists the count is 0 (e.g. Liked Music reports no count) — we
// never invent one.
func itemCount(m map[string]any) int {
	if n := countInRuns(asSlice(dig(m, "subtitle", "runs"))); n > 0 {
		return n
	}
	runs := make([]any, 0, len(m))
	for _, r := range flexRuns(m) {
		runs = append(runs, r)
	}
	return countInRuns(runs)
}

// countInRuns joins the text of the given runs and returns the first track-count
// phrase found, or 0.
func countInRuns(runs []any) int {
	var b strings.Builder
	for _, r := range runs {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		b.WriteString(str(rm["text"]))
		b.WriteByte(' ')
	}
	sub := countRe.FindStringSubmatch(b.String())
	if len(sub) < 2 {
		return 0
	}
	n, err := strconv.Atoi(sub[1])
	if err != nil {
		return 0
	}
	return n
}

// pageType returns the browse page type of a text run, if any.
func pageType(run map[string]any) string {
	return str(dig(run,
		"navigationEndpoint", "browseEndpoint",
		"browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType"))
}

// --- generic JSON helpers --------------------------------------------------

func dig(v any, path ...string) any {
	cur := v
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[key]
	}
	return cur
}

func findAll(v any, key string) []any {
	var out []any
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			for k, val := range n {
				if k == key {
					out = append(out, val)
				}
				walk(val)
			}
		case []any:
			for _, e := range n {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

func findString(v any, key string) string {
	found := ""
	var walk func(any) bool
	walk = func(node any) bool {
		switch n := node.(type) {
		case map[string]any:
			if val, ok := n[key]; ok {
				if s, ok := val.(string); ok && s != "" {
					found = s
					return true
				}
			}
			for _, val := range n {
				if walk(val) {
					return true
				}
			}
		case []any:
			for _, e := range n {
				if walk(e) {
					return true
				}
			}
		}
		return false
	}
	walk(v)
	return found
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
