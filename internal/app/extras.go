package app

import (
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Myleshen/yt-music-tui/internal/pomodoro"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// --- Pomodoro timer ---------------------------------------------------------

// pomodoroOSD renders the current phase and remaining time for the OSD.
func (m Model) pomodoroOSD() string {
	icon, label := "🍅", "work"
	if m.pomodoro.Phase() == pomodoro.Break {
		icon, label = "☕", "break"
	}
	return fmt.Sprintf("%s  %s %s", icon, label, pomodoro.Format(m.pomodoro.Remaining()))
}

// pomodoroIndicator is the compact countdown shown in the player bar. It is
// empty while the timer is stopped.
func (m Model) pomodoroIndicator() string {
	if !m.pomodoro.Running() {
		return ""
	}
	icon := "🍅"
	if m.pomodoro.Phase() == pomodoro.Break {
		icon = "☕"
	}
	return icon + " " + pomodoro.Format(m.pomodoro.Remaining()) + "   "
}

// pomodoroCompletedCmds handles a phase rollover: a desktop notification plus
// an OSD announcing the next phase.
func (m *Model) pomodoroCompletedCmds(completed pomodoro.Phase) []tea.Cmd {
	switch completed {
	case pomodoro.Work:
		notifyDesktop("Pomodoro", "Work session complete — time for a break")
		return []tea.Cmd{m.showOSD("☕  break time — " + pomodoro.Format(m.pomodoro.Remaining()))}
	case pomodoro.Break:
		notifyDesktop("Pomodoro", "Break over — back to work")
		return []tea.Cmd{m.showOSD("🍅  work time — " + pomodoro.Format(m.pomodoro.Remaining()))}
	}
	return nil
}

// notifyDesktop sends a desktop notification via notify-send when it is
// installed. It never blocks: the command runs in its own goroutine and any
// failure is ignored (logged at debug).
func notifyDesktop(summary, body string) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}
	go func() {
		if err := exec.Command("notify-send", "-a", "ytm", summary, body).Run(); err != nil {
			slog.Debug("notify-send failed", "err", err)
		}
	}()
}

// trackItems converts tracks to list items for the bubbles list.
func (m Model) trackItems(tracks []ytmusic.Track) []list.Item {
	items := make([]list.Item, len(tracks))
	for i, t := range tracks {
		items[i] = trackItem{track: t}
	}
	return items
}
