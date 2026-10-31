package player

import (
	"sync"
	"time"
)

// CacheTTL is how long a stream URL is considered valid.
// YouTube signs URLs for ~6 hours.
const CacheTTL = 6 * time.Hour

// URLPersister is satisfied by *store.Store — kept as an interface so
// the player package doesn't need to import store.
type URLPersister interface {
	SetURL(videoID, url string)
}

// URLDeleter is optionally satisfied by a persister that can also remove a
// cached URL (used when a stream URL has expired and must be re-resolved).
// It is a separate interface so memory-only and older persisters keep working.
type URLDeleter interface {
	DeleteURL(videoID string)
}

type cacheEntry struct {
	url      string
	cachedAt time.Time
}

// URLCache is a thread-safe in-memory cache with optional write-through persistence.
type URLCache struct {
	mu        sync.Mutex
	entries   map[string]cacheEntry
	persister URLPersister // nil = memory-only
}

// NewURLCache creates a cache. Pass a *store.Store to enable write-through, or nil
// for memory-only (useful in tests).
func NewURLCache(p URLPersister) *URLCache {
	return &URLCache{
		entries:   make(map[string]cacheEntry),
		persister: p,
	}
}

// Load populates the cache from a persisted entry without triggering a write-back.
// Call this on startup when warming the cache from the DB.
func (c *URLCache) Load(videoID, url string, cachedAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[videoID] = cacheEntry{url: url, cachedAt: cachedAt}
}

// Get returns a cached URL if present and not expired.
func (c *URLCache) Get(videoID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[videoID]
	if !ok || time.Since(e.cachedAt) > CacheTTL {
		return "", false
	}
	return e.url, true
}

// Set stores a URL in memory and persists it if a persister is configured.
func (c *URLCache) Set(videoID, url string) {
	now := time.Now()
	c.mu.Lock()
	c.entries[videoID] = cacheEntry{url: url, cachedAt: now}
	c.mu.Unlock()
	if c.persister != nil {
		c.persister.SetURL(videoID, url)
	}
}

// Evict removes a cached URL (in memory and, when supported, on disk) so the
// next lookup forces a fresh resolution. It is used when a googlevideo URL has
// expired and playback failed.
func (c *URLCache) Evict(videoID string) {
	c.mu.Lock()
	delete(c.entries, videoID)
	c.mu.Unlock()
	if d, ok := c.persister.(URLDeleter); ok && d != nil {
		d.DeleteURL(videoID)
	}
}
