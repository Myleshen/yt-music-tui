package app

import (
	"testing"
	"time"
)

func TestSleepTimer_CountdownAndExpiry(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	var s SleepTimer

	if s.Active() || s.Expired(start) {
		t.Fatal("zero SleepTimer must be inactive")
	}
	if got := s.Remaining(start); got != 0 {
		t.Errorf("inactive Remaining = %v, want 0", got)
	}

	s.Set(30*time.Minute, start)
	if !s.Active() {
		t.Fatal("timer should be active after Set")
	}
	if got := s.Total(); got != 30*time.Minute {
		t.Errorf("Total = %v, want 30m", got)
	}

	// Halfway through: 15 minutes remain, not expired.
	mid := start.Add(15 * time.Minute)
	if got := s.Remaining(mid); got != 15*time.Minute {
		t.Errorf("Remaining(half) = %v, want 15m", got)
	}
	if s.Expired(mid) {
		t.Error("timer must not be expired at the halfway point")
	}

	// One tick shy of the deadline: still active, 500ms left.
	near := start.Add(30*time.Minute - 500*time.Millisecond)
	if got := s.Remaining(near); got != 500*time.Millisecond {
		t.Errorf("Remaining(near) = %v, want 500ms", got)
	}
	if s.Expired(near) {
		t.Error("timer must not be expired before its deadline")
	}

	// At the deadline it expires, and Remaining clamps to zero.
	due := start.Add(30 * time.Minute)
	if !s.Expired(due) {
		t.Error("timer must be expired at its deadline")
	}
	if got := s.Remaining(due); got != 0 {
		t.Errorf("Remaining(due) = %v, want 0", got)
	}
	// Past the deadline stays expired and clamped.
	if !s.Expired(due.Add(time.Hour)) {
		t.Error("timer must stay expired after its deadline")
	}
	if got := s.Remaining(due.Add(time.Hour)); got != 0 {
		t.Errorf("Remaining(past) = %v, want 0", got)
	}
}

func TestSleepTimer_Cancel(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	var s SleepTimer
	s.Set(15*time.Minute, start)
	s.Cancel()
	if s.Active() {
		t.Error("timer should be inactive after Cancel")
	}
	if s.Expired(start.Add(time.Hour)) {
		t.Error("cancelled timer must never expire")
	}
	// A non-positive Set is equivalent to cancel.
	s.Set(0, start)
	if s.Active() {
		t.Error("Set(0) must cancel/leave the timer inactive")
	}
	s.Set(-time.Minute, start)
	if s.Active() {
		t.Error("Set(negative) must leave the timer inactive")
	}
}
