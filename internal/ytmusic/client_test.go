package ytmusic

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	return m
}

// trackItem builds a minimal musicResponsiveListItemRenderer fixture.
func trackItem(title, artist, videoID string) string {
	return fmt.Sprintf(`{
		"musicResponsiveListItemRenderer": {
			"playlistItemData": {"videoId": %q},
			"flexColumns": [
				{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": %q}]}}},
				{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": %q,
					"navigationEndpoint": {"browseEndpoint": {"browseEndpointContextSupportedConfigs":
						{"browseEndpointContextMusicConfig": {"pageType": "MUSIC_PAGE_TYPE_ARTIST"}}}}}]}}}
			]
		}
	}`, videoID, title, artist)
}

// TestWalkContinuationsFollowsTokens walks a two-page fixture and checks that
// the continuation token is followed exactly once.
func TestWalkContinuationsFollowsTokens(t *testing.T) {
	page1 := mustJSON(t, `{
		"sectionListRenderer": {"contents": [`+trackItem("Song A", "Artist A", "vidA")+`]},
		"continuations": [{"nextContinuationData": {"continuation": "TOKEN1"}}]
	}`)
	page2 := mustJSON(t, `{
		"continuationContents": {"musicShelfContinuation": {"contents": [
			`+trackItem("Song B", "Artist B", "vidB")+`
		]}}
	}`)

	var fetched []string
	fetch := func(token string) (map[string]any, error) {
		fetched = append(fetched, token)
		if token == "TOKEN1" {
			return page2, nil
		}
		return nil, fmt.Errorf("unexpected token %q", token)
	}

	var titles []string
	visits := 0
	err := walkContinuations(page1, fetch, func(root map[string]any) bool {
		visits++
		for _, it := range findAll(root, "musicResponsiveListItemRenderer") {
			m, _ := it.(map[string]any)
			if runs := flexRuns(m); len(runs) > 0 {
				titles = append(titles, str(runs[0]["text"]))
			}
		}
		return true
	})
	if err != nil {
		t.Fatalf("walkContinuations: %v", err)
	}
	if visits != 2 {
		t.Errorf("collect called %d times, want 2", visits)
	}
	if !reflect.DeepEqual(fetched, []string{"TOKEN1"}) {
		t.Errorf("fetched tokens = %v, want [TOKEN1]", fetched)
	}
	if !reflect.DeepEqual(titles, []string{"Song A", "Song B"}) {
		t.Errorf("titles = %v, want [Song A Song B]", titles)
	}
}

// TestWalkContinuationsStopsOnRepeatedToken guards against an infinite loop when
// a server keeps handing back the same token.
func TestWalkContinuationsStopsOnRepeatedToken(t *testing.T) {
	page := mustJSON(t, `{"continuations": [{"nextContinuationData": {"continuation": "LOOP"}}]}`)
	fetches := 0
	err := walkContinuations(page, func(token string) (map[string]any, error) {
		fetches++
		return page, nil
	}, func(root map[string]any) bool { return true })
	if err != nil {
		t.Fatalf("walkContinuations: %v", err)
	}
	if fetches != 1 {
		t.Errorf("fetches = %d, want 1 (repeated token must stop the walk)", fetches)
	}
}

// TestWalkContinuationsRespectsCap verifies collect can stop pagination early.
func TestWalkContinuationsRespectsCap(t *testing.T) {
	page := `{"continuations": [{"nextContinuationData": {"continuation": "NEXT"}}]}`
	fetches := 0
	err := walkContinuations(mustJSON(t, page), func(token string) (map[string]any, error) {
		fetches++
		return mustJSON(t, page), nil
	}, func(root map[string]any) bool { return false })
	if err != nil {
		t.Fatalf("walkContinuations: %v", err)
	}
	if fetches != 0 {
		t.Errorf("fetches = %d, want 0 when collect stops immediately", fetches)
	}
}

// TestContinuationTokenShapes covers the two continuation JSON shapes.
func TestContinuationTokenShapes(t *testing.T) {
	viaNext := mustJSON(t, `{"continuations": [{"nextContinuationData": {"continuation": "AAA"}}]}`)
	if got := continuationToken(viaNext); got != "AAA" {
		t.Errorf("nextContinuationData token = %q, want AAA", got)
	}
	viaCmd := mustJSON(t, `{"continuationEndpoint": {"continuationCommand": {"token": "BBB"}}}`)
	if got := continuationToken(viaCmd); got != "BBB" {
		t.Errorf("continuationCommand token = %q, want BBB", got)
	}
	if got := continuationToken(mustJSON(t, `{"foo": "bar"}`)); got != "" {
		t.Errorf("token for tokenless page = %q, want empty", got)
	}
}

// TestParseTrackItemsThumbnail checks the largest thumbnail is chosen and that a
// missing thumbnails array falls back to the hqdefault image.
func TestParseTrackItemsThumbnail(t *testing.T) {
	withThumb := mustJSON(t, `{
		"videoId": "abc123",
		"thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [
			{"url": "https://img/small.jpg", "width": 60},
			{"url": "https://img/large.jpg", "width": 544}
		]}}},
		"flexColumns": [{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Title"}]}}}]
	}`)
	got := parseTrackItems([]any{withThumb})
	if len(got) != 1 {
		t.Fatalf("got %d tracks, want 1", len(got))
	}
	if got[0].Thumbnail != "https://img/large.jpg" {
		t.Errorf("thumbnail = %q, want largest entry", got[0].Thumbnail)
	}

	noThumb := mustJSON(t, `{
		"videoId": "xyz789",
		"flexColumns": [{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "Title"}]}}}]
	}`)
	got = parseTrackItems([]any{noThumb})
	if len(got) != 1 {
		t.Fatalf("got %d tracks, want 1", len(got))
	}
	if want := "https://i.ytimg.com/vi/xyz789/hqdefault.jpg"; got[0].Thumbnail != want {
		t.Errorf("fallback thumbnail = %q, want %q", got[0].Thumbnail, want)
	}
}

// panelItem builds a minimal playlistPanelVideoRenderer fixture (radio/mix
// "next" response shape).
func panelItem(title, artist, videoID, length string) string {
	return fmt.Sprintf(`{
		"playlistPanelVideoRenderer": {
			"videoId": %q,
			"title": {"runs": [{"text": %q}]},
			"longBylineText": {"runs": [{"text": %q}]},
			"lengthText": {"runs": [{"text": %q}]},
			"thumbnail": {"musicThumbnailRenderer": {"thumbnail": {"thumbnails": [{"url": "https://img/cover.jpg"}]}}}
		}
	}`, videoID, title, artist, length)
}

// TestParsePlaylistPanelItems covers radio/mix parsing: fields are extracted,
// duplicates by videoID are dropped, and items without a videoID are skipped.
func TestParsePlaylistPanelItems(t *testing.T) {
	root := mustJSON(t, `{"contents": {"items": [
		`+panelItem("Song A", "Artist A", "vidA", "3:12")+`,
		`+panelItem("Song A dup", "Artist A", "vidA", "3:12")+`,
		`+panelItem("Song B", "Artist B", "vidB", "4:05")+`,
		`+panelItem("No id", "Artist C", "", "1:00")+`
	]}}`)

	got := parsePlaylistPanelItems(findAll(root, "playlistPanelVideoRenderer"))
	if len(got) != 2 {
		t.Fatalf("got %d tracks, want 2 (dup + no-id dropped): %+v", len(got), got)
	}
	if got[0].VideoID != "vidA" || got[0].Title != "Song A" || got[0].Artist != "Artist A" || got[0].Duration != "3:12" {
		t.Errorf("unexpected first track: %+v", got[0])
	}
	if got[0].URL != "https://music.youtube.com/watch?v=vidA" {
		t.Errorf("URL = %q", got[0].URL)
	}
	if got[0].Thumbnail != "https://img/cover.jpg" {
		t.Errorf("Thumbnail = %q", got[0].Thumbnail)
	}
	if got[1].VideoID != "vidB" {
		t.Errorf("second track = %+v, want vidB", got[1])
	}
}

// TestRatingEndpoint checks each rating maps to its own InnerTube path. The
// naive single "like/rate" endpoint 404s (verified live); the real API uses
// per-state paths.
func TestRatingEndpoint(t *testing.T) {
	cases := []struct {
		rating Rating
		want   string
	}{
		{RatingLike, "like/like"},
		{RatingDislike, "like/dislike"},
		{RatingIndifferent, "like/removelike"},
	}
	for _, tc := range cases {
		got, err := ratingEndpoint(tc.rating)
		if err != nil {
			t.Errorf("ratingEndpoint(%s) error: %v", tc.rating, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ratingEndpoint(%s) = %q, want %q", tc.rating, got, tc.want)
		}
	}
	if _, err := ratingEndpoint("BOGUS"); err == nil {
		t.Error("ratingEndpoint should reject an unknown rating")
	}
}

// TestRatingRequest checks the request body/endpoint construction and its
// guards.
func TestRatingRequest(t *testing.T) {
	ep, body, err := ratingRequest("vid123", RatingLike)
	if err != nil {
		t.Fatalf("ratingRequest: %v", err)
	}
	if ep != "like/like" {
		t.Errorf("endpoint = %q, want like/like", ep)
	}
	target, ok := body["target"].(map[string]any)
	if !ok || target["videoId"] != "vid123" {
		t.Errorf("body target = %#v, want videoId vid123", body["target"])
	}
	if _, present := body["rating"]; present {
		t.Error("body must not carry a rating key; the rating lives in the endpoint")
	}
	if _, _, err := ratingRequest("   ", RatingLike); err == nil {
		t.Error("empty video id should be rejected")
	}
	if _, _, err := ratingRequest("vid123", "NOPE"); err == nil {
		t.Error("invalid rating should be rejected")
	}
}

// TestSetRating_SendsAuthenticatedRequest exercises the full request path
// against an httptest server, asserting the endpoint, auth headers, context,
// and body.
func TestSetRating_SendsAuthenticatedRequest(t *testing.T) {
	var gotPath, gotAuth, gotCookie string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c, err := NewClient("SAPISID=abc123; __Secure-1PAPISID=zzz")
	if err != nil {
		t.Fatal(err)
	}
	c.baseURL = srv.URL + "/youtubei/v1/"

	if err := c.SetRating("vid123", RatingLike); err != nil {
		t.Fatalf("SetRating: %v", err)
	}
	if gotPath != "/youtubei/v1/like/like" {
		t.Errorf("path = %q, want /youtubei/v1/like/like", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "SAPISIDHASH ") {
		t.Errorf("Authorization = %q, want SAPISIDHASH prefix", gotAuth)
	}
	if gotCookie == "" {
		t.Error("Cookie header was not sent")
	}
	target, _ := gotBody["target"].(map[string]any)
	if target["videoId"] != "vid123" {
		t.Errorf("body target = %#v", gotBody["target"])
	}
	ctx, _ := gotBody["context"].(map[string]any)
	client, _ := ctx["client"].(map[string]any)
	if client["clientName"] != "WEB_REMIX" {
		t.Errorf("context client name = %v, want WEB_REMIX", client["clientName"])
	}
}

// TestSetRating_AllStatesHitCorrectPath checks the endpoint actually used for
// each rating state.
func TestSetRating_AllStatesHitCorrectPath(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c, _ := NewClient("SAPISID=abc123")
	c.baseURL = srv.URL + "/youtubei/v1/"
	for _, r := range []Rating{RatingLike, RatingDislike, RatingIndifferent} {
		if err := c.SetRating("vid123", r); err != nil {
			t.Fatalf("SetRating(%s): %v", r, err)
		}
	}
	want := []string{"/youtubei/v1/like/like", "/youtubei/v1/like/dislike", "/youtubei/v1/like/removelike"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

// TestSetRating_Unauthenticated ensures an anonymous client never hits the
// network.
func TestSetRating_Unauthenticated(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()
	c, _ := NewClient("")
	c.baseURL = srv.URL + "/youtubei/v1/"
	if err := c.SetRating("vid123", RatingLike); err == nil {
		t.Error("SetRating without a session should fail")
	}
	if called {
		t.Error("unauthenticated SetRating must not make a request")
	}
}

// TestParseSuggestions extracts the full query from each suggestion's
// searchEndpoint, falling back to the display runs, deduping and skipping
// entries with no text. Suggestions without a query are still included via the
// runs fallback.
func TestParseSuggestions(t *testing.T) {
	root := mustJSON(t, `{"contents": [
		{"searchSuggestionsSectionRenderer": {"contents": [
			{"searchSuggestionRenderer": {
				"suggestion": {"runs": [{"text": "radio"}, {"text": "head"}]},
				"navigationEndpoint": {"searchEndpoint": {"query": "radiohead"}}
			}},
			{"searchSuggestionRenderer": {
				"suggestion": {"runs": [{"text": "radiohead"}, {"text": " let down"}]}
			}},
			{"searchSuggestionRenderer": {
				"suggestion": {"runs": [{"text": "radiohead"}]},
				"navigationEndpoint": {"searchEndpoint": {"query": "radiohead"}}
			}},
			{"searchSuggestionRenderer": {"suggestion": {"runs": []}}}
		]}}
	]}`)

	got := parseSuggestions(root)
	want := []string{"radiohead", "radiohead let down"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseSuggestions = %v, want %v", got, want)
	}
	if got := parseSuggestions(mustJSON(t, `{"contents": []}`)); len(got) != 0 {
		t.Errorf("empty response suggestions = %v, want none", got)
	}
}

// TestGetSearchSuggestions_blankInput avoids a network round-trip for empty
// input.
func TestGetSearchSuggestions_blankInput(t *testing.T) {
	c, err := NewClient("")
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.GetSearchSuggestions("   ")
	if err != nil || len(got) != 0 {
		t.Fatalf("blank input = (%v, %v), want (nil, nil)", got, err)
	}
}

// TestGetRadio_missingID ensures an empty seed is rejected without a request.
func TestGetRadio_missingID(t *testing.T) {
	c, err := NewClient("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetRadio(""); err == nil {
		t.Error("expected an error for an empty video id")
	}
}

// TestItemCount checks track counts are found across subtitle/flex runs and that
// absent counts stay 0 (never invented).
func TestItemCount(t *testing.T) {
	cases := []struct {
		name string
		json string
		want int
	}{
		{
			name: "subtitle single run",
			json: `{"subtitle": {"runs": [{"text": "50 songs"}]}}`,
			want: 50,
		},
		{
			name: "subtitle split runs",
			json: `{"subtitle": {"runs": [{"text": "100"}, {"text": " tracks"}]}}`,
			want: 100,
		},
		{
			name: "flex columns",
			json: `{"flexColumns": [{"musicResponsiveListItemFlexColumnRenderer": {"text": {"runs": [{"text": "My Mix"}, {"text": "• 100 videos"}]}}}]}`,
			want: 100,
		},
		{
			name: "no count (liked music style)",
			json: `{"subtitle": {"runs": [{"text": "Auto playlist"}]}}`,
			want: 0,
		},
		{
			name: "number without a unit is not a count",
			json: `{"subtitle": {"runs": [{"text": "2024 Mix"}]}}`,
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := itemCount(mustJSON(t, tc.json)); got != tc.want {
				t.Errorf("itemCount = %d, want %d", got, tc.want)
			}
		})
	}
}
