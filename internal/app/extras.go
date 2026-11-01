package app

import (
	"fmt"
	"log/slog"

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

// pomodoroCompletedCmds handles a phase rollover. When a work phase ends the
// music is paused so the break is quiet, then an OSD and a desktop notification
// nudge the user to stand up and move.
func (m *Model) pomodoroCompletedCmds(completed pomodoro.Phase) []tea.Cmd {
	switch completed {
	case pomodoro.Work:
		if m.mpv != nil {
			slog.Info("pomodoro: work complete — pausing playback")
			if err := m.mpv.Pause(); err != nil {
				slog.Debug("pomodoro: pause failed", "err", err)
			}
			// Reflect the pause immediately so the player bar, MPRIS, and
			// Discord do not show a stale "playing" state until the next tick.
			m.playerState.Playing = false
		}
		brk := pomodoro.Format(m.pomodoro.Remaining())
		return []tea.Cmd{
			m.showOSD("☕  break time — stand up & move  ·  " + brk),
			m.notifyTextCmd(
				"☕  Break time — stand up and move",
				fmt.Sprintf("Nice work! Step away from the screen, stretch, and walk around for %s.", brk),
			),
		}
	case pomodoro.Break:
		work := pomodoro.Format(m.pomodoro.Remaining())
		return []tea.Cmd{
			m.showOSD("🍅  work time — " + work),
			m.notifyTextCmd(
				"🍅  Break over — back to work",
				fmt.Sprintf("Break's done. Settle in for %s of focused work.", work),
			),
		}
	}
	return nil
}

// trackItems converts tracks to list items for the bubbles list.
func (m Model) trackItems(tracks []ytmusic.Track) []list.Item {
	items := make([]list.Item, len(tracks))
	for i, t := range tracks {
		items[i] = trackItem{track: t}
	}
	return items
}
