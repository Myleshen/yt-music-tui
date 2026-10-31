package app

import (
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// synthImage builds a small two-tone image so the half-block renderer has
// distinct top/bottom pixels to work with.
func synthImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if y < 2 {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	return img
}

func TestRenderHalfBlocks_EmitsCellsAndReset(t *testing.T) {
	out := renderHalfBlocks(synthImage(), 4, 2)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	// 4 cols x 2 rows = 8 upper-half-block cells.
	if got := strings.Count(out, "▀"); got != 8 {
		t.Errorf("expected 8 block glyphs, got %d", got)
	}
	// Two rows -> one newline separator, no trailing newline.
	if got := strings.Count(out, "\n"); got != 1 {
		t.Errorf("expected 1 newline for 2 rows, got %d", got)
	}
	if !strings.HasSuffix(out, "\x1b[0m") {
		t.Errorf("output should end with an ANSI reset, got %q", out[maxInt(0, len(out)-8):])
	}
	if !strings.Contains(out, "\x1b[38;2;") {
		t.Error("expected truecolor foreground escapes")
	}
	if !strings.Contains(out, "\x1b[48;2;") {
		t.Error("expected truecolor background escapes")
	}
}

func TestRenderHalfBlocks_DegenerateInputs(t *testing.T) {
	if got := renderHalfBlocks(nil, 4, 2); got != "" {
		t.Errorf("nil image should render empty, got %q", got)
	}
	if got := renderHalfBlocks(synthImage(), 0, 2); got != "" {
		t.Errorf("zero cols should render empty, got %q", got)
	}
	if got := renderHalfBlocks(synthImage(), 2, 0); got != "" {
		t.Errorf("zero rows should render empty, got %q", got)
	}
	if got := renderHalfBlocks(&image.RGBA{}, 2, 2); got != "" {
		t.Errorf("empty-bounds image should render empty, got %q", got)
	}
}

func TestRenderArt_NarrowTerminalSkips(t *testing.T) {
	img := synthImage()
	if got := renderArt(img, artMinWidth-1); got != "" {
		t.Errorf("narrow terminal should skip art, got %d bytes", len(got))
	}
	out := renderArt(img, artMinWidth)
	if out == "" {
		t.Fatal("wide terminal should render art")
	}
	// The grid is computed from the (square) source aspect ratio.
	cols, rows := artSize(artMinWidth, 4, 4)
	if got := strings.Count(out, "\n"); got != rows-1 {
		t.Errorf("expected %d newlines, got %d", rows-1, got)
	}
	if got := strings.Count(out, "▀"); got != cols*rows {
		t.Errorf("expected %d cells, got %d", cols*rows, got)
	}
}

func TestArtSize_AspectAndBounds(t *testing.T) {
	// Square source at a wide terminal: 40x20 cells, matching the ~40px default.
	cols, rows := artSize(120, 1000, 1000)
	if cols != artMaxCols || rows != artMaxCols/2 {
		t.Errorf("square @120 = %dx%d, want %dx%d", cols, rows, artMaxCols, artMaxCols/2)
	}

	// 16:9 source: wider than tall, so fewer rows for the same width, and the
	// grid aspect (cols/(rows*2)) tracks the source.
	cols, rows = artSize(120, 1280, 720)
	gotAspect := float64(cols) / float64(rows*2)
	wantAspect := 1280.0 / 720.0
	if diff := gotAspect - wantAspect; diff > 0.1 || diff < -0.1 {
		t.Errorf("16:9 grid aspect %.3f, want ~%.3f (%dx%d)", gotAspect, wantAspect, cols, rows)
	}
	if rows >= artMaxCols/2 {
		t.Errorf("16:9 should be shorter than square, got %d rows", rows)
	}

	// Portrait source is clamped to artMaxRows and never exceeds artMaxCols.
	cols, rows = artSize(120, 720, 1280)
	if rows > artMaxRows || cols > artMaxCols {
		t.Errorf("portrait exceeded caps: %dx%d", cols, rows)
	}

	// Narrow terminal: ~1/3 width, no more than the cap.
	cols, rows = artSize(90, 1000, 1000)
	if cols > 30 || rows > 15 || cols < 1 || rows < 1 {
		t.Errorf("narrow grid = %dx%d, want <=30x15", cols, rows)
	}

	// Below the gate / bad dimensions -> no art.
	if c, r := artSize(artMinWidth-1, 1000, 1000); c != 0 || r != 0 {
		t.Errorf("below gate: got %dx%d, want 0x0", c, r)
	}
	if c, r := artSize(120, 0, 100); c != 0 || r != 0 {
		t.Errorf("zero width: got %dx%d, want 0x0", c, r)
	}
	if c, r := artSize(120, 100, 0); c != 0 || r != 0 {
		t.Errorf("zero height: got %dx%d, want 0x0", c, r)
	}
}

func TestCenterCrop_PreservesAspect(t *testing.T) {
	src := image.Rect(0, 0, 200, 100) // 2:1
	// Target 1:1 -> crop the width to 100x100, centred.
	crop := centerCrop(src, 2, 2)
	if crop.Dx() != 100 || crop.Dy() != 100 {
		t.Errorf("wide crop = %dx%d, want 100x100", crop.Dx(), crop.Dy())
	}
	if crop.Min.X != 50 {
		t.Errorf("crop not centred: Min.X=%d, want 50", crop.Min.X)
	}
	// Target 2:1 -> already matches, no crop.
	crop = centerCrop(src, 2, 1)
	if crop != src {
		t.Errorf("matching aspect should not crop, got %v", crop)
	}
}

func TestArtURL_UpgradesThumbnail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "lh3 sized",
			in:   "https://lh3.googleusercontent.com/abc=w120-h120-l90-rj",
			want: "https://lh3.googleusercontent.com/abc=w1080-h1080-l90-rj",
		},
		{
			name: "lh3 unsized",
			in:   "https://lh3.googleusercontent.com/abc",
			want: "https://lh3.googleusercontent.com/abc=w1080-h1080-l90-rj",
		},
	}
	for _, c := range cases {
		got := artURL(ytmusic.Track{Thumbnail: c.in})
		if got != c.want {
			t.Errorf("%s: artURL = %q, want %q", c.name, got, c.want)
		}
	}
	// Non-lh3 thumbnails pass through untouched.
	if got := artURL(ytmusic.Track{Thumbnail: "https://example.com/x.jpg"}); got != "https://example.com/x.jpg" {
		t.Errorf("passthrough artURL = %q", got)
	}
	// Fallback chain starts at maxresdefault when no thumbnail is present.
	if got := artURL(ytmusic.Track{VideoID: "vid123"}); got != "https://i.ytimg.com/vi/vid123/maxresdefault.jpg" {
		t.Errorf("fallback artURL = %q", got)
	}
	// No thumbnail and no video ID -> empty.
	if got := artURL(ytmusic.Track{}); got != "" {
		t.Errorf("empty artURL = %q", got)
	}
}

func TestArtURLs_FallbackChain(t *testing.T) {
	got := artURLs(ytmusic.Track{VideoID: "abc"})
	want := []string{
		"https://i.ytimg.com/vi/abc/maxresdefault.jpg",
		"https://i.ytimg.com/vi/abc/sddefault.jpg",
		"https://i.ytimg.com/vi/abc/hqdefault.jpg",
	}
	if len(got) != len(want) {
		t.Fatalf("artURLs len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("artURLs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := artURLs(ytmusic.Track{}); len(got) != 0 {
		t.Errorf("no track should give no URLs, got %v", got)
	}
}

func TestArtCachePath_KeyedByURLAndVersion(t *testing.T) {
	a := artCachePath("vid", "https://x/a.jpg")
	b := artCachePath("vid", "https://x/b.jpg")
	if a == b {
		t.Error("different URLs must map to different cache paths")
	}
	if a != artCachePath("vid", "https://x/a.jpg") {
		t.Error("same URL must map to a stable path")
	}
	if !strings.Contains(a, "vid") || !strings.Contains(a, artCacheVersion) {
		t.Errorf("cache path should carry videoID and version: %q", a)
	}
	if filepath.Base(filepath.Dir(a)) != artCacheVersion {
		t.Errorf("cache path should live under %q: %q", artCacheVersion, a)
	}
}
