package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/Myleshen/yt-music-tui/internal/stats"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_Open(t *testing.T) {
	// Should create buckets without error and allow immediate use.
	s := openTestStore(t)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestStore_RatingsRoundTrip(t *testing.T) {
	s := openTestStore(t)

	if _, ok := s.LoadRatings(); ok {
		t.Error("expected no ratings before saving")
	}
	ratings := map[string]string{"vidA": "LIKE", "vidB": "DISLIKE"}
	if err := s.SaveRatings(ratings); err != nil {
		t.Fatalf("SaveRatings: %v", err)
	}
	got, ok := s.LoadRatings()
	if !ok {
		t.Fatal("expected LoadRatings ok=true")
	}
	if len(got) != 2 || got["vidA"] != "LIKE" || got["vidB"] != "DISLIKE" {
		t.Errorf("ratings = %v, want the saved map", got)
	}
}

func TestStore_SetURL_GetURL(t *testing.T) {
	s := openTestStore(t)

	s.SetURL("vid1", "https://example.com/stream1")

	entry, ok := s.GetURL("vid1")
	if !ok {
		t.Fatal("expected GetURL to return ok=true")
	}
	if entry.URL != "https://example.com/stream1" {
		t.Errorf("unexpected URL: %s", entry.URL)
	}
	if entry.CachedAt.IsZero() {
		t.Error("CachedAt must be set")
	}
	// CachedAt should be recent (within 5 seconds)
	if time.Since(entry.CachedAt) > 5*time.Second {
		t.Errorf("CachedAt appears too old: %v", entry.CachedAt)
	}
}

func TestStore_GetURL_missing(t *testing.T) {
	s := openTestStore(t)

	_, ok := s.GetURL("nonexistent")
	if ok {
		t.Error("expected ok=false for unknown videoID")
	}
}

func TestStore_SetURL_overwrite(t *testing.T) {
	s := openTestStore(t)

	s.SetURL("vid1", "https://example.com/old")
	s.SetURL("vid1", "https://example.com/new")

	entry, ok := s.GetURL("vid1")
	if !ok {
		t.Fatal("expected ok=true after overwrite")
	}
	if entry.URL != "https://example.com/new" {
		t.Errorf("expected new URL after overwrite, got %s", entry.URL)
	}
}

func TestStore_LoadAllURLs(t *testing.T) {
	s := openTestStore(t)

	s.SetURL("vid1", "https://example.com/stream1")
	s.SetURL("vid2", "https://example.com/stream2")
	s.SetURL("vid3", "https://example.com/stream3")

	all := s.LoadAllURLs()
	if len(all) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(all))
	}
	if all["vid1"].URL != "https://example.com/stream1" {
		t.Errorf("unexpected url for vid1: %s", all["vid1"].URL)
	}
	if all["vid2"].URL != "https://example.com/stream2" {
		t.Errorf("unexpected url for vid2: %s", all["vid2"].URL)
	}
}

func TestStore_LoadAllURLs_empty(t *testing.T) {
	s := openTestStore(t)

	all := s.LoadAllURLs()
	if len(all) != 0 {
		t.Errorf("expected 0 entries on empty store, got %d", len(all))
	}
}

func TestStore_SaveLoadLastPlaying(t *testing.T) {
	s := openTestStore(t)

	tracks := []ytmusic.Track{
		{VideoID: "vid1", Title: "Track 1", Artist: "Artist A"},
		{VideoID: "vid2", Title: "Track 2", Artist: "Artist B"},
	}
	state := LastPlayingState{
		Tracks:       tracks,
		Index:        1,
		Position:     42.5,
		FromPlaylist: "My Playlist",
	}

	if err := s.SaveLastPlaying(state); err != nil {
		t.Fatalf("SaveLastPlaying: %v", err)
	}

	loaded, ok := s.LoadLastPlaying()
	if !ok {
		t.Fatal("expected ok=true after Save")
	}
	if loaded.Index != 1 {
		t.Errorf("expected Index=1, got %d", loaded.Index)
	}
	if loaded.Position != 42.5 {
		t.Errorf("expected Position=42.5, got %f", loaded.Position)
	}
	if loaded.FromPlaylist != "My Playlist" {
		t.Errorf("expected FromPlaylist='My Playlist', got %q", loaded.FromPlaylist)
	}
	if len(loaded.Tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(loaded.Tracks))
	}
	if loaded.Tracks[0].VideoID != "vid1" {
		t.Errorf("expected first track vid1, got %s", loaded.Tracks[0].VideoID)
	}
}

func TestStore_LastPlaying_exactQueueRoundTrip(t *testing.T) {
	s := openTestStore(t)

	state := LastPlayingState{
		Tracks: []ytmusic.Track{
			{VideoID: "a", Title: "A", Artist: "AA"},
			{VideoID: "b", Title: "B", Artist: "BB"},
			{VideoID: "c", Title: "C", Artist: "CC"},
		},
		Index:        1,
		Position:     12.5,
		FromPlaylist: "Focus",
		Shuffle:      true,
		Order:        []int{2, 0, 1},
	}
	if err := s.SaveLastPlaying(state); err != nil {
		t.Fatalf("SaveLastPlaying: %v", err)
	}

	got, ok := s.LoadLastPlaying()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !got.Shuffle {
		t.Error("Shuffle flag not persisted")
	}
	if len(got.Order) != 3 || got.Order[0] != 2 || got.Order[2] != 1 {
		t.Errorf("Order = %v, want [2 0 1]", got.Order)
	}
	if got.Index != 1 || got.Position != 12.5 {
		t.Errorf("unexpected index/position: %d / %v", got.Index, got.Position)
	}
	if len(got.Tracks) != 3 || got.Tracks[2].VideoID != "c" {
		t.Errorf("tracks not round-tripped: %+v", got.Tracks)
	}
}

func TestStore_LastPlaying_unshuffledOmitsOrder(t *testing.T) {
	s := openTestStore(t)
	if err := s.SaveLastPlaying(LastPlayingState{
		Tracks: []ytmusic.Track{{VideoID: "a"}},
		Index:  0,
	}); err != nil {
		t.Fatal(err)
	}
	got, ok := s.LoadLastPlaying()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.Shuffle || len(got.Order) != 0 {
		t.Errorf("expected no shuffle/order, got shuffle=%v order=%v", got.Shuffle, got.Order)
	}
}

func TestStore_DeleteURL(t *testing.T) {
	s := openTestStore(t)
	s.SetURL("vid1", "https://example.com/stream")
	s.SetURL("vid2", "https://example.com/stream2")

	s.DeleteURL("vid1")

	if _, ok := s.GetURL("vid1"); ok {
		t.Error("expected vid1 to be deleted")
	}
	if _, ok := s.GetURL("vid2"); !ok {
		t.Error("DeleteURL must not remove other entries")
	}
	// Deleting a missing key is a no-op, not a panic.
	s.DeleteURL("does-not-exist")
}

func TestStore_LoadLastPlaying_empty(t *testing.T) {
	s := openTestStore(t)

	_, ok := s.LoadLastPlaying()
	if ok {
		t.Error("expected ok=false when no state has been saved")
	}
}

func TestStore_SaveLastPlaying_overwrite(t *testing.T) {
	s := openTestStore(t)

	first := LastPlayingState{
		Tracks:   []ytmusic.Track{{VideoID: "vid1", Title: "First"}},
		Index:    0,
		Position: 10.0,
	}
	second := LastPlayingState{
		Tracks:   []ytmusic.Track{{VideoID: "vid2", Title: "Second"}},
		Index:    0,
		Position: 99.0,
	}

	if err := s.SaveLastPlaying(first); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLastPlaying(second); err != nil {
		t.Fatal(err)
	}

	loaded, ok := s.LoadLastPlaying()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if loaded.Position != 99.0 {
		t.Errorf("expected second save to overwrite, got position %.1f", loaded.Position)
	}
	if loaded.Tracks[0].VideoID != "vid2" {
		t.Errorf("expected vid2, got %s", loaded.Tracks[0].VideoID)
	}
}

func TestStore_Library_roundTrip(t *testing.T) {
	s := openTestStore(t)

	// Missing key → not found.
	if _, _, ok := s.LoadLibrary(); ok {
		t.Fatal("expected ok=false on empty store")
	}

	tracks := []ytmusic.Track{
		{VideoID: "vid1", Title: "Track 1", Artist: "A", Album: "X", Duration: "3:12", URL: "u1"},
		{VideoID: "vid2", Title: "Track 2", Artist: "B", URL: "u2"},
	}
	if err := s.SaveLibrary(tracks); err != nil {
		t.Fatalf("SaveLibrary: %v", err)
	}

	got, savedAt, ok := s.LoadLibrary()
	if !ok {
		t.Fatal("expected ok=true after SaveLibrary")
	}
	if savedAt.IsZero() || time.Since(savedAt) > 5*time.Second {
		t.Errorf("unexpected savedAt: %v", savedAt)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(got))
	}
	if got[0].VideoID != "vid1" || got[0].Album != "X" || got[1].Artist != "B" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestStore_Playlists_roundTrip(t *testing.T) {
	s := openTestStore(t)

	if _, _, ok := s.LoadPlaylists(); ok {
		t.Fatal("expected ok=false on empty store")
	}

	pls := []ytmusic.Playlist{
		{PlaylistID: "pl1", Title: "Chill", Count: 12},
		{PlaylistID: "pl2", Title: "Focus", Count: 5},
	}
	if err := s.SavePlaylists(pls); err != nil {
		t.Fatalf("SavePlaylists: %v", err)
	}

	got, _, ok := s.LoadPlaylists()
	if !ok {
		t.Fatal("expected ok=true after SavePlaylists")
	}
	if len(got) != 2 || got[0].PlaylistID != "pl1" || got[1].Count != 5 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestStore_PlaylistTracks_roundTrip(t *testing.T) {
	s := openTestStore(t)

	if _, _, ok := s.LoadPlaylistTracks("pl1"); ok {
		t.Fatal("expected ok=false for unknown playlist")
	}
	// Empty id is rejected cleanly.
	if err := s.SavePlaylistTracks("", []ytmusic.Track{{VideoID: "x"}}); err != nil {
		t.Fatalf("SavePlaylistTracks with empty id should be a no-op, got %v", err)
	}
	if _, _, ok := s.LoadPlaylistTracks(""); ok {
		t.Error("expected ok=false for empty id")
	}

	tracks := []ytmusic.Track{{VideoID: "vid1", Title: "One"}, {VideoID: "vid2", Title: "Two"}}
	if err := s.SavePlaylistTracks("pl1", tracks); err != nil {
		t.Fatalf("SavePlaylistTracks: %v", err)
	}

	got, _, ok := s.LoadPlaylistTracks("pl1")
	if !ok {
		t.Fatal("expected ok=true after SavePlaylistTracks")
	}
	if len(got) != 2 || got[1].Title != "Two" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	// A different playlist must not see pl1's tracks.
	if _, _, ok := s.LoadPlaylistTracks("pl2"); ok {
		t.Error("pl2 should not resolve to pl1's cached tracks")
	}
}

func TestAddToHistory_dedupeOrderCap(t *testing.T) {
	// Newest first, blank ignored.
	got := AddToHistory(nil, "  ")
	if got != nil {
		t.Fatalf("blank query should be ignored, got %v", got)
	}
	got = AddToHistory(nil, "one")
	got = AddToHistory(got, "two")
	if !reflect.DeepEqual(got, []string{"two", "one"}) {
		t.Fatalf("newest first = %v", got)
	}
	// Case-insensitive dedupe moves the existing entry to the front.
	got = AddToHistory(got, "ONE")
	if !reflect.DeepEqual(got, []string{"ONE", "two"}) {
		t.Fatalf("dedupe move-to-front = %v", got)
	}
	// Whitespace around a query is normalised before comparing.
	got = AddToHistory(got, "  two ")
	if !reflect.DeepEqual(got, []string{"two", "ONE"}) {
		t.Fatalf("trimmed dedupe = %v", got)
	}
	// Cap at MaxSearchHistory, dropping the oldest.
	var h []string
	for i := 0; i < MaxSearchHistory+10; i++ {
		h = AddToHistory(h, fmt.Sprintf("q%02d", i))
	}
	if len(h) != MaxSearchHistory {
		t.Fatalf("len = %d, want cap %d", len(h), MaxSearchHistory)
	}
	if h[0] != fmt.Sprintf("q%02d", MaxSearchHistory+9) {
		t.Errorf("newest entry = %q", h[0])
	}
}

func TestStore_SearchHistory_roundTrip(t *testing.T) {
	s := openTestStore(t)
	if h := s.LoadSearchHistory(); len(h) != 0 {
		t.Fatalf("fresh store history = %v, want empty", h)
	}
	if err := s.AddSearchHistory("radiohead"); err != nil {
		t.Fatalf("AddSearchHistory: %v", err)
	}
	if err := s.AddSearchHistory("bjork"); err != nil {
		t.Fatalf("AddSearchHistory: %v", err)
	}
	if err := s.AddSearchHistory("Radiohead"); err != nil { // dedupe
		t.Fatalf("AddSearchHistory: %v", err)
	}
	got := s.LoadSearchHistory()
	if !reflect.DeepEqual(got, []string{"Radiohead", "bjork"}) {
		t.Fatalf("history = %v, want [Radiohead bjork]", got)
	}
}

// TestStore_Metadata_servesStale confirms stale-on-open behavior: an entry
// saved long ago is still returned (with its timestamp) rather than dropped.
func TestStore_Metadata_servesStale(t *testing.T) {
	s := openTestStore(t)

	// Write the envelope directly with an old timestamp.
	old := time.Now().Add(-(MetadataTTL + 48*time.Hour))
	payload, _ := json.Marshal([]ytmusic.Track{{VideoID: "vid1"}})
	env, _ := json.Marshal(metaEnvelope{SavedAt: old, Data: payload})
	if err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMeta).Put(keyLibrary, env)
	}); err != nil {
		t.Fatalf("seed stale entry: %v", err)
	}

	got, savedAt, ok := s.LoadLibrary()
	if !ok {
		t.Fatal("stale entry must still be returned (stale-on-open)")
	}
	if len(got) != 1 || got[0].VideoID != "vid1" {
		t.Errorf("unexpected stale payload: %+v", got)
	}
	if time.Since(savedAt) < MetadataTTL {
		t.Errorf("expected stale timestamp, got %v", savedAt)
	}
}

func TestStore_AppendPlay(t *testing.T) {
	s := openTestStore(t)
	for i := 0; i < 3; i++ {
		if err := s.AppendPlay(stats.Play{VideoID: "v", Title: "T", Seconds: 30}); err != nil {
			t.Fatalf("AppendPlay: %v", err)
		}
	}
	plays := s.LoadPlays()
	if len(plays) != 3 {
		t.Fatalf("LoadPlays = %d, want 3", len(plays))
	}
	if plays[0].At.IsZero() {
		t.Error("play timestamp should be filled in")
	}
}
