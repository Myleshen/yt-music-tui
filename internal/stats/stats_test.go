package stats

import (
	"testing"
	"time"
)

func at(y int, mo time.Month, d, h int) time.Time {
	return time.Date(y, mo, d, h, 0, 0, 0, time.UTC)
}

func TestSummarize_empty(t *testing.T) {
	s := Summarize(nil, at(2026, time.October, 10, 12))
	if s.TotalPlays != 0 || s.TotalSeconds != 0 || s.StreakDays != 0 || s.ListeningDays != 0 {
		t.Fatalf("empty history should be zero: %+v", s)
	}
	if len(s.TopArtists) != 0 || len(s.TopTracks) != 0 {
		t.Fatalf("empty history should have no leaderboards: %+v", s)
	}
}

func TestSummarize_totalsAndTops(t *testing.T) {
	plays := []Play{
		{VideoID: "a", Title: "One", Artist: "A", At: at(2026, time.October, 10, 9), Seconds: 120},
		{VideoID: "b", Title: "Two", Artist: "A", At: at(2026, time.October, 10, 10), Seconds: 180},
		{VideoID: "c", Title: "Three", Artist: "B", At: at(2026, time.October, 10, 11), Seconds: 60},
		{VideoID: "a", Title: "One", Artist: "A", At: at(2026, time.October, 10, 12), Seconds: 30},
	}
	s := Summarize(plays, at(2026, time.October, 10, 13))
	if s.TotalPlays != 4 {
		t.Errorf("TotalPlays = %d, want 4", s.TotalPlays)
	}
	if s.TotalSeconds != 390 {
		t.Errorf("TotalSeconds = %v, want 390", s.TotalSeconds)
	}
	if Minutes(s.TotalSeconds) != 6 {
		t.Errorf("Minutes = %d, want 6", Minutes(s.TotalSeconds))
	}
	if len(s.TopArtists) != 2 || s.TopArtists[0].Name != "A" || s.TopArtists[0].Count != 3 {
		t.Errorf("TopArtists = %+v, want A=3 first", s.TopArtists)
	}
	if len(s.TopTracks) == 0 || s.TopTracks[0].Name != "One — A" || s.TopTracks[0].Count != 2 {
		t.Errorf("TopTracks = %+v, want 'One — A'=2 first", s.TopTracks)
	}
	if !s.FirstPlay.Equal(plays[0].At) || !s.LastPlay.Equal(plays[3].At) {
		t.Errorf("First/Last = %v/%v", s.FirstPlay, s.LastPlay)
	}
}

func TestStreak(t *testing.T) {
	today := at(2026, time.October, 10, 12)
	tests := []struct {
		name  string
		plays []Play
		want  int
	}{
		{"none", nil, 0},
		{"today only", []Play{{At: today}}, 1},
		{"three consecutive ending today", []Play{
			{At: at(2026, time.October, 8, 9)},
			{At: at(2026, time.October, 9, 9)},
			{At: today},
		}, 3},
		{"gap breaks streak", []Play{
			{At: at(2026, time.October, 5, 9)},
			{At: at(2026, time.October, 9, 9)},
			{At: today},
		}, 2},
		{"yesterday counts when nothing today", []Play{
			{At: at(2026, time.October, 8, 9)},
			{At: at(2026, time.October, 9, 9)},
		}, 2},
		{"stale history has no streak", []Play{
			{At: at(2026, time.October, 1, 9)},
			{At: at(2026, time.October, 2, 9)},
		}, 0},
		{"multiple plays same day count once", []Play{
			{At: at(2026, time.October, 10, 8)},
			{At: at(2026, time.October, 10, 20)},
		}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Streak(tc.plays, today); got != tc.want {
				t.Errorf("Streak = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSummarize_listeningDays(t *testing.T) {
	plays := []Play{
		{At: at(2026, time.October, 8, 9)},
		{At: at(2026, time.October, 8, 22)},
		{At: at(2026, time.October, 10, 9)},
	}
	s := Summarize(plays, at(2026, time.October, 10, 12))
	if s.ListeningDays != 2 {
		t.Errorf("ListeningDays = %d, want 2", s.ListeningDays)
	}
}

func TestTopCounts_tieBreakAlphabetical(t *testing.T) {
	got := topCounts(map[string]int{"b": 1, "a": 1, "c": 2}, 2)
	if len(got) != 2 || got[0].Name != "c" || got[1].Name != "a" {
		t.Errorf("topCounts = %+v, want c then a", got)
	}
}
