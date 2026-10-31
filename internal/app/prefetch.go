package app

import (
	"log/slog"
	"sync"
	"time"

	"github.com/Myleshen/yt-music-tui/internal/player"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

const (
	// prefetchQueueSize bounds how many tracks may wait for resolution.
	prefetchQueueSize = 6
	// prefetchGap is the minimum spacing between yt-dlp invocations.
	prefetchGap = 300 * time.Millisecond
)

// prefetcher resolves stream URLs in the background, one at a time, to make
// next/previous skips feel instant. Serialising and spacing out yt-dlp calls is
// deliberate: resolving many streams at once trips YouTube's abuse/bot throttle
// (GOOGLE_ABUSE / ENHANCE_YOUR_CALM). It is a single worker draining a small
// buffered channel; duplicate and already-cached videoIDs are dropped, and a
// full queue simply drops new work (tracks fall back to on-demand resolution).
type prefetcher struct {
	cache       *player.URLCache
	cookiesFile string
	resolve     func(cookiesFile, ytURL string) (string, error)

	mu     sync.Mutex
	queued map[string]bool
	ch     chan ytmusic.Track
}

// newPrefetcher builds a prefetcher backed by resolveStreamURL and starts its
// worker goroutine.
func newPrefetcher(cache *player.URLCache, cookiesFile string) *prefetcher {
	return newPrefetcherWithResolver(cache, cookiesFile, extractStreamURL)
}

// newPrefetcherWithResolver is like newPrefetcher but allows injecting the
// resolver (used by tests).
func newPrefetcherWithResolver(cache *player.URLCache, cookiesFile string, resolve func(cookiesFile, ytURL string) (string, error)) *prefetcher {
	p := &prefetcher{
		cache:       cache,
		cookiesFile: cookiesFile,
		resolve:     resolve,
		queued:      make(map[string]bool),
		ch:          make(chan ytmusic.Track, prefetchQueueSize),
	}
	go p.run()
	return p
}

// Prewarm enqueues the given tracks for background URL resolution, skipping
// anything already cached or already queued. It never blocks the caller.
func (p *prefetcher) Prewarm(tracks []ytmusic.Track) {
	if p == nil {
		return
	}
	for _, t := range tracks {
		if t.VideoID == "" || t.URL == "" {
			continue
		}
		if p.cache != nil {
			if _, ok := p.cache.Get(t.VideoID); ok {
				continue
			}
		}
		p.mu.Lock()
		already := p.queued[t.VideoID]
		if !already {
			p.queued[t.VideoID] = true
		}
		p.mu.Unlock()
		if already {
			continue
		}
		select {
		case p.ch <- t:
		default:
			slog.Debug("prefetch: queue full, dropping", "videoID", t.VideoID)
		}
	}
}

// run is the single serial worker. It re-checks the cache before resolving (the
// track may have been fetched on demand in the meantime) and pauses between
// resolutions to stay gentle on YouTube.
func (p *prefetcher) run() {
	for t := range p.ch {
		if p.cache != nil {
			if _, ok := p.cache.Get(t.VideoID); ok {
				continue
			}
		}
		slog.Debug("prefetch: resolving stream URL", "videoID", t.VideoID, "title", t.Title)
		url, err := p.resolve(p.cookiesFile, t.URL)
		if err != nil {
			slog.Debug("prefetch: resolution failed", "videoID", t.VideoID, "err", err)
		} else {
			p.cache.Set(t.VideoID, url)
			slog.Debug("prefetch: cached stream URL", "videoID", t.VideoID)
		}
		time.Sleep(prefetchGap)
	}
}
