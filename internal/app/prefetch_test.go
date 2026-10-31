package app

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Myleshen/yt-music-tui/internal/player"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// waitFor polls cond until it returns true or the deadline elapses.
func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", d)
}

func TestPrefetcher_ResolvesAndCaches(t *testing.T) {
	cache := player.NewURLCache(nil)
	var mu sync.Mutex
	var calls []string

	p := newPrefetcherWithResolver(cache, "", func(_, ytURL string) (string, error) {
		mu.Lock()
		calls = append(calls, ytURL)
		mu.Unlock()
		return "stream://" + ytURL, nil
	})

	p.Prewarm([]ytmusic.Track{
		{VideoID: "a", URL: "https://x/a"},
		{VideoID: "b", URL: "https://x/b"},
	})

	waitFor(t, 3*time.Second, func() bool {
		_, a := cache.Get("a")
		_, b := cache.Get("b")
		return a && b
	})

	if url, _ := cache.Get("a"); url != "stream://https://x/a" {
		t.Errorf("unexpected cached url for a: %q", url)
	}
	mu.Lock()
	n := len(calls)
	mu.Unlock()
	if n != 2 {
		t.Errorf("expected 2 resolver calls, got %d", n)
	}
}

func TestPrefetcher_SkipsCachedAndDuplicates(t *testing.T) {
	cache := player.NewURLCache(nil)
	cache.Set("already", "stream://cached")

	var mu sync.Mutex
	count := 0
	p := newPrefetcherWithResolver(cache, "", func(_, _ string) (string, error) {
		mu.Lock()
		count++
		mu.Unlock()
		return "stream://x", nil
	})

	// "already" is cached; "dup" is enqueued twice.
	p.Prewarm([]ytmusic.Track{
		{VideoID: "already", URL: "https://x/already"},
		{VideoID: "dup", URL: "https://x/dup"},
		{VideoID: "dup", URL: "https://x/dup"},
	})

	waitFor(t, 3*time.Second, func() bool {
		_, ok := cache.Get("dup")
		return ok
	})

	// Give any (incorrect) extra work a moment to be observed.
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	got := count
	mu.Unlock()
	if got != 1 {
		t.Errorf("expected exactly 1 resolution (cached skipped, duplicate coalesced), got %d", got)
	}
}

func TestPrefetcher_FailureDoesNotCache(t *testing.T) {
	cache := player.NewURLCache(nil)
	p := newPrefetcherWithResolver(cache, "", func(_, _ string) (string, error) {
		return "", errors.New("boom")
	})

	p.Prewarm([]ytmusic.Track{{VideoID: "bad", URL: "https://x/bad"}})

	// Nothing is cached on failure.
	time.Sleep(300 * time.Millisecond)
	if _, ok := cache.Get("bad"); ok {
		t.Error("failed resolution must not populate the cache")
	}
}

func TestPrefetcher_IgnoresIncompleteTracks(t *testing.T) {
	cache := player.NewURLCache(nil)
	var mu sync.Mutex
	count := 0
	p := newPrefetcherWithResolver(cache, "", func(_, _ string) (string, error) {
		mu.Lock()
		count++
		mu.Unlock()
		return "stream://x", nil
	})

	p.Prewarm([]ytmusic.Track{
		{VideoID: "", URL: "https://x/noid"},
		{VideoID: "nourl", URL: ""},
	})

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	got := count
	mu.Unlock()
	if got != 0 {
		t.Errorf("expected 0 resolutions for incomplete tracks, got %d", got)
	}
}
