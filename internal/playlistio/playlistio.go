// Package playlistio imports and exports playlists/queues as M3U and CSV.
//
// Exports are written under ~/.config/ytmusic-tui/exports/ and imports are read
// from ~/.config/ytmusic-tui/import.m3u, so the feature never touches files
// outside the app's own config directory. The formats are deliberately plain:
// an M3U uses the extended #EXTINF directive and a watch URL per track; a CSV
// carries the full track metadata. Both round-trip losslessly for our fields.
package playlistio

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

const appDir = "ytmusic-tui"

// Dir returns ~/.config/ytmusic-tui (the app's config directory).
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config", appDir)
}

// ExportDir returns the directory exports are written to.
func ExportDir() string { return filepath.Join(Dir(), "exports") }

// DefaultImportPath returns the fixed path an in-app import reads from.
func DefaultImportPath() string { return filepath.Join(Dir(), "import.m3u") }

// Entry is one parsed M3U line: a URL or local path plus optional metadata.
type Entry struct {
	// URL is the raw location from the M3U (a watch URL or a local file path).
	URL string
	// VideoID is extracted from the URL when it is a YouTube watch link, or
	// derived from a local file's basename when it matches <id>.<ext>.
	VideoID string
	// Title/Artist come from the #EXTINF directive when present.
	Title  string
	Artist string
	// DurationSec is the #EXTINF duration in seconds (0 when absent).
	DurationSec int
}

// ExportM3U writes tracks as an extended M3U to path, creating parent dirs.
func ExportM3U(path string, tracks []ytmusic.Track) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, t := range tracks {
		b.WriteString(fmt.Sprintf("#EXTINF:%d,%s\n", durationSeconds(t.Duration), extinfLabel(t)))
		b.WriteString(trackLocation(t))
		b.WriteString("\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// ExportCSV writes tracks as CSV (with a header row) to path.
func ExportCSV(path string, tracks []ytmusic.Track) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"videoId", "title", "artist", "album", "duration", "url"}); err != nil {
		return err
	}
	for _, t := range tracks {
		row := []string{t.VideoID, t.Title, t.Artist, t.Album, t.Duration, trackLocation(t)}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// ImportM3U parses an M3U/M3U8 file into entries. Missing files and malformed
// lines are handled gracefully: a missing file is an error, but individual bad
// lines are skipped.
func ImportM3U(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseM3U(string(data)), nil
}

// ParseM3U parses M3U text into entries.
func ParseM3U(text string) []Entry {
	var entries []Entry
	var pending *Entry
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#EXTM3U") {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			e := parseExtinf(line)
			pending = &e
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue // other directives are ignored
		}
		e := Entry{URL: line}
		if pending != nil {
			e.Title, e.Artist, e.DurationSec = pending.Title, pending.Artist, pending.DurationSec
			pending = nil
		}
		e.VideoID = VideoIDFromLocation(e.URL)
		entries = append(entries, e)
	}
	return entries
}

// parseExtinf parses a "#EXTINF:<seconds>,<Artist> - <Title>" directive.
func parseExtinf(line string) Entry {
	rest := strings.TrimPrefix(line, "#EXTINF:")
	var e Entry
	if comma := strings.IndexByte(rest, ','); comma >= 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(rest[:comma])); err == nil {
			e.DurationSec = n
		}
		rest = rest[comma+1:]
	}
	rest = strings.TrimSpace(rest)
	if artist, title, ok := strings.Cut(rest, " - "); ok {
		e.Artist = strings.TrimSpace(artist)
		e.Title = strings.TrimSpace(title)
	} else {
		e.Title = rest
	}
	return e
}

// VideoIDFromLocation extracts a YouTube video ID from a watch/share URL. It
// returns "" when nothing recognisable is found.
func VideoIDFromLocation(loc string) string {
	loc = strings.TrimSpace(loc)
	if loc == "" {
		return ""
	}
	if !strings.Contains(loc, "://") {
		return ""
	}
	return videoIDFromURL(loc)
}

// videoIDFromURL pulls the v= parameter (or a youtu.be/<id> path) from a URL.
func videoIDFromURL(u string) string {
	if i := strings.Index(u, "v="); i >= 0 {
		id := u[i+2:]
		if j := strings.IndexAny(id, "&#?"); j >= 0 {
			id = id[:j]
		}
		if isVideoID(id) {
			return id
		}
	}
	if i := strings.Index(u, "youtu.be/"); i >= 0 {
		id := u[i+len("youtu.be/"):]
		if j := strings.IndexAny(id, "&#?"); j >= 0 {
			id = id[:j]
		}
		if isVideoID(id) {
			return id
		}
	}
	return ""
}

// isVideoID reports whether s looks like a YouTube video ID (the standard
// 11-character [A-Za-z0-9_-] alphabet).
func isVideoID(s string) bool {
	if len(s) != 11 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// trackLocation returns what should be written for a track: its watch URL when
// known, otherwise a URL derived from the videoID.
func trackLocation(t ytmusic.Track) string {
	if t.URL != "" {
		return t.URL
	}
	if t.VideoID != "" {
		return "https://music.youtube.com/watch?v=" + t.VideoID
	}
	return ""
}

// extinfLabel renders the "Artist - Title" portion of an #EXTINF line.
func extinfLabel(t ytmusic.Track) string {
	switch {
	case t.Artist == "":
		return t.Title
	case t.Title == "":
		return t.Artist
	default:
		return t.Artist + " - " + t.Title
	}
}

// durationSeconds converts a "M:SS"/"H:MM:SS" string to seconds.
func durationSeconds(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	parts := strings.Split(s, ":")
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return total
}

// ExportName sanitises a playlist/queue name into a filesystem-safe stem.
func ExportName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "queue"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "queue"
	}
	return out
}

// WriteExport writes both an M3U and a CSV for tracks into ExportDir, returning
// the two paths. The file stem is name plus a timestamp so exports never
// clobber each other.
func WriteExport(name string, tracks []ytmusic.Track, now time.Time) (m3uPath, csvPath string, err error) {
	stem := fmt.Sprintf("%s-%s", ExportName(name), now.Format("20060102-150405"))
	m3uPath = filepath.Join(ExportDir(), stem+".m3u")
	csvPath = filepath.Join(ExportDir(), stem+".csv")
	if err := ExportM3U(m3uPath, tracks); err != nil {
		return "", "", err
	}
	if err := ExportCSV(csvPath, tracks); err != nil {
		return "", "", err
	}
	return m3uPath, csvPath, nil
}
