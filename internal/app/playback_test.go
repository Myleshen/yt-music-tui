package app

import (
	"errors"
	"testing"

	"github.com/Myleshen/yt-music-tui/internal/mediactl"
	"github.com/Myleshen/yt-music-tui/internal/player"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// withResolver swaps the package stream resolver for the duration of fn.
func withResolver(t *testing.T, resolve func(cookiesFile, ytURL string) (string, error), fn func()) {
	t.Helper()
	orig := streamResolver
	streamResolver = resolve
	t.Cleanup(func() { streamResolver = orig })
	fn()
}

func TestRefreshAndLoadCmd_evictsAndResolves(t *testing.T) {
	cache := player.NewURLCache(nil)
	cache.Set("vid1", "https://old/expired")

	m := Model{urlCache: cache, cookiesFile: "cookies.txt"}
	track := ytmusic.Track{VideoID: "vid1", Title: "T", Artist: "A", URL: "https://music.youtube.com/watch?v=vid1"}

	var gotCookies, gotURL string
	withResolver(t, func(cookies, ytURL string) (string, error) {
		gotCookies, gotURL = cookies, ytURL
		return "https://new/fresh", nil
	}, func() {
		msg := m.refreshAndLoadCmd(track)()
		lm, ok := msg.(mpvLoadMsg)
		if !ok {
			t.Fatalf("got %T, want mpvLoadMsg", msg)
		}
		if lm.err != nil {
			t.Fatalf("unexpected error: %v", lm.err)
		}
		if lm.url != "https://new/fresh" {
			t.Errorf("url = %q, want the freshly resolved URL", lm.url)
		}
		if lm.title != "T - A" {
			t.Errorf("title = %q, want %q", lm.title, "T - A")
		}
	})

	if gotCookies != "cookies.txt" {
		t.Errorf("resolver cookies = %q, want cookies.txt", gotCookies)
	}
	if gotURL != track.URL {
		t.Errorf("resolver ytURL = %q, want %q", gotURL, track.URL)
	}
	// The stale URL is replaced by the fresh one.
	if url, ok := cache.Get("vid1"); !ok || url != "https://new/fresh" {
		t.Errorf("cache after refresh = (%q,%v), want fresh URL", url, ok)
	}
}

func TestRefreshAndLoadCmd_errorLeavesNothingCached(t *testing.T) {
	cache := player.NewURLCache(nil)
	cache.Set("vid1", "https://old/expired")
	m := Model{urlCache: cache}
	track := ytmusic.Track{VideoID: "vid1", URL: "u"}

	withResolver(t, func(_, _ string) (string, error) {
		return "", errors.New("boom")
	}, func() {
		lm, ok := m.refreshAndLoadCmd(track)().(mpvLoadMsg)
		if !ok || lm.err == nil {
			t.Fatalf("expected an mpvLoadMsg error, got %#v", lm)
		}
	})

	if _, ok := cache.Get("vid1"); ok {
		t.Error("a failed refresh must leave the URL evicted")
	}
}

func TestRetriableTrack(t *testing.T) {
	m := Model{}
	if !m.retriableTrack(ytmusic.Track{VideoID: "v"}) {
		t.Error("a fresh track with a videoID should be retriable")
	}
	if m.retriableTrack(ytmusic.Track{}) {
		t.Error("a track without a videoID is not retriable")
	}
	m.retried = map[string]bool{"v": true}
	if m.retriableTrack(ytmusic.Track{VideoID: "v"}) {
		t.Error("an already-retried track must not be retried again")
	}
}

func TestAppendRadio_dedupesAndCaps(t *testing.T) {
	q := player.NewQueue([]ytmusic.Track{{VideoID: "a"}, {VideoID: "b"}, {VideoID: "c"}}, 0)
	m := Model{queue: q}

	in := []ytmusic.Track{{VideoID: "b"}, {VideoID: "d"}, {VideoID: "e"}, {VideoID: "d"}}
	if n := m.appendRadio(in); n != 2 {
		t.Errorf("appendRadio added %d, want 2 (dup dropped)", n)
	}
	if q.Len() != 5 {
		t.Errorf("queue length = %d, want 5", q.Len())
	}

	// radioAppendMax bounds a single fetch.
	q2 := player.NewQueue([]ytmusic.Track{{VideoID: "seed"}}, 0)
	m2 := Model{queue: q2}
	many := make([]ytmusic.Track, radioAppendMax+10)
	for i := range many {
		many[i] = ytmusic.Track{VideoID: "r" + string(rune('A'+i))}
	}
	if n := m2.appendRadio(many); n != radioAppendMax {
		t.Errorf("appendRadio added %d, want cap %d", n, radioAppendMax)
	}
}

func TestRadioCmd_guards(t *testing.T) {
	// No queue → no radio.
	m := Model{}
	if cmd := m.radioCmd(); cmd != nil {
		t.Error("radioCmd without a queue should be nil")
	}
	// Queue but no client → no radio.
	m = Model{queue: player.NewQueue([]ytmusic.Track{{VideoID: "v"}}, 0)}
	if cmd := m.radioCmd(); cmd != nil {
		t.Error("radioCmd without a client should be nil")
	}
	// Concurrent fetch in flight → no radio (even with a client present).
	client, err := ytmusic.NewClient("")
	if err != nil {
		t.Fatal(err)
	}
	m = Model{queue: player.NewQueue([]ytmusic.Track{{VideoID: "v"}}, 0), ytClient: client}
	m.radioLoading = true
	if cmd := m.radioCmd(); cmd != nil {
		t.Error("radioCmd while loading should be nil")
	}
	// At the growth cap → no radio.
	m.radioLoading = false
	m.radioAppended = radioTotalMax
	if cmd := m.radioCmd(); cmd != nil {
		t.Error("radioCmd at the growth cap should be nil")
	}
}

func TestAppendRadio_respectsTotalCap(t *testing.T) {
	q := player.NewQueue([]ytmusic.Track{{VideoID: "seed"}}, 0)
	m := Model{queue: q, radioAppended: radioTotalMax}
	if n := m.appendRadio([]ytmusic.Track{{VideoID: "new"}}); n != 0 {
		t.Errorf("appendRadio past the total cap added %d, want 0", n)
	}
	if q.Len() != 1 {
		t.Errorf("queue grew past the cap: %d", q.Len())
	}
}

func TestHandleMPRISCommand_shuffle(t *testing.T) {
	q := player.NewQueue(mkTracks(4), 0)
	m := Model{queue: q}

	m.handleMPRISCommand(mediactl.Command{Action: mediactl.ActionSetShuffle, Shuffle: true})
	if !q.Shuffled() {
		t.Error("MPRIS SetShuffle(true) did not enable shuffle")
	}
	m.handleMPRISCommand(mediactl.Command{Action: mediactl.ActionSetShuffle, Shuffle: false})
	if q.Shuffled() {
		t.Error("MPRIS SetShuffle(false) did not disable shuffle")
	}
}

func TestHandleMPRISCommand_nextWithoutMPV(t *testing.T) {
	// Next with no player/queue must be a safe no-op.
	m := Model{}
	if cmds := m.handleMPRISCommand(mediactl.Command{Action: mediactl.ActionNext}); len(cmds) != 0 {
		t.Errorf("Next without a queue returned %d cmds, want 0", len(cmds))
	}
}

func TestToggleShuffle_nilQueue(t *testing.T) {
	m := Model{}
	if cmds := m.toggleShuffle(); cmds != nil {
		t.Errorf("toggleShuffle without a queue = %v, want nil", cmds)
	}
}

func TestFriendlyError(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"dial tcp: i/o timeout", "network problem — check your connection"},
		{"Get \"x\": connection refused", "network problem — check your connection"},
		{"innertube search: HTTP 403", "this stream expired — retrying"},
		{"innertube browse: HTTP 401", "sign-in expired — run `ytm auth`"},
		{"yt-dlp: exited with error", "couldn't resolve the audio stream"},
		{"weird internal failure", "something went wrong"},
	}
	for _, c := range cases {
		if got := friendlyError(errors.New(c.in)); got != c.want {
			t.Errorf("friendlyError(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if friendlyError(nil) != "" {
		t.Error("friendlyError(nil) should be empty")
	}
	// flashError sets a friendly status and returns a toast command.
	m := Model{}
	_ = m.flashError("search failed", errors.New("dial tcp: i/o timeout"))
	if m.status != "search failed" {
		t.Errorf("status = %q, want the friendly prefix", m.status)
	}
	if !m.osdVisible || !m.osdError {
		t.Error("flashError must show an error-styled toast")
	}
}
