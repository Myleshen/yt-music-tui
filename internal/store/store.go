package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/Myleshen/yt-music-tui/internal/stats"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

var (
	bucketURLs  = []byte("urls")
	bucketState = []byte("state")
	bucketMeta  = []byte("metadata")
	bucketStats = []byte("stats")
	keyLast     = []byte("last_playing")

	keyLibrary       = []byte("library")
	keyPlaylists     = []byte("playlists")
	keyRatings       = []byte("ratings")
	keySearchHistory = []byte("search_history")
	keyPlays         = []byte("plays")
)

// MaxSearchHistory caps how many recent searches are retained, newest first.
const MaxSearchHistory = 50

// maxStoredPlays caps the local play history so the database cannot grow
// without bound. Older plays beyond this are dropped.
const maxStoredPlays = 20000

// MetadataTTL is how long cached library/playlist metadata is considered
// fresh. Entries older than this are still served on open (so tabs are
// instant) — callers refresh them in the background.
const MetadataTTL = 24 * time.Hour

// URLEntry is stored in the urls bucket.
type URLEntry struct {
	URL      string    `json:"url"`
	CachedAt time.Time `json:"cachedAt"`
}

// LastPlayingState is stored in the state bucket. It captures the exact queue:
// the ordered track list, the current index, the playback position, the
// shuffle flag/order, and where the queue came from, so a relaunch can restore
// the precise session.
type LastPlayingState struct {
	Tracks       []ytmusic.Track `json:"tracks"`
	Index        int             `json:"index"`
	Position     float64         `json:"position"`
	FromPlaylist string          `json:"fromPlaylist"`
	Shuffle      bool            `json:"shuffle"`
	// Order is the shuffled playback order (a permutation of track indices).
	// It is empty for the default sorted order.
	Order []int `json:"order,omitempty"`
}

// Store wraps a bbolt database for persistent ytm state.
type Store struct {
	db *bolt.DB
}

// DefaultPath returns ~/.config/ytmusic-tui/state.db.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ytmusic-tui", "state.db")
}

// Open opens (or creates) the bbolt database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketURLs, bucketState, bucketMeta, bucketStats} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// --- URL cache ---

// SetURL persists a stream URL for a videoID.
func (s *Store) SetURL(videoID, url string) {
	data, _ := json.Marshal(URLEntry{URL: url, CachedAt: time.Now()})
	_ = s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketURLs).Put([]byte(videoID), data)
	})
}

// GetURL returns the stored URL and its age for a videoID.
func (s *Store) GetURL(videoID string) (URLEntry, bool) {
	var e URLEntry
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketURLs).Get([]byte(videoID))
		if v == nil {
			return bolt.ErrBucketNotFound
		}
		return json.Unmarshal(v, &e)
	})
	return e, err == nil
}

// DeleteURL removes a cached stream URL for a videoID. It is called when a URL
// has expired so the next lookup re-resolves instead of re-serving a dead URL.
func (s *Store) DeleteURL(videoID string) {
	_ = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketURLs)
		if b == nil {
			return nil
		}
		return b.Delete([]byte(videoID))
	})
}

// LoadAllURLs returns all stored URL entries (for warming the in-memory cache).
func (s *Store) LoadAllURLs() map[string]URLEntry {
	result := make(map[string]URLEntry)
	_ = s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketURLs).ForEach(func(k, v []byte) error {
			var e URLEntry
			if json.Unmarshal(v, &e) == nil {
				result[string(k)] = e
			}
			return nil
		})
	})
	return result
}

// --- Playback state ---

// SaveLastPlaying persists the current playback state.
func (s *Store) SaveLastPlaying(state LastPlayingState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketState).Put(keyLast, data)
	})
}

// LoadLastPlaying returns the last saved playback state.
func (s *Store) LoadLastPlaying() (LastPlayingState, bool) {
	var state LastPlayingState
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketState).Get(keyLast)
		if v == nil {
			return bolt.ErrBucketNotFound
		}
		return json.Unmarshal(v, &state)
	})
	return state, err == nil
}

// --- Metadata cache (library / playlists / playlist tracks) ---
//
// Each entry is a JSON blob with a saved-at timestamp. Readers get the blob
// back regardless of age (stale-on-open) together with its timestamp so
// callers can decide whether to refresh in the background.

// metaEnvelope is the on-disk shape of a metadata entry.
type metaEnvelope struct {
	SavedAt time.Time       `json:"savedAt"`
	Data    json.RawMessage `json:"data"`
}

// saveMeta stores an arbitrary JSON-marshalable value under key with a
// timestamp of now.
func saveMeta[T any](s *Store, key []byte, value T) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	env, err := json.Marshal(metaEnvelope{SavedAt: time.Now(), Data: payload})
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMeta).Put(key, env)
	})
}

// loadMeta returns the value stored under key, the time it was saved, and
// whether it was found. The value is returned even when older than MetadataTTL.
func loadMeta[T any](s *Store, key []byte) (T, time.Time, bool) {
	var zero T
	var env metaEnvelope
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketMeta).Get(key)
		if v == nil {
			return bolt.ErrBucketNotFound
		}
		return json.Unmarshal(v, &env)
	})
	if err != nil {
		return zero, time.Time{}, false
	}
	var value T
	if err := json.Unmarshal(env.Data, &value); err != nil {
		return zero, time.Time{}, false
	}
	return value, env.SavedAt, true
}

// SaveLibrary persists the user's liked-songs list.
func (s *Store) SaveLibrary(tracks []ytmusic.Track) error {
	return saveMeta(s, keyLibrary, tracks)
}

// LoadLibrary returns the cached liked songs and when they were saved.
func (s *Store) LoadLibrary() ([]ytmusic.Track, time.Time, bool) {
	return loadMeta[[]ytmusic.Track](s, keyLibrary)
}

// SavePlaylists persists the user's library playlists.
func (s *Store) SavePlaylists(playlists []ytmusic.Playlist) error {
	return saveMeta(s, keyPlaylists, playlists)
}

// LoadPlaylists returns the cached playlists and when they were saved.
func (s *Store) LoadPlaylists() ([]ytmusic.Playlist, time.Time, bool) {
	return loadMeta[[]ytmusic.Playlist](s, keyPlaylists)
}

// SaveRatings persists the user's track ratings (videoID -> "LIKE",
// "DISLIKE", or "INDIFFERENT") so the heart indicator survives a restart.
func (s *Store) SaveRatings(ratings map[string]string) error {
	return saveMeta(s, keyRatings, ratings)
}

// LoadRatings returns the persisted track ratings.
func (s *Store) LoadRatings() (map[string]string, bool) {
	ratings, _, ok := loadMeta[map[string]string](s, keyRatings)
	return ratings, ok
}

func playlistTracksKey(id string) []byte {
	return []byte("playlist_tracks:" + id)
}

// SavePlaylistTracks persists the tracks of a single playlist by ID.
func (s *Store) SavePlaylistTracks(id string, tracks []ytmusic.Track) error {
	if id == "" {
		return nil
	}
	return saveMeta(s, playlistTracksKey(id), tracks)
}

// LoadPlaylistTracks returns the cached tracks of a playlist and when they
// were saved.
func (s *Store) LoadPlaylistTracks(id string) ([]ytmusic.Track, time.Time, bool) {
	if id == "" {
		return nil, time.Time{}, false
	}
	return loadMeta[[]ytmusic.Track](s, playlistTracksKey(id))
}

// --- Search history ---

// AddToHistory returns history with query moved to the front. Matching is
// case-insensitive and whitespace-trimmed, so re-searching "Radiohead" after
// "radiohead" does not create a duplicate; the list is capped at
// MaxSearchHistory. A blank query is ignored. It is a pure helper so the
// dedupe/order rules can be tested without a database.
func AddToHistory(history []string, query string) []string {
	q := strings.TrimSpace(query)
	if q == "" {
		return history
	}
	out := make([]string, 0, len(history)+1)
	out = append(out, q)
	for _, h := range history {
		if strings.EqualFold(strings.TrimSpace(h), q) {
			continue
		}
		out = append(out, h)
	}
	if len(out) > MaxSearchHistory {
		out = out[:MaxSearchHistory]
	}
	return out
}

// AddSearchHistory records a search query (newest first) and persists it.
func (s *Store) AddSearchHistory(query string) error {
	return saveMeta(s, keySearchHistory, AddToHistory(s.LoadSearchHistory(), query))
}

// LoadSearchHistory returns the persisted recent searches, newest first.
func (s *Store) LoadSearchHistory() []string {
	h, _, ok := loadMeta[[]string](s, keySearchHistory)
	if !ok {
		return nil
	}
	return h
}

// --- Listening stats ---

// AppendPlay records a local listen, trimming the oldest entries when the
// history grows past maxStoredPlays.
func (s *Store) AppendPlay(p stats.Play) error {
	if p.At.IsZero() {
		p.At = time.Now()
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketStats)
		var plays []stats.Play
		if v := b.Get(keyPlays); v != nil {
			_ = json.Unmarshal(v, &plays)
		}
		plays = append(plays, p)
		if len(plays) > maxStoredPlays {
			plays = plays[len(plays)-maxStoredPlays:]
		}
		data, err := json.Marshal(plays)
		if err != nil {
			return err
		}
		return b.Put(keyPlays, data)
	})
}

// LoadPlays returns the recorded listening history (oldest first).
func (s *Store) LoadPlays() []stats.Play {
	var plays []stats.Play
	_ = s.db.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(bucketStats).Get(keyPlays); v != nil {
			_ = json.Unmarshal(v, &plays)
		}
		return nil
	})
	return plays
}
