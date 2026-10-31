package playlistio

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

func sample() []ytmusic.Track {
	return []ytmusic.Track{
		{VideoID: "dQw4w9WgXcQ", Title: "Never Gonna Give You Up", Artist: "Rick Astley", Album: "Whenever", Duration: "3:33", URL: "https://music.youtube.com/watch?v=dQw4w9WgXcQ"},
		{VideoID: "9bZkp7q19f0", Title: "Gangnam Style", Artist: "PSY", Duration: "4:12"},
	}
}

func TestM3U_roundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.m3u")
	if err := ExportM3U(path, sample()); err != nil {
		t.Fatalf("ExportM3U: %v", err)
	}
	entries, err := ImportM3U(path)
	if err != nil {
		t.Fatalf("ImportM3U: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("imported %d entries, want 2", len(entries))
	}
	if entries[0].VideoID != "dQw4w9WgXcQ" || entries[0].Artist != "Rick Astley" || entries[0].Title != "Never Gonna Give You Up" {
		t.Errorf("entry[0] = %+v", entries[0])
	}
	if entries[0].DurationSec != 213 {
		t.Errorf("entry[0].DurationSec = %d, want 213", entries[0].DurationSec)
	}
	if entries[1].VideoID != "9bZkp7q19f0" || entries[1].Title != "Gangnam Style" {
		t.Errorf("entry[1] = %+v", entries[1])
	}
}

func TestCSV_roundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.csv")
	if err := ExportCSV(path, sample()); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); len(got) == 0 || got[:7] != "videoId" {
		t.Errorf("CSV missing header: %q", got)
	}
}

func TestVideoIDFromLocation(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://music.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/watch?v=9bZkp7q19f0&t=30s", "9bZkp7q19f0"},
		{"https://youtu.be/dQw4w9WgXcQ?si=x", "dQw4w9WgXcQ"},
		{"/some/song.mp3", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := VideoIDFromLocation(tc.in); got != tc.want {
			t.Errorf("VideoIDFromLocation(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseM3U_skipsMalformedLines(t *testing.T) {
	// Comments and blank lines are skipped; a non-location line is kept (a
	// relative path is legal in M3U) but resolves to no video ID, so callers
	// can drop it. A bad #EXTINF duration is ignored rather than fatal.
	text := "#EXTM3U\n#EXTINF:213,Rick Astley - Never Gonna Give You Up\nhttps://music.youtube.com/watch?v=dQw4w9WgXcQ\n\n# a comment\nnot a url\n#EXTINF:bad\nhttps://youtu.be/9bZkp7q19f0\n"
	entries := ParseM3U(text)
	if len(entries) != 3 {
		t.Fatalf("parsed %d entries, want 3: %+v", len(entries), entries)
	}
	if entries[1].VideoID != "" {
		t.Errorf("junk line should resolve to no videoID: %+v", entries[1])
	}
	if entries[2].VideoID != "9bZkp7q19f0" || entries[2].DurationSec != 0 {
		t.Errorf("entry[2] = %+v", entries[2])
	}
}

func TestExportName(t *testing.T) {
	tests := map[string]string{
		"":                 "queue",
		"My Mix / 2026":    "My-Mix--2026",
		"chill beats.m3u":  "chill-beats-m3u",
		"already-safe_123": "already-safe_123",
	}
	for in, want := range tests {
		if got := ExportName(in); got != want {
			t.Errorf("ExportName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteExport(t *testing.T) {
	// Redirect HOME so the export lands in a temp dir.
	t.Setenv("HOME", t.TempDir())
	m3u, csv, err := WriteExport("My Queue", sample(), time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("WriteExport: %v", err)
	}
	for _, p := range []string{m3u, csv} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
	if filepath.Base(m3u) != "My-Queue-20261010-120000.m3u" {
		t.Errorf("m3u name = %s", filepath.Base(m3u))
	}
}
