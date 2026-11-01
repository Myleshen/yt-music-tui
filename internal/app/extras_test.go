package app

import (
	"strings"
	"testing"
	"time"

	"github.com/Myleshen/yt-music-tui/internal/pomodoro"
)

// TestPomodoroWorkCompleteNudgesToMove verifies that finishing a work phase
// announces the break with an actionable "stand up and move" message and shows
// the remaining break time.
func TestPomodoroWorkCompleteNudgesToMove(t *testing.T) {
	m := Model{pomodoro: pomodoro.New(25*time.Minute, 5*time.Minute)}
	start := time.Now()
	m.pomodoro.Start(start)
	completed, done := m.pomodoro.Tick(start.Add(25 * time.Minute))
	if !done || completed != pomodoro.Work {
		t.Fatalf("tick: done=%v completed=%v, want work completed", done, completed)
	}

	var got []string
	withNotifications(t, func(summary, body string) error {
		got = append(got, summary+"|"+body)
		return nil
	}, func() {
		cmds := m.pomodoroCompletedCmds(completed)
		if len(cmds) != 2 {
			t.Fatalf("cmds = %d, want OSD + notification", len(cmds))
		}
		// Execute only the notification cmd: the OSD cmd is a 1.2s hide tick.
		if cmd := cmds[1]; cmd != nil {
			_ = cmd()
		}
	})

	if len(got) != 1 {
		t.Fatalf("notifications = %v, want exactly 1", got)
	}
	if !strings.Contains(got[0], "stand up and move") {
		t.Errorf("notification %q should tell the user to move", got[0])
	}
	if !strings.Contains(got[0], "5:00") {
		t.Errorf("notification %q should include the break length", got[0])
	}
	if !strings.Contains(m.osdText, "break time") || !strings.Contains(m.osdText, "5:00") {
		t.Errorf("OSD = %q, want break time with the break length", m.osdText)
	}
}

// TestPomodoroBreakCompleteBackToWork verifies the break -> work announcement.
func TestPomodoroBreakCompleteBackToWork(t *testing.T) {
	m := Model{pomodoro: pomodoro.New(25*time.Minute, 5*time.Minute)}
	start := time.Now()
	m.pomodoro.Start(start)
	if _, done := m.pomodoro.Tick(start.Add(25 * time.Minute)); !done {
		t.Fatal("work phase should complete")
	}
	completed, done := m.pomodoro.Tick(start.Add(30 * time.Minute))
	if !done || completed != pomodoro.Break {
		t.Fatalf("tick: done=%v completed=%v, want break completed", done, completed)
	}

	var got []string
	withNotifications(t, func(summary, body string) error {
		got = append(got, summary+"|"+body)
		return nil
	}, func() {
		cmds := m.pomodoroCompletedCmds(completed)
		if len(cmds) != 2 {
			t.Fatalf("cmds = %d, want OSD + notification", len(cmds))
		}
		if cmd := cmds[1]; cmd != nil {
			_ = cmd()
		}
	})

	if len(got) != 1 {
		t.Fatalf("notifications = %v, want exactly 1", got)
	}
	if !strings.Contains(got[0], "back to work") || !strings.Contains(got[0], "25:00") {
		t.Errorf("notification %q should announce the work interval", got[0])
	}
}
