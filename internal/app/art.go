package app

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.Decode
	_ "image/png"  // register PNG decoder for the rare PNG thumbnail
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

const (
	// artMaxCols is the widest album art, in terminal cells. Each cell encodes
	// two vertical pixels, so a square source renders artMaxCols x artMaxCols/2
	// = 40x20 cells (~40x40 px): big enough to read as a real image without
	// dominating the layout.
	artMaxCols = 40
	// artAbsoluteMaxCols bounds a user-configured art width (ui.art_size), so a
	// nonsensical value cannot swallow the whole terminal.
	artAbsoluteMaxCols = 80
	// artMaxRows bounds the height so a portrait source cannot push the art
	// taller than this many terminal rows.
	artMaxRows = 20
	// artMinCols is the floor for narrow terminals, so the art never collapses
	// to an unreadable sliver before the artMinWidth gate disables it entirely.
	artMinCols = 12
	// artMinWidth is the narrowest terminal that still shows art. Below this
	// the layout degenerates and we skip art entirely.
	artMinWidth = 60

	artFetchTimeout = 12 * time.Second

	// lh3Size is the size directive appended to lh3 Google thumbnails: request a
	// much larger source than the default 120px thumb so the downscale to
	// terminal cells stays crisp.
	lh3Size = "=w1080-h1080-l90-rj"

	// artCacheVersion namespaces the on-disk art cache. Old low-resolution files
	// live in the parent dir and are ignored; bumping this discards the whole
	// cache.
	artCacheVersion = "v2"
)

// artHTTPClient is shared by all art fetches (connection reuse, sane timeout).
var artHTTPClient = &http.Client{Timeout: artFetchTimeout}

// artMaxColsActive is the effective album-art width cap. It defaults to
// artMaxCols ("auto") and is adjusted once at startup from ui.art_size via
// SetArtMaxCols.
var artMaxColsActive = artMaxCols

// SetArtMaxCols sets the album-art width cap from config. n <= 0 means "auto"
// (use the built-in cap); values are clamped to [artMinCols, artAbsoluteMaxCols].
func SetArtMaxCols(n int) {
	switch {
	case n <= 0:
		artMaxColsActive = artMaxCols
	case n < artMinCols:
		artMaxColsActive = artMinCols
	case n > artAbsoluteMaxCols:
		artMaxColsActive = artAbsoluteMaxCols
	default:
		artMaxColsActive = n
	}
}

// parseArtSize interprets the ui.art_size string: "auto" (or anything
// unparseable) yields 0, an integer yields that width.
func parseArtSize(s string) int {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "auto" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// artResultMsg carries a rendered album-art block back to the UI.
type artResultMsg struct {
	videoID  string
	width    int
	rendered string
	err      error
}

// artFileMsg reports a cached cover-art file for MPRIS clients.
type artFileMsg struct {
	videoID string
	path    string
	err     error
}

// lh3SizeParams matches Google's lh3 thumbnail size directives, e.g.
// "=w120-h120-l90-rj". We rewrite them to request a crisper source image.
var lh3SizeParams = regexp.MustCompile(`=w\d+-h\d+-[a-z0-9-]+`)

// artKey identifies a cached render by video AND terminal width, because the
// rendered ANSI depends on how wide we rasterise.
func artKey(videoID string, width int) string {
	return videoID + "@" + strconv.Itoa(width)
}

// artDir returns ~/.config/ytmusic-tui/art (created on demand).
func artDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ytmusic-tui", "art")
}

// artCachePath returns the on-disk path for a video's image at a specific URL.
// Hashing the URL keeps entries for different resolutions/sources apart, so a
// previously cached low-res JPEG is never reused for a high-res request; the
// version directory discards anything written by an older layout.
func artCachePath(videoID, url string) string {
	sum := sha256.Sum256([]byte(url))
	name := fmt.Sprintf("%s-%x.jpg", videoID, sum[:8])
	return filepath.Join(artDir(), artCacheVersion, name)
}

// artURLs returns the candidate image URLs for a track, in preference order.
// The first is the best available source; the rest are fallbacks tried when an
// earlier candidate 404s (notably YouTube's maxresdefault, which is absent for
// some videos).
func artURLs(t ytmusic.Track) []string {
	if t.Thumbnail != "" {
		thumb := t.Thumbnail
		if strings.Contains(thumb, "lh3.googleusercontent.com") {
			if lh3SizeParams.MatchString(thumb) {
				return []string{lh3SizeParams.ReplaceAllString(thumb, lh3Size)}
			}
			return []string{thumb + lh3Size}
		}
		return []string{thumb}
	}
	if t.VideoID != "" {
		base := "https://i.ytimg.com/vi/" + t.VideoID + "/"
		return []string{
			base + "maxresdefault.jpg",
			base + "sddefault.jpg",
			base + "hqdefault.jpg",
		}
	}
	return nil
}

// artURL returns the preferred image URL, or "" when none is available. It is
// the first entry of artURLs.
func artURL(t ytmusic.Track) string {
	urls := artURLs(t)
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

// fetchAndRenderArt loads a track's thumbnail (disk cache first), decodes it,
// and rasterises it into an ANSI half-block string at the given width. It runs
// inside a tea.Cmd, so it must never touch model state and must not block the
// UI goroutine.
func fetchAndRenderArt(t ytmusic.Track, width int) (string, error) {
	if t.VideoID == "" {
		return "", fmt.Errorf("no video ID")
	}
	if width < artMinWidth {
		return "", nil
	}
	urls := artURLs(t)
	if len(urls) == 0 {
		return "", fmt.Errorf("no art URL")
	}
	data, err := loadArtBytes(t.VideoID, urls)
	if err != nil {
		return "", err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode art: %w", err)
	}
	return renderArt(img, width), nil
}

// loadArtFile returns the on-disk cache path for videoID's image, reading the
// cache when present and otherwise downloading (and persisting) it. Candidate
// URLs are tried in order, so a 404 on a higher-resolution source falls through
// to the next; the last error is returned if every candidate fails.
func loadArtFile(videoID string, urls []string) (string, error) {
	var lastErr error
	for _, url := range urls {
		path := artCachePath(videoID, url)
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return path, nil
		}
		data, err := downloadArt(url)
		if err != nil {
			lastErr = err
			continue
		}
		if err := persistArt(path, data); err != nil {
			// Without a file there is nothing to hand to MPRIS clients, so
			// treat a failed write as a failed candidate.
			lastErr = err
			continue
		}
		return path, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no art URLs")
	}
	return "", lastErr
}

// loadArtBytes returns the raw image for videoID. See loadArtFile.
func loadArtBytes(videoID string, urls []string) ([]byte, error) {
	path, err := loadArtFile(videoID, urls)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cached art: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty cached art")
	}
	return data, nil
}

// artFileForTrack resolves a track's cover image to a cached file, downloading
// it when needed. Unlike fetchAndRenderArt it does not depend on the terminal
// width, so MPRIS clients can show artwork even in a narrow TUI.
func artFileForTrack(t ytmusic.Track) (string, error) {
	if t.VideoID == "" {
		return "", fmt.Errorf("no video ID")
	}
	urls := artURLs(t)
	if len(urls) == 0 {
		return "", fmt.Errorf("no art URL")
	}
	return loadArtFile(t.VideoID, urls)
}

// artFileURL converts a cached art path into the file:// URI MPRIS expects.
func artFileURL(path string) string {
	if path == "" {
		return ""
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// downloadArt fetches a single image URL, bounded by a 16MB read limit.
func downloadArt(url string) ([]byte, error) {
	resp, err := artHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch art: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch art: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read art: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty art response")
	}
	return data, nil
}

// persistArt writes the downloaded image into the art cache with 0700/0600
// permissions, atomically via a temp file.
func persistArt(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		slog.Debug("art: chmod dir failed", "err", err)
	}
	tmp, err := os.CreateTemp(dir, ".art-*.tmp")
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
	return os.Rename(tmpName, path)
}

// artSize computes the terminal cell grid for an image of the given pixel
// dimensions on a terminal termWidth cells wide, preserving the source aspect
// ratio. Pixels are cols wide by rows*2 tall, so the grid satisfies
// cols / (rows*2) ≈ imgW / imgH. The width targets roughly a third of the
// terminal, capped at artMaxCols/artMaxRows so the art never dominates.
func artSize(termWidth, imgW, imgH int) (cols, rows int) {
	if termWidth < artMinWidth || imgW <= 0 || imgH <= 0 {
		return 0, 0
	}
	aspect := float64(imgW) / float64(imgH)

	target := termWidth / 3
	if target > artMaxColsActive {
		target = artMaxColsActive
	}
	if target < artMinCols {
		target = artMinCols
	}

	rows = int(math.Round(float64(target) / (2 * aspect)))
	if rows < 1 {
		rows = 1
	}
	if rows > artMaxRows {
		rows = artMaxRows
	}
	// Derive cols from the (clamped) rows so the grid's aspect ratio stays
	// faithful to the source rather than the rough width target.
	cols = int(math.Round(float64(rows) * 2 * aspect))
	if cols < 1 {
		cols = 1
	}
	if cols > artMaxColsActive {
		cols = artMaxColsActive
	}
	return cols, rows
}

// renderArt rasterises img into an ANSI half-block string sized to the terminal
// while preserving the image's aspect ratio, or "" when the terminal is too
// narrow to make art worthwhile.
func renderArt(img image.Image, termWidth int) string {
	if img == nil || termWidth < artMinWidth {
		return ""
	}
	b := img.Bounds()
	cols, rows := artSize(termWidth, b.Dx(), b.Dy())
	if cols == 0 || rows == 0 {
		return ""
	}
	return renderHalfBlocks(img, cols, rows)
}

// centerCrop returns the largest sub-rectangle of src whose aspect ratio
// matches dstW:dstH, centred within src. Scaling that crop fills the
// destination exactly, so the image is never stretched -- only cropped.
func centerCrop(src image.Rectangle, dstW, dstH int) image.Rectangle {
	sw, sh := src.Dx(), src.Dy()
	if sw <= 0 || sh <= 0 || dstW <= 0 || dstH <= 0 {
		return src
	}
	srcAspect := float64(sw) / float64(sh)
	dstAspect := float64(dstW) / float64(dstH)
	if srcAspect > dstAspect {
		// Source is wider than the target: crop the sides.
		w := int(math.Round(float64(sh) * dstAspect))
		if w < 1 {
			w = 1
		}
		x0 := src.Min.X + (sw-w)/2
		return image.Rect(x0, src.Min.Y, x0+w, src.Max.Y)
	}
	// Source is taller (or equal): crop top and bottom.
	h := int(math.Round(float64(sw) / dstAspect))
	if h < 1 {
		h = 1
	}
	y0 := src.Min.Y + (sh-h)/2
	return image.Rect(src.Min.X, y0, src.Max.X, y0+h)
}

// renderHalfBlocks converts an image to cols x rows terminal cells using the
// upper-half-block glyph "▀": the foreground colour is the top pixel and the
// background colour is the bottom pixel, so each cell encodes two vertical
// pixels. The source is centre-cropped to the destination aspect ratio first,
// so the result is never stretched. The result is truecolor ANSI, terminated
// with a reset.
func renderHalfBlocks(img image.Image, cols, rows int) string {
	if img == nil || cols <= 0 || rows <= 0 {
		return ""
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return ""
	}

	dstW, dstH := cols, rows*2
	crop := centerCrop(b, dstW, dstH)

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, crop, xdraw.Over, nil)

	var sb strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			top := dst.RGBAAt(x, y*2)
			bot := dst.RGBAAt(x, y*2+1)
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀",
				top.R, top.G, top.B, bot.R, bot.G, bot.B)
		}
		sb.WriteString("\x1b[0m")
		if y < rows-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
