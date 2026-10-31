package app

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// fastPlayerClient is the yt-dlp YouTube player client used for the first,
// fast stream-resolution attempt.
//
// Benchmarked on this machine for a cold (`--no-cache-dir`) `-f bestaudio -g`
// resolution of three tracks (median of 3 runs each):
//
//	client        cold avg   notes
//	android       ~3.6-3.8s  skips the webpage/JS (n-param) challenge
//	ios           ~3.6-3.9s
//	web           ~3.8-4.3s
//	default       ~4.5-5.1s  fetches the watch page + solves the JS challenge
//	tv_embedded   ~5.2s
//	mweb          ~5.1s
//	web_safari    ~5.7s
//
// "android" is consistently the fastest reliable client and returns the same
// audio/webm stream shape as the default (verified playable through mpv).
// yt-dlp remains the sole resolver and every attempt falls back to the default
// client, so a client change on YouTube's side degrades to the previous
// behaviour rather than breaking playback.
const fastPlayerClient = "android"

// ytdlpAttempts returns the ordered yt-dlp argument lists to try when resolving
// a stream URL. Each list is a complete argument set except for the final URL,
// which the caller appends. The fast client is tried first; later attempts use
// the maximally-compatible default. When a cookies file is available on disk it
// is attached to the earlier attempts.
func ytdlpAttempts(cookiesFile string, cookiesExist bool) [][]string {
	base := []string{"-f", "bestaudio", "-g", "--quiet", "--no-warnings"}
	fast := []string{"--extractor-args", "youtube:player_client=" + fastPlayerClient}
	mk := func(extra []string, withCookies bool) []string {
		a := make([]string, 0, len(base)+len(extra)+3)
		a = append(a, base...)
		a = append(a, extra...)
		if withCookies {
			a = append(a, "--cookies", cookiesFile)
		}
		return a
	}

	var out [][]string
	if cookiesExist {
		out = append(out, mk(fast, true), mk(nil, true))
	} else {
		out = append(out, mk(fast, false))
	}
	// Always end with the plain default client, without cookies: the most
	// compatible command, mirroring the pre-speedup behaviour.
	out = append(out, mk(nil, false))
	return out
}

// extractStreamURL resolves a playable audio stream URL via yt-dlp.
//
// yt-dlp is far more robust here than scraping the watch page in-process: it
// uses the InnerTube player API, handles signature/n-param descrambling, and is
// gentle enough not to trip YouTube's abuse detection. We try the fast player
// client first (see fastPlayerClient) and fall back to the default; when a
// cookies file is set we also try with our authenticated cookies so the request
// counts as logged-in (less throttling, and age-restricted tracks work).
func extractStreamURL(cookiesFile, ytURL string) (string, error) {
	cookiesExist := false
	if cookiesFile != "" {
		if _, err := os.Stat(cookiesFile); err == nil {
			cookiesExist = true
		}
	}

	var lastErr error
	for i, args := range ytdlpAttempts(cookiesFile, cookiesExist) {
		url, err := runYtDlp(append(append([]string{}, args...), ytURL)...)
		if err == nil {
			if i > 0 {
				slog.Debug("stream resolved via fallback attempt", "attempt", i)
			}
			return url, nil
		}
		lastErr = err
		slog.Debug("yt-dlp attempt failed", "attempt", i, "err", err)
	}
	return "", fmt.Errorf("yt-dlp: %w", lastErr)
}

func runYtDlp(args ...string) (string, error) {
	out, err := exec.Command("yt-dlp", args...).Output()
	if err != nil {
		return "", err
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", fmt.Errorf("yt-dlp returned empty URL")
	}
	return url, nil
}

// resolveMedia returns what mpv should play for a track. It uses the stream-URL
// cache when possible and otherwise resolves a fresh stream via yt-dlp. It
// performs blocking I/O and is only ever called from a tea.Cmd goroutine.
func (m Model) resolveMedia(track ytmusic.Track) (string, error) {
	if cached, ok := m.urlCache.Get(track.VideoID); ok {
		slog.Debug("stream URL cache hit", "videoID", track.VideoID)
		return cached, nil
	}
	slog.Info("cache miss — fetching stream URL", "videoID", track.VideoID, "title", track.Title)
	url, err := streamResolver(m.cookiesFile, track.URL)
	if err != nil {
		return "", err
	}
	m.urlCache.Set(track.VideoID, url)
	return url, nil
}
