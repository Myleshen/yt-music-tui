package pomodoro

import (
	"testing"
	"time"
)

func TestToggleStartsAndStops(t *testing.T) {
	now := time.Unix(1000, 0)
	tm := New(25*time.Minute, 5*time.Minute)
	if tm.Running() {
		t.Fatal("new timer should be idle")
	}
	tm.Toggle(now)
	if !tm.Running() || tm.Phase() != Work {
		t.Fatalf("after toggle: running=%v phase=%v, want running work", tm.Running(), tm.Phase())
	}
	if tm.Remaining() != 25*time.Minute {
		t.Errorf("remaining = %v, want 25m", tm.Remaining())
	}
	tm.Toggle(now)
	if tm.Running() || tm.Phase() != Idle {
		t.Fatalf("after second toggle: running=%v phase=%v, want idle", tm.Running(), tm.Phase())
	}
	if tm.Remaining() != 0 {
		t.Errorf("remaining after stop = %v, want 0", tm.Remaining())
	}
}

func TestTickCountsDown(t *testing.T) {
	base := time.Unix(2000, 0)
	tm := New(25*time.Minute, 5*time.Minute)
	tm.Start(base)

	if _, done := tm.Tick(base.Add(10 * time.Minute)); done {
		t.Fatal("phase completed early")
	}
	if got, want := tm.Remaining(), 15*time.Minute; got != want {
		t.Errorf("remaining = %v, want %v", got, want)
	}
	if tm.Phase() != Work {
		t.Errorf("phase = %v, want work", tm.Phase())
	}
}

func TestWorkBreakCycle(t *testing.T) {
	base := time.Unix(3000, 0)
	tm := New(25*time.Minute, 5*time.Minute)
	tm.Start(base)

	completed, done := tm.Tick(base.Add(25 * time.Minute))
	if !done || completed != Work {
		t.Fatalf("want work completed, got done=%v completed=%v", done, completed)
	}
	if tm.Phase() != Break || tm.Remaining() != 5*time.Minute {
		t.Fatalf("after work: phase=%v remaining=%v, want break 5m", tm.Phase(), tm.Remaining())
	}

	completed, done = tm.Tick(base.Add(30 * time.Minute))
	if !done || completed != Break {
		t.Fatalf("want break completed, got done=%v completed=%v", done, completed)
	}
	if tm.Phase() != Work || tm.Remaining() != 25*time.Minute {
		t.Fatalf("after break: phase=%v remaining=%v, want work 25m", tm.Phase(), tm.Remaining())
	}
}

func TestTickIdleIsNoop(t *testing.T) {
	tm := New(25*time.Minute, 5*time.Minute)
	if completed, done := tm.Tick(time.Unix(4000, 0)); done || completed != Idle {
		t.Fatalf("idle tick returned %v/%v, want Idle/false", completed, done)
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "00:00"},
		{-5 * time.Second, "00:00"},
		{5 * time.Second, "00:05"},
		{65 * time.Second, "01:05"},
		{25 * time.Minute, "25:00"},
		{90 * time.Minute, "1:30:00"},
	}
	for _, c := range cases {
		if got := Format(c.in); got != c.want {
			t.Errorf("Format(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
