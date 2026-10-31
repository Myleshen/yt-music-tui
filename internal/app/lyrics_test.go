package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

func TestStripTimestamps(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"[00:12.34]Hello world", "Hello world"},
		{"[00:01.00][00:05.50]La la", "La la"},
		{"[ar:Some Artist]", ""},
		{"[length:03:45]", ""},
		{"No tags here", "No tags here"},
		{"  [01:02.003]  trailing  ", "trailing"},
	}
	for _, c := range cases {
		if got := stripTimestamps(c.in); got != c.want {
			t.Errorf("stripTimestamps(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitNonEmpty_SyncedLyrics(t *testing.T) {
	synced := "[00:01.00]Line one\n[00:03.20]Line two\n\n[00:05.00]Line three\n"
	got := splitNonEmpty(synced, true)
	want := []string{"Line one", "Line two", "Line three"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitNonEmpty(synced) = %v, want %v", got, want)
	}
}

func TestSplitNonEmpty_PlainLyricsPreserved(t *testing.T) {
	plain := "  first line  \nsecond line\n\nthird\n"
	got := splitNonEmpty(plain, false)
	want := []string{"  first line", "second line", "third"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitNonEmpty(plain) = %v, want %v", got, want)
	}
}

func TestParseLRC(t *testing.T) {
	raw := "[ar:Some Artist]\n[ti:A Song]\n" +
		"[00:12.34]Hello world\n" +
		"[00:05]First line\n" +
		"[00:30.5]Repeated chorus\n" +
		"[00:35.500][00:50.000]Repeated chorus\n" +
		"\n" +
		"[length:03:45]\n"

	got := parseLRC(raw)
	want := []lyricLine{
		{time: 5, text: "First line"},
		{time: 12.34, text: "Hello world"},
		{time: 30.5, text: "Repeated chorus"},
		{time: 35.5, text: "Repeated chorus"},
		{time: 50, text: "Repeated chorus"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseLRC = %#v, want %#v", got, want)
	}
}

func TestParseLRC_OffsetAndColonFraction(t *testing.T) {
	// A "[mm:ss:xx]" separator and a +500ms offset.
	got := parseLRC("[offset:+500]\n[00:10:25]shifted")
	want := []lyricLine{{time: 10.75, text: "shifted"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseLRC offset = %#v, want %#v", got, want)
	}

	// A negative offset works too.
	got = parseLRC("[offset:-1000]\n[00:02.00]earlier")
	want = []lyricLine{{time: 1, text: "earlier"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseLRC negative offset = %#v, want %#v", got, want)
	}
}

func TestParseLRC_PlainTextIgnored(t *testing.T) {
	// A document with no timestamps at all yields nothing (the plain path is
	// used instead).
	if got := parseLRC("just\nsome\nplain text"); len(got) != 0 {
		t.Errorf("parseLRC of untimed text = %#v, want empty", got)
	}
	// Empty lines and timestamp-only lines are skipped.
	got := parseLRC("[00:01.00]\n[00:02.00]real")
	want := []lyricLine{{time: 2, text: "real"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseLRC = %#v, want %#v", got, want)
	}
}

func TestActiveLyricLine(t *testing.T) {
	lines := []lyricLine{
		{time: 1, text: "a"},
		{time: 5, text: "b"},
		{time: 10, text: "c"},
	}
	cases := []struct {
		t    float64
		want int
	}{
		{-1, -1},
		{0, -1},
		{1, 0},
		{4.9, 0},
		{5, 1},
		{9.99, 1},
		{10, 2},
		{1000, 2},
	}
	for _, c := range cases {
		if got := activeLyricLine(lines, c.t); got != c.want {
			t.Errorf("activeLyricLine(t=%v) = %d, want %d", c.t, got, c.want)
		}
	}
	if got := activeLyricLine(nil, 5); got != -1 {
		t.Errorf("activeLyricLine(nil) = %d, want -1", got)
	}
}

func TestLyricsResult_DisplayPrefersSynced(t *testing.T) {
	// When both forms are present the synced form wins so lines can be
	// highlighted; the plain form is still cached for fallback.
	r := lyricsResult{
		plain:  []string{"plain a", "plain b"},
		synced: "[00:01.00]synced a\n[00:02.00]synced b",
	}
	got := r.display()
	if !got.Synced {
		t.Fatal("expected synced display when synced lyrics are present")
	}
	want := []lyricLine{{time: 1, text: "synced a"}, {time: 2, text: "synced b"}}
	if !reflect.DeepEqual(got.Lines, want) {
		t.Errorf("display lines = %#v, want %#v", got.Lines, want)
	}

	// Without synced lyrics the plain form is used, unsynced.
	got = lyricsResult{plain: []string{"plain a"}}.display()
	if got.Synced {
		t.Error("plain lyrics must not be marked synced")
	}
	if len(got.Lines) != 1 || got.Lines[0].time != -1 || got.Lines[0].text != "plain a" {
		t.Errorf("plain display = %#v", got.Lines)
	}

	// A synced document that fails to parse falls back to the plain form.
	got = lyricsResult{plain: []string{"fallback"}, synced: "no timestamps here"}.display()
	if got.Synced || len(got.Lines) != 1 || got.Lines[0].text != "fallback" {
		t.Errorf("unparseable synced should fall back to plain, got %#v", got)
	}
}

func TestLyricsHasContent(t *testing.T) {
	if lyricsHasContent(lrclibTrack{Instrumental: true, PlainLyrics: "x"}) {
		t.Error("instrumental tracks have no lyrics")
	}
	if lyricsHasContent(lrclibTrack{}) {
		t.Error("empty track has no lyrics")
	}
	if !lyricsHasContent(lrclibTrack{PlainLyrics: "hello"}) {
		t.Error("plain lyrics count as content")
	}
	if !lyricsHasContent(lrclibTrack{SyncedLyrics: "[00:01.00]hello"}) {
		t.Error("synced lyrics count as content")
	}
}

func TestParseDurationString(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"3:45", 225, true},
		{"0:59", 59, true},
		{"1:02:03", 3723, true},
		{"", 0, false},
		{"abc", 0, false},
		{"1:2:3:4", 0, false},
	}
	for _, c := range cases {
		got, ok := parseDurationString(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("parseDurationString(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestLyricsCacheKey_StableAndCaseInsensitive(t *testing.T) {
	a := lyricsCacheKey(ytmusic.Track{Artist: "The Beatles", Title: "Hey Jude", Album: "Past Masters"})
	b := lyricsCacheKey(ytmusic.Track{Artist: "the beatles", Title: "hey jude", Album: "past masters"})
	if a != b {
		t.Error("cache key should be case-insensitive")
	}
	if a == lyricsCacheKey(ytmusic.Track{Artist: "The Beatles", Title: "Revolution"}) {
		t.Error("different tracks must not share a cache key")
	}
	// NUL separators prevent ("ab","c") colliding with ("a","bc").
	if lyricsCacheKey(ytmusic.Track{Artist: "ab", Title: "c"}) == lyricsCacheKey(ytmusic.Track{Artist: "a", Title: "bc"}) {
		t.Error("field boundaries must be distinguished")
	}
	if len(a) != 64 {
		t.Errorf("expected a 64-char hex sha256, got %d chars", len(a))
	}
}

func TestDiskLyrics_RoundTripBothForms(t *testing.T) {
	dir := t.TempDir()
	track := ytmusic.Track{Artist: "Radiohead", Title: "Weird Fishes", Album: "In Rainbows"}
	res := lyricsResult{
		plain:  []string{"In the deepest ocean", "The bottom of the sea"},
		synced: "[00:01.00]In the deepest ocean\n[00:05.00]The bottom of the sea",
	}

	if err := saveDiskLyrics(dir, track, res, true); err != nil {
		t.Fatalf("saveDiskLyrics error = %v", err)
	}
	got, found, hit := loadDiskLyrics(dir, track)
	if !hit {
		t.Fatal("expected a cache hit after save")
	}
	if !found {
		t.Error("found should be true")
	}
	if !reflect.DeepEqual(got, res) {
		t.Errorf("result = %#v, want %#v", got, res)
	}
	// The synced form survives the round-trip with timestamps intact.
	if got.display().Lines[1].time != 5 {
		t.Errorf("synced timing lost: %#v", got.display().Lines)
	}

	// A different track is a miss.
	if _, _, hit := loadDiskLyrics(dir, ytmusic.Track{Artist: "Radiohead", Title: "Nude"}); hit {
		t.Error("unrelated track should miss")
	}
}

func TestDiskLyrics_NegativeResultRoundTrips(t *testing.T) {
	dir := t.TempDir()
	track := ytmusic.Track{Artist: "Aphex Twin", Title: "Avril 14th"}
	if err := saveDiskLyrics(dir, track, lyricsResult{}, false); err != nil {
		t.Fatalf("saveDiskLyrics error = %v", err)
	}
	res, found, hit := loadDiskLyrics(dir, track)
	if !hit {
		t.Fatal("expected a cache hit for the negative entry")
	}
	if found {
		t.Error("found should be false for a no-lyrics entry")
	}
	if !res.empty() {
		t.Errorf("expected no lyrics, got %#v", res)
	}
}

func TestDiskLyrics_Permissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lyrics")
	track := ytmusic.Track{Artist: "A", Title: "B"}
	if err := saveDiskLyrics(dir, track, lyricsResult{plain: []string{"x"}}, true); err != nil {
		t.Fatalf("saveDiskLyrics error = %v", err)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("cache dir perm = %o, want 700", perm)
	}
	fi, err := os.Stat(filepath.Join(dir, lyricsCacheKey(track)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache file perm = %o, want 600", perm)
	}
	// No temp files should be left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected exactly 1 cache file, found %d", len(entries))
	}
}

func TestDiskLyrics_MissingDirIsMiss(t *testing.T) {
	if _, _, hit := loadDiskLyrics(filepath.Join(t.TempDir(), "absent"), ytmusic.Track{Title: "x"}); hit {
		t.Error("loading from a missing dir should miss, not error")
	}
}

func TestDiskLyrics_CorruptEntryIsMiss(t *testing.T) {
	dir := t.TempDir()
	track := ytmusic.Track{Artist: "A", Title: "B"}
	if err := os.WriteFile(filepath.Join(dir, lyricsCacheKey(track)+".json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, hit := loadDiskLyrics(dir, track); hit {
		t.Error("corrupt cache entry should be treated as a miss")
	}
}

func TestLyricsCacheDir(t *testing.T) {
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "ytmusic-tui", "lyrics")
	if got := lyricsCacheDir(); got != want {
		t.Errorf("lyricsCacheDir() = %q, want %q", got, want)
	}
}
