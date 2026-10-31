// Package pomodoro implements a small, non-blocking work/break focus timer.
//
// The timer holds no goroutines and does no I/O: it is advanced by the caller
// (the TUI's existing 500 ms tick) via Tick, which computes elapsed wall-clock
// time and reports phase completions. This keeps the UI thread free and makes
// the state machine trivially testable.
package pomodoro

import (
	"fmt"
	"time"
)

// Phase is the current timer phase.
type Phase int

const (
	// Idle means the timer is stopped.
	Idle Phase = iota
	// Work is a focus interval.
	Work
	// Break is a rest interval.
	Break
)

func (p Phase) String() string {
	switch p {
	case Work:
		return "work"
	case Break:
		return "break"
	default:
		return "idle"
	}
}

// Timer is a work/break countdown driven by Tick.
type Timer struct {
	work      time.Duration
	brk       time.Duration
	phase     Phase
	remaining time.Duration
	last      time.Time
}

// New returns an idle timer. Non-positive durations fall back to a 25/5 minute
// classic Pomodoro.
func New(work, brk time.Duration) Timer {
	if work <= 0 {
		work = 25 * time.Minute
	}
	if brk <= 0 {
		brk = 5 * time.Minute
	}
	return Timer{work: work, brk: brk}
}

// Running reports whether a phase is counting down.
func (t *Timer) Running() bool { return t.phase != Idle }

// Phase returns the current phase.
func (t *Timer) Phase() Phase { return t.phase }

// Remaining returns the time left in the current phase (never negative).
func (t *Timer) Remaining() time.Duration {
	if t.remaining < 0 {
		return 0
	}
	return t.remaining
}

// Work returns the configured work duration.
func (t *Timer) Work() time.Duration { return t.work }

// Break returns the configured break duration.
func (t *Timer) Break() time.Duration { return t.brk }

// Start begins a work phase. It is a no-op when already running.
func (t *Timer) Start(now time.Time) {
	if t.phase != Idle {
		return
	}
	t.phase = Work
	t.remaining = t.work
	t.last = now
}

// Stop halts the timer and returns it to idle.
func (t *Timer) Stop() {
	t.phase = Idle
	t.remaining = 0
	t.last = time.Time{}
}

// Toggle starts the timer when idle and stops it when running.
func (t *Timer) Toggle(now time.Time) {
	if t.phase == Idle {
		t.Start(now)
		return
	}
	t.Stop()
}

// Tick advances the timer by the wall-clock time since the previous tick. When
// a phase's countdown reaches zero it rolls over to the next phase (work →
// break → work → …) and reports the phase that just completed with done=true.
// When nothing completes it returns (Idle, false).
func (t *Timer) Tick(now time.Time) (completed Phase, done bool) {
	if t.phase == Idle {
		return Idle, false
	}
	if t.last.IsZero() {
		t.last = now
		return Idle, false
	}
	delta := now.Sub(t.last)
	if delta < 0 {
		delta = 0
	}
	t.last = now
	t.remaining -= delta
	if t.remaining > 0 {
		return Idle, false
	}
	completed = t.phase
	if t.phase == Work {
		t.phase = Break
		t.remaining = t.brk
	} else {
		t.phase = Work
		t.remaining = t.work
	}
	return completed, true
}

// Format renders a duration as MM:SS, or H:MM:SS once it exceeds an hour.
func Format(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
