package app

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Myleshen/yt-music-tui/internal/player"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// withNotifications swaps the package notifier for the duration of fn.
func withNotifications(t *testing.T, fn func(summary, body string) error, run func()) {
	t.Helper()
	orig := sendNotification
	sendNotification = fn
	t.Cleanup(func() { sendNotification = orig })
	run()
}

func TestCheckSleep_ExpiredStopsAndNotifies(t *testing.T) {
	var got []string
	m := Model{}
	// Arm in the past so the deadline has already passed.
	m.sleep.Set(15*time.Minute, time.Now().Add(-16*time.Minute))

	withNotifications(t, func(summary, body string) error {
		got = append(got, summary+"|"+body)
		return nil
	}, func() {
		cmds := m.checkSleep()
		if m.sleep.Active() {
			t.Error("an expired timer must be cleared")
		}
		if len(cmds) == 0 {
			t.Fatal("checkSleep should return OSD + notification commands")
		}
		for _, c := range cmds {
			if c != nil {
				_ = c()
			}
		}
	})
	if len(got) != 1 {
		t.Fatalf("notifications = %v, want exactly 1", got)
	}
}

func TestCheckSleep_NotYetExpired(t *testing.T) {
	m := Model{}
	m.sleep.Set(30*time.Minute, time.Now())
	if cmds := m.checkSleep(); len(cmds) != 0 {
		t.Errorf("checkSleep before the deadline returned %d cmds, want 0", len(cmds))
	}
	if !m.sleep.Active() {
		t.Error("timer must remain armed before its deadline")
	}
	// An inactive timer never fires.
	m.sleep.Cancel()
	if cmds := m.checkSleep(); len(cmds) != 0 {
		t.Errorf("inactive timer fired: %d cmds", len(cmds))
	}
}

func TestSleepMenu_ArmsAndCancels(t *testing.T) {
	m := Model{sleepDefault: 45 * time.Minute}

	armed, _ := m.updateSleepMode(keyMsg("1"))
	if got := armed.(Model).sleep.Total(); got != 15*time.Minute {
		t.Errorf("choice 1 armed %v, want 15m", got)
	}
	if armed.(Model).sleepMode {
		t.Error("menu should close after a choice")
	}

	// The "d" choice uses the configured default.
	def, _ := m.updateSleepMode(keyMsg("d"))
	if got := def.(Model).sleep.Total(); got != 45*time.Minute {
		t.Errorf("default choice armed %v, want 45m", got)
	}

	// 0 turns an armed timer off.
	m.sleep.Set(10*time.Minute, time.Now())
	off, _ := m.updateSleepMode(keyMsg("0"))
	if off.(Model).sleep.Active() {
		t.Error("choice 0 should cancel the timer")
	}

	// esc just closes the menu without arming.
	esc, _ := m.updateSleepMode(keyMsg("esc"))
	if esc.(Model).sleepMode {
		t.Error("esc should close the menu")
	}
}

func TestSleepIndicator(t *testing.T) {
	m := Model{}
	if m.sleepIndicator() != "" {
		t.Error("no indicator when the timer is inactive")
	}
	m.sleep.Set(30*time.Minute, time.Now())
	if ind := m.sleepIndicator(); !strings.Contains(ind, "😴") {
		t.Errorf("indicator = %q, want to contain the sleep icon", ind)
	}
}

func TestOnTrackChanged_Notifies(t *testing.T) {
	var got []string
	m := Model{
		width:             0, // no art fetch (keeps the command list network-free)
		mpv:               &player.MPV{},
		queue:             player.NewQueue([]ytmusic.Track{{VideoID: "v1", Title: "Song", Artist: "Band"}}, 0),
		notifyTrackChange: true,
	}

	withNotifications(t, func(summary, body string) error {
		got = append(got, summary+"|"+body)
		return nil
	}, func() {
		for _, c := range m.onTrackChanged() {
			if c != nil {
				_ = c()
			}
		}
	})

	if len(got) != 1 {
		t.Fatalf("notifications = %v, want exactly 1", got)
	}
	if !strings.Contains(got[0], "Song") || !strings.Contains(got[0], "Band") {
		t.Errorf("notification = %q, want title and artist", got[0])
	}
}

func TestOnTrackChanged_NotifyDisabled(t *testing.T) {
	var called bool
	m := Model{
		mpv:   &player.MPV{},
		queue: player.NewQueue([]ytmusic.Track{{VideoID: "v1", Title: "Song"}}, 0),
	}
	withNotifications(t, func(summary, body string) error {
		called = true
		return nil
	}, func() {
		for _, c := range m.onTrackChanged() {
			if c != nil {
				_ = c()
			}
		}
	})
	if called {
		t.Error("notifications must not fire when notifyTrackChange is false")
	}
}

// TestNarrowLayout_NoOverflow asserts that nothing overflows the terminal width
// below 100 columns, with and without the lyrics panel.
func TestNarrowLayout_NoOverflow(t *testing.T) {
	for _, w := range []int{40, 60, 80, 99, 100} {
		for _, lyrics := range []bool{false, true} {
			m := newNowPlayingModel(w, lyrics)
			views := map[string]string{
				"nowPlaying": m.nowPlayingView(40),
				"playerBar":  m.playerBarView(),
				"header":     m.headerView(),
			}
			for name, out := range views {
				for _, line := range strings.Split(out, "\n") {
					if lw := lipgloss.Width(line); lw > w {
						t.Errorf("width=%d lyrics=%v %s: line %d cols > %d: %q",
							w, lyrics, name, lw, w, line)
					}
				}
			}
		}
	}
}

func TestHeader_CompactOnNarrow(t *testing.T) {
	m := newNowPlayingModel(80, false)
	out := m.headerView()
	if strings.Contains(out, "Now Playing") {
		t.Errorf("narrow header should use short tab labels:\n%s", out)
	}
	wide := newNowPlayingModel(120, false)
	if !strings.Contains(wide.headerView(), "Now Playing") {
		t.Error("wide header should show full tab labels")
	}
}
