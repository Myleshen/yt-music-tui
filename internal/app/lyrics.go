package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

const (
	lyricsEndpoint = "https://lrclib.net/api"
	lyricsTimeout  = 10 * time.Second
	// lyricsUserAgent identifies us to LRCLIB, which asks API consumers to set
	// a descriptive User-Agent.
	lyricsUserAgent = "ytm/0.1.0 (+https://github.com/Myleshen/yt-music-tui)"
)

// lyricsClient is shared across fetches.
var lyricsClient = &http.Client{Timeout: lyricsTimeout}

var (
	// lrcTag matches an LRC tag: a timestamp like "[00:12.34]" or a metadata tag
	// like "[ar:Artist]".
	lrcTag = regexp.MustCompile(`\[[^\]]*\]`)
	// lrcTime matches a timestamp tag: [mm:ss], [mm:ss.x], [mm:ss.xx] or
	// [mm:ss.xxx]. The separator may be '.' (standard) or ':' (some editors).
	lrcTime = regexp.MustCompile(`\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	// lrcOffset matches the [offset:±ms] metadata tag.
	lrcOffset = regexp.MustCompile(`\[offset:\s*([+-]?\d+)\s*\]`)
)

// lyricLine is one display line of lyrics. time is the line's start position in
// seconds; it is negative for unsynced (plain) lyrics.
type lyricLine struct {
	time float64
	text string
}

// Lyrics is a track's lyrics: the display lines and whether they are synced to
// the audio. When Synced is true every line carries a valid start time (except
// possibly the very first few before the first timestamp).
type Lyrics struct {
	Lines  []lyricLine
	Synced bool
}

// empty reports whether there is nothing to display.
func (l Lyrics) empty() bool { return len(l.Lines) == 0 }

// lyricsResult is the raw shape returned by a fetch and written to the disk
// cache: the plain (unsynced) lines and the raw synced LRC document. Both forms
// are kept so synced highlighting can be reproduced without a network round-trip
// and so a track whose synced lyrics fail to parse still has plain lines.
type lyricsResult struct {
	plain  []string
	synced string
}

// empty reports whether the result carries no lyrics at all.
func (r lyricsResult) empty() bool {
	return len(r.plain) == 0 && strings.TrimSpace(r.synced) == ""
}

// display picks the best representation: synced lyrics (parsed from the raw
// LRC, so lines can be highlighted) when available, otherwise the plain lines.
func (r lyricsResult) display() Lyrics {
	if lines := parseLRC(r.synced); len(lines) > 0 {
		return Lyrics{Lines: lines, Synced: true}
	}
	return Lyrics{Lines: plainLyricLines(r.plain)}
}

// lyricsMsg carries fetched lyrics back to the UI.
type lyricsMsg struct {
	videoID string
	lyrics  Lyrics
	found   bool
	err     error
}

// lrclibTrack mirrors the subset of LRCLIB's JSON we care about.
type lrclibTrack struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// fetchLyrics looks a track up on LRCLIB (no API key required). It first asks
// the precise /api/get endpoint; on a miss it falls back to /api/search and
// takes the first result that actually carries lyrics.
func fetchLyrics(t ytmusic.Track) (lyricsResult, bool, error) {
	if strings.TrimSpace(t.Title) == "" {
		return lyricsResult{}, false, fmt.Errorf("track has no title")
	}

	if res, ok, err := lrclibGet(t); err == nil && ok {
		return res, true, nil
	} else if err != nil {
		slog.Debug("lyrics: get failed, trying search", "err", err)
	}

	return lrclibSearch(t)
}

// fetchLyricsCached serves lyrics cache-first: the on-disk cache is consulted
// before any network request, and a successful (or negative) result is written
// back so later runs need no network. The in-memory cache in the model sits
// above this and avoids even the disk read within a session.
func fetchLyricsCached(t ytmusic.Track) (Lyrics, bool, error) {
	dir := lyricsCacheDir()
	if res, found, hit := loadDiskLyrics(dir, t); hit {
		return res.display(), found, nil
	}
	res, found, err := fetchLyrics(t)
	if err != nil {
		return Lyrics{}, false, err
	}
	if serr := saveDiskLyrics(dir, t, res, found); serr != nil {
		slog.Debug("lyrics: disk cache write failed", "err", serr)
	}
	return res.display(), found, nil
}

// lyricsCacheDir returns ~/.config/ytmusic-tui/lyrics.
func lyricsCacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ytmusic-tui", "lyrics")
}

// lyricsCacheEntry is the on-disk shape: whether lyrics were found plus both
// forms (plain lines and the raw synced LRC). Storing the not-found case lets a
// track with no lyrics skip LRCLIB on the next run too.
type lyricsCacheEntry struct {
	Found  bool     `json:"found"`
	Plain  []string `json:"plain,omitempty"`
	Synced string   `json:"synced,omitempty"`
}

// lyricsCacheKey hashes artist + title + album (case-insensitively) into a
// stable filename. The NUL separators prevent collisions between, say,
// ("ab","c") and ("a","bc").
func lyricsCacheKey(t ytmusic.Track) string {
	h := sha256.New()
	for _, part := range []string{t.Artist, t.Title, t.Album} {
		_, _ = io.WriteString(h, strings.ToLower(strings.TrimSpace(part)))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// loadDiskLyrics reads the cached entry for t from dir. hit is false when
// there is no readable entry; found reports whether lyrics were available.
func loadDiskLyrics(dir string, t ytmusic.Track) (res lyricsResult, found, hit bool) {
	path := filepath.Join(dir, lyricsCacheKey(t)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return lyricsResult{}, false, false
	}
	var e lyricsCacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return lyricsResult{}, false, false
	}
	return lyricsResult{plain: e.Plain, synced: e.Synced}, e.Found, true
}

// saveDiskLyrics writes the cache entry for t into dir with 0700/0600
// permissions, atomically via a temp file.
func saveDiskLyrics(dir string, t ytmusic.Track, res lyricsResult, found bool) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// MkdirAll honours umask; force the directory private.
	if err := os.Chmod(dir, 0o700); err != nil {
		slog.Debug("lyrics: chmod cache dir failed", "err", err)
	}
	data, err := json.Marshal(lyricsCacheEntry{Found: found, Plain: res.plain, Synced: res.synced})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".lyrics-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, lyricsCacheKey(t)+".json"))
}

// lrclibGet queries the precise lookup endpoint.
func lrclibGet(t ytmusic.Track) (lyricsResult, bool, error) {
	q := url.Values{}
	q.Set("artist_name", t.Artist)
	q.Set("track_name", t.Title)
	if t.Album != "" {
		q.Set("album_name", t.Album)
	}
	if secs, ok := parseDurationString(t.Duration); ok {
		q.Set("duration", strconv.Itoa(secs))
	}

	var track lrclibTrack
	status, err := lrclibJSON(lyricsEndpoint+"/get?"+q.Encode(), &track)
	if err != nil {
		return lyricsResult{}, false, err
	}
	if status == http.StatusNotFound || status != http.StatusOK {
		return lyricsResult{}, false, nil
	}
	return resultFromTrack(track), lyricsHasContent(track), nil
}

// lrclibSearch queries the fuzzy search endpoint and returns the first result
// that carries lyrics.
func lrclibSearch(t ytmusic.Track) (lyricsResult, bool, error) {
	q := url.Values{}
	q.Set("artist_name", t.Artist)
	q.Set("track_name", t.Title)

	var results []lrclibTrack
	status, err := lrclibJSON(lyricsEndpoint+"/search?"+q.Encode(), &results)
	if err != nil {
		return lyricsResult{}, false, err
	}
	if status != http.StatusOK {
		return lyricsResult{}, false, nil
	}
	for _, r := range results {
		if lyricsHasContent(r) {
			return resultFromTrack(r), true, nil
		}
	}
	return lyricsResult{}, false, nil
}

// lrclibJSON performs a GET and decodes a JSON response, returning the HTTP
// status code (so callers can distinguish a 404 from a transport error).
func lrclibJSON(endpoint string, out any) (int, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", lyricsUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := lyricsClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return resp.StatusCode, nil
	}
	// Bound the body defensively; lyrics payloads are small.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode lyrics: %w", err)
	}
	return resp.StatusCode, nil
}

// lyricsHasContent reports whether a result actually contains lyrics.
func lyricsHasContent(t lrclibTrack) bool {
	if t.Instrumental {
		return false
	}
	return strings.TrimSpace(t.PlainLyrics) != "" || strings.TrimSpace(t.SyncedLyrics) != ""
}

// resultFromTrack extracts both lyric forms from an LRCLIB result, keeping the
// raw synced document so its timestamps survive.
func resultFromTrack(t lrclibTrack) lyricsResult {
	var r lyricsResult
	if strings.TrimSpace(t.PlainLyrics) != "" {
		r.plain = splitNonEmpty(t.PlainLyrics, false)
	}
	if strings.TrimSpace(t.SyncedLyrics) != "" {
		r.synced = t.SyncedLyrics
	}
	return r
}

// --- LRC parsing & timing --------------------------------------------------

// parseLRC parses a synced LRC document into time-stamped display lines, sorted
// by start time. Metadata tags ([ar:], [ti:], [by:], [offset:], ...) are
// stripped; a line carrying several timestamps (a repeated chorus) yields one
// entry per timestamp. Text without any timestamp is ignored: a synced
// document's value is its timing. An [offset:±ms] tag shifts every line.
func parseLRC(raw string) []lyricLine {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	offset := 0.0
	if m := lrcOffset.FindStringSubmatch(raw); len(m) == 2 {
		if ms, err := strconv.Atoi(m[1]); err == nil {
			offset = float64(ms) / 1000
		}
	}

	var out []lyricLine
	for _, ln := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		times := lrcTime.FindAllStringSubmatch(ln, -1)
		if len(times) == 0 {
			continue
		}
		// Strip timestamps first, then any remaining metadata tags, so a line
		// like "[00:01.00][ar:X]hi" (unusual but possible) still reads "hi".
		text := strings.TrimSpace(lrcTime.ReplaceAllString(ln, ""))
		text = strings.TrimSpace(lrcTag.ReplaceAllString(text, ""))
		if text == "" {
			continue
		}
		for _, tm := range times {
			out = append(out, lyricLine{time: lrcSeconds(tm) + offset, text: text})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].time < out[j].time })
	return out
}

// lrcSeconds converts the capture groups of lrcTime into seconds, honouring the
// fractional part's digit count.
func lrcSeconds(m []string) float64 {
	min, _ := strconv.Atoi(m[1])
	sec, _ := strconv.Atoi(m[2])
	frac := 0.0
	if len(m) >= 4 && m[3] != "" {
		f, _ := strconv.Atoi(m[3])
		switch len(m[3]) {
		case 1:
			frac = float64(f) / 10
		case 2:
			frac = float64(f) / 100
		default:
			frac = float64(f) / 1000
		}
	}
	return float64(min*60+sec) + frac
}

// activeLyricLine returns the index of the line to highlight at position t
// (seconds): the last line whose start time is <= t. It returns -1 before the
// first line and for empty lyrics. lines must be sorted by time.
func activeLyricLine(lines []lyricLine, t float64) int {
	lo, hi := 0, len(lines)-1
	idx := -1
	for lo <= hi {
		mid := (lo + hi) / 2
		if lines[mid].time <= t {
			idx = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return idx
}

// plainLyricLines converts plain lyric strings into unsynced lyricLines.
func plainLyricLines(lines []string) []lyricLine {
	out := make([]lyricLine, len(lines))
	for i, ln := range lines {
		out[i] = lyricLine{time: -1, text: ln}
	}
	return out
}

// splitNonEmpty splits s into trimmed, non-empty lines. When strip is true each
// line is also passed through stripTimestamps.
func splitNonEmpty(s string, strip bool) []string {
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, ln := range raw {
		if strip {
			ln = stripTimestamps(ln)
		} else {
			ln = strings.TrimRight(ln, " \t")
		}
		if strings.TrimSpace(ln) == "" {
			continue
		}
		out = append(out, ln)
	}
	return out
}

// stripTimestamps removes LRC tags (timestamps and metadata) from a single
// line, returning the remaining lyric text.
func stripTimestamps(line string) string {
	return strings.TrimSpace(lrcTag.ReplaceAllString(line, ""))
}

// parseDurationString converts a "M:SS" or "H:MM:SS" duration into seconds.
func parseDurationString(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0, false
		}
		total = total*60 + n
	}
	return total, true
}
