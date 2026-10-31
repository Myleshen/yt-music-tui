package app

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Myleshen/yt-music-tui/internal/playlistio"
	"github.com/Myleshen/yt-music-tui/internal/stats"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// minPlaySeconds is the least listening time that counts as a play in the local
// stats. Shorter glances (a skip within a few seconds) are not recorded.
const minPlaySeconds = 5

// --- Messages ---

// statsMsg carries a freshly aggregated listening summary.
type statsMsg struct {
	summary stats.Summary
	err     error
}

// exportDoneMsg reports the outcome of an M3U/CSV export.
type exportDoneMsg struct {
	m3u string
	csv string
	n   int
	err error
}

// importDoneMsg reports the outcome of an M3U import.
type importDoneMsg struct {
	tracks  []ytmusic.Track
	skipped int
	err     error
}

// --- Listening stats ---

// loadStatsCmd loads the play history and aggregates it.
func (m Model) loadStatsCmd() tea.Cmd {
	st := m.store
	return func() tea.Msg {
		var plays []stats.Play
		if st != nil {
			plays = st.LoadPlays()
		}
		return statsMsg{summary: stats.Summarize(plays, time.Now())}
	}
}

// finalizePlay records the current track's listen into the local history and
// resets the accumulator. It is called when the track changes and on quit.
func (m *Model) finalizePlay() []tea.Cmd {
	id, seconds := m.recordingID, m.listenSeconds
	m.listenSeconds = 0
	if id == "" || seconds < minPlaySeconds || m.store == nil {
		return nil
	}
	p := stats.Play{
		VideoID: id,
		Title:   m.recordingTitle,
		Artist:  m.recordingArtist,
		At:      time.Now(),
		Seconds: seconds,
	}
	st := m.store
	return []tea.Cmd{func() tea.Msg {
		if err := st.AppendPlay(p); err != nil {
			slog.Warn("record play", "videoID", p.VideoID, "err", err)
		}
		return nil
	}}
}

// recordPlayNow writes the pending listen synchronously. It is used on quit,
// where a queued tea.Cmd would not get a chance to run.
func (m *Model) recordPlayNow() {
	id, seconds := m.recordingID, m.listenSeconds
	m.listenSeconds = 0
	if id == "" || seconds < minPlaySeconds || m.store == nil {
		return
	}
	p := stats.Play{
		VideoID: id,
		Title:   m.recordingTitle,
		Artist:  m.recordingArtist,
		At:      time.Now(),
		Seconds: seconds,
	}
	if err := m.store.AppendPlay(p); err != nil {
		slog.Warn("record play on quit", "videoID", id, "err", err)
	}
}

// statsView renders the Stats tab: totals, a current streak, and the top
// artists/tracks with proportional bars.
func (m Model) statsView(height int) string {
	accent := accentForTab(TabStats)
	title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("📊  Listening stats")
	sub := styleMuted.Render("local only · no account · nothing leaves this machine")

	if !m.statsLoaded {
		return title + "\n" + sub + "\n\n" + styleMuted.Render("  loading…")
	}
	s := m.statsSummary
	if s.TotalPlays == 0 {
		return title + "\n" + sub + "\n\n" +
			styleMuted.Render("  no plays recorded yet — listen to something and come back")
	}

	var b strings.Builder
	b.WriteString(title + "\n" + sub + "\n\n")
	b.WriteString(fmt.Sprintf("  %s   %s   %s   %s\n\n",
		statCard(accent, "plays", fmt.Sprintf("%d", s.TotalPlays)),
		statCard(accent, "minutes", fmt.Sprintf("%d", stats.Minutes(s.TotalSeconds))),
		statCard(accent, "days", fmt.Sprintf("%d", s.ListeningDays)),
		statCard(accent, "streak", fmt.Sprintf("%d 🔥", s.StreakDays))))

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colText).Render("  Top artists") + "\n")
	b.WriteString(leaderboard(s.TopArtists, 26) + "\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colText).Render("  Top tracks") + "\n")
	b.WriteString(leaderboard(s.TopTracks, 40) + "\n")

	if !s.FirstPlay.IsZero() {
		b.WriteString(styleMuted.Render(fmt.Sprintf("  since %s", s.FirstPlay.Format("2006-01-02"))) + "\n")
	}
	return b.String()
}

// statCard renders one labelled figure for the stats header row.
func statCard(accent lipgloss.AdaptiveColor, label, value string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(accent).Render(value) +
		" " + styleMuted.Render(label)
}

// leaderboard renders ranked counts as aligned rows with a proportional bar.
func leaderboard(counts []stats.Count, labelWidth int) string {
	if len(counts) == 0 {
		return styleMuted.Render("    —")
	}
	max := 1
	for _, c := range counts {
		if c.Count > max {
			max = c.Count
		}
	}
	var lines []string
	for _, c := range counts {
		label := clip(c.Name, labelWidth)
		pad := strings.Repeat(" ", maxInt(0, labelWidth-lipgloss.Width(label)))
		bar := lipgloss.NewStyle().Foreground(colBar).Render(strings.Repeat("█", barWidth(c.Count, max)))
		lines = append(lines, fmt.Sprintf("    %s%s  %s %s", label, pad, bar, styleMuted.Render(fmt.Sprintf("%d", c.Count))))
	}
	return strings.Join(lines, "\n")
}

// barWidth maps a count onto a 0..20 cell bar relative to max.
func barWidth(n, max int) int {
	if max <= 0 || n <= 0 {
		return 0
	}
	w := n * 20 / max
	if w < 1 {
		w = 1
	}
	return w
}

// --- Export / import ---

// exportSelection picks the tracks to export and starts the export.
func (m *Model) exportSelection() []tea.Cmd {
	tracks, name := m.exportTracks()
	if len(tracks) == 0 {
		return []tea.Cmd{m.showOSD("↥  nothing to export")}
	}
	return []tea.Cmd{
		m.showOSD("↥  exporting " + clip(name, 24) + "…"),
		m.exportCmd(name, tracks),
	}
}

// exportTracks chooses what "export" means for the current view.
func (m Model) exportTracks() ([]ytmusic.Track, string) {
	if m.queue != nil && m.queue.Len() > 0 {
		name := m.fromPlaylist
		if name == "" {
			name = "queue"
		}
		return m.queue.Tracks(), name
	}
	switch m.tab {
	case TabPlaylists:
		if m.inPlaylist && len(m.playlistTracks) > 0 {
			return m.playlistTracks, m.trackList.Title
		}
	case TabLibrary:
		if len(m.libraryTracks) > 0 {
			return m.libraryTracks, "Liked Songs"
		}
	case TabSearch:
		if len(m.searchResults) > 0 {
			return m.searchResults, "search"
		}
	}
	return nil, ""
}

func (m Model) exportCmd(name string, tracks []ytmusic.Track) tea.Cmd {
	return func() tea.Msg {
		m3u, csv, err := playlistio.WriteExport(name, tracks, time.Now())
		return exportDoneMsg{m3u: m3u, csv: csv, n: len(tracks), err: err}
	}
}

// importM3U reads the fixed import path into the queue.
func (m *Model) importM3U() []tea.Cmd {
	path := playlistio.DefaultImportPath()
	return []tea.Cmd{
		m.showOSD("↧  importing " + filepath.Base(path) + "…"),
		m.importCmd(path),
	}
}

func (m Model) importCmd(path string) tea.Cmd {
	return func() tea.Msg {
		entries, err := playlistio.ImportM3U(path)
		if err != nil {
			return importDoneMsg{err: err}
		}
		tracks, skipped := tracksFromEntries(entries)
		return importDoneMsg{tracks: tracks, skipped: skipped}
	}
}

// tracksFromEntries converts parsed M3U entries into playable tracks, dropping
// entries without a recognisable video ID.
func tracksFromEntries(entries []playlistio.Entry) (tracks []ytmusic.Track, skipped int) {
	for _, e := range entries {
		if e.VideoID == "" {
			skipped++
			continue
		}
		tracks = append(tracks, ytmusic.Track{
			VideoID: e.VideoID,
			Title:   e.Title,
			Artist:  e.Artist,
			URL:     e.URL,
		})
	}
	return tracks, skipped
}
