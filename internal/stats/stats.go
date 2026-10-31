// Package stats aggregates local listening history.
//
// Plays are recorded locally (in bbolt) and summarised here; nothing is ever
// sent to a network service. The aggregation is pure — it takes a slice of
// plays and a reference time — so it is straightforward to unit test.
package stats

import (
	"sort"
	"time"
)

// Play is a single recorded listen. Seconds is how long the track was actually
// listened to, which may be less than its full duration.
type Play struct {
	VideoID string    `json:"videoId"`
	Title   string    `json:"title"`
	Artist  string    `json:"artist"`
	At      time.Time `json:"at"`
	Seconds float64   `json:"seconds"`
}

// Count is a label with an occurrence total.
type Count struct {
	Name  string
	Count int
}

// Summary is an aggregated view of the listening history.
type Summary struct {
	TotalPlays    int
	TotalSeconds  float64
	TopArtists    []Count
	TopTracks     []Count
	StreakDays    int
	ListeningDays int
	FirstPlay     time.Time
	LastPlay      time.Time
}

// topN is how many entries each leaderboard keeps.
const topN = 5

// Summarize aggregates plays into a Summary, using now to anchor the streak
// calculation. An empty history yields a zero Summary.
func Summarize(plays []Play, now time.Time) Summary {
	var s Summary
	if len(plays) == 0 {
		return s
	}
	s.TotalPlays = len(plays)
	artistCounts := map[string]int{}
	trackCounts := map[string]int{}
	for _, p := range plays {
		s.TotalSeconds += p.Seconds
		if p.Artist != "" {
			artistCounts[p.Artist]++
		}
		if label := DisplayName(p); label != "" {
			trackCounts[label]++
		}
		if s.FirstPlay.IsZero() || p.At.Before(s.FirstPlay) {
			s.FirstPlay = p.At
		}
		if p.At.After(s.LastPlay) {
			s.LastPlay = p.At
		}
	}
	s.TopArtists = topCounts(artistCounts, topN)
	s.TopTracks = topCounts(trackCounts, topN)
	s.ListeningDays = len(listeningDays(plays, now.Location()))
	s.StreakDays = Streak(plays, now)
	return s
}

// Streak returns the number of consecutive days (ending today, or yesterday
// when nothing has been played yet today) with at least one recorded play. A
// history with no recent activity has a streak of 0.
func Streak(plays []Play, now time.Time) int {
	days := listeningDays(plays, now.Location())
	if len(days) == 0 {
		return 0
	}
	cursor := startOfDay(now)
	if !days[cursor] {
		// Allow the streak to survive a day that has not been listened to yet.
		cursor = cursor.AddDate(0, 0, -1)
		if !days[cursor] {
			return 0
		}
	}
	streak := 0
	for days[cursor] {
		streak++
		cursor = cursor.AddDate(0, 0, -1)
	}
	return streak
}

// listeningDays returns the set of local calendar days on which a play occurred.
func listeningDays(plays []Play, loc *time.Location) map[time.Time]bool {
	days := map[time.Time]bool{}
	for _, p := range plays {
		if p.At.IsZero() {
			continue
		}
		days[startOfDay(p.At.In(loc))] = true
	}
	return days
}

// startOfDay truncates t to midnight in its own location.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// topCounts returns the n most frequent labels, ties broken alphabetically for
// deterministic output.
func topCounts(counts map[string]int, n int) []Count {
	out := make([]Count, 0, len(counts))
	for name, c := range counts {
		out = append(out, Count{Name: name, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Minutes returns the whole minutes represented by a number of seconds.
func Minutes(seconds float64) int {
	if seconds <= 0 {
		return 0
	}
	return int(seconds / 60)
}

// DisplayName returns a human label for a play, used by the track leaderboard.
func DisplayName(p Play) string {
	switch {
	case p.Title == "" && p.Artist == "":
		return p.VideoID
	case p.Artist == "":
		return p.Title
	case p.Title == "":
		return p.Artist
	default:
		return p.Title + " — " + p.Artist
	}
}
