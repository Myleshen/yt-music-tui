package app

import "time"

// SleepTimer tracks an optional "stop playback after N minutes" deadline. All
// methods are cheap and non-blocking, so the model can drive it from the
// existing 500ms tick without any extra goroutine or timer.
//
// The zero value is an inactive timer.
type SleepTimer struct {
	active   bool
	deadline time.Time
	total    time.Duration
}

// Set arms the timer to expire d from now. A non-positive duration cancels it.
func (s *SleepTimer) Set(d time.Duration, now time.Time) {
	if d <= 0 {
		s.Cancel()
		return
	}
	s.active = true
	s.deadline = now.Add(d)
	s.total = d
}

// Cancel disarms the timer.
func (s *SleepTimer) Cancel() {
	s.active = false
	s.deadline = time.Time{}
	s.total = 0
}

// Active reports whether a deadline is armed.
func (s SleepTimer) Active() bool { return s.active }

// Total returns the duration the timer was armed for (0 when inactive).
func (s SleepTimer) Total() time.Duration { return s.total }

// Remaining returns the time left before the deadline, clamped to zero. An
// inactive timer always reports zero.
func (s SleepTimer) Remaining(now time.Time) time.Duration {
	if !s.active {
		return 0
	}
	d := s.deadline.Sub(now)
	if d < 0 {
		return 0
	}
	return d
}

// Expired reports whether an armed timer has reached (or passed) its deadline.
func (s SleepTimer) Expired(now time.Time) bool {
	return s.active && !now.Before(s.deadline)
}
