package player

import (
	"testing"
	"time"
)

// mockPersister records calls to SetURL for inspection in tests.
type mockPersister struct {
	calls map[string]string
}

func newMockPersister() *mockPersister {
	return &mockPersister{calls: make(map[string]string)}
}

func (m *mockPersister) SetURL(videoID, url string) {
	m.calls[videoID] = url
}

func TestURLCache_MissOnEmpty(t *testing.T) {
	c := NewURLCache(nil)
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected cache miss for unknown videoID")
	}
}

func TestURLCache_SetGet(t *testing.T) {
	c := NewURLCache(nil)
	c.Set("vid1", "https://example.com/stream1")

	url, ok := c.Get("vid1")
	if !ok {
		t.Fatal("expected cache hit after Set")
	}
	if url != "https://example.com/stream1" {
		t.Errorf("unexpected url: %s", url)
	}
}

func TestURLCache_TTLExpiry(t *testing.T) {
	c := NewURLCache(nil)

	// Load an entry that is already past TTL
	expired := time.Now().Add(-(CacheTTL + time.Minute))
	c.Load("vid_expired", "https://example.com/old", expired)

	_, ok := c.Get("vid_expired")
	if ok {
		t.Error("expected cache miss for expired entry")
	}
}

func TestURLCache_Load_doesNotPersist(t *testing.T) {
	p := newMockPersister()
	c := NewURLCache(p)

	c.Load("vid1", "https://example.com/stream", time.Now())

	if _, called := p.calls["vid1"]; called {
		t.Error("Load() must not call the persister")
	}
}

func TestURLCache_Set_callsPersister(t *testing.T) {
	p := newMockPersister()
	c := NewURLCache(p)

	c.Set("vid1", "https://example.com/stream")

	if url, ok := p.calls["vid1"]; !ok {
		t.Error("Set() must call the persister")
	} else if url != "https://example.com/stream" {
		t.Errorf("persisted wrong url: %s", url)
	}
}

func TestURLCache_NilPersister(t *testing.T) {
	// Must not panic when no persister is configured
	c := NewURLCache(nil)
	c.Set("vid1", "https://example.com/stream")
	_, ok := c.Get("vid1")
	if !ok {
		t.Error("expected hit with nil persister")
	}
}

// mockDeleter implements both URLPersister and URLDeleter.
type mockDeleter struct {
	mockPersister
	deleted []string
}

func newMockDeleter() *mockDeleter {
	return &mockDeleter{mockPersister: *newMockPersister()}
}

func (m *mockDeleter) DeleteURL(videoID string) {
	m.deleted = append(m.deleted, videoID)
}

func TestURLCache_Evict(t *testing.T) {
	p := newMockDeleter()
	c := NewURLCache(p)

	c.Set("vid1", "https://example.com/stream")
	if _, ok := c.Get("vid1"); !ok {
		t.Fatal("expected a hit before eviction")
	}

	c.Evict("vid1")
	if _, ok := c.Get("vid1"); ok {
		t.Error("expected a miss after Evict")
	}
	if len(p.deleted) != 1 || p.deleted[0] != "vid1" {
		t.Errorf("persisted deletions = %v, want [vid1]", p.deleted)
	}
}

func TestURLCache_Evict_withoutDeleter(t *testing.T) {
	// A persister that only knows SetURL must not panic on Evict.
	p := newMockPersister()
	c := NewURLCache(p)
	c.Set("vid1", "u")
	c.Evict("vid1")
	if _, ok := c.Get("vid1"); ok {
		t.Error("expected a miss after Evict with a non-deleting persister")
	}
}

func TestURLCache_OverwriteRefreshesTTL(t *testing.T) {
	c := NewURLCache(nil)

	// Simulate a near-expired entry via Load
	almostExpired := time.Now().Add(-(CacheTTL - 10*time.Second))
	c.Load("vid1", "https://old.url", almostExpired)

	// Overwrite via Set
	c.Set("vid1", "https://new.url")

	url, ok := c.Get("vid1")
	if !ok {
		t.Fatal("expected hit after overwrite")
	}
	if url != "https://new.url" {
		t.Errorf("expected new url, got %s", url)
	}
}
