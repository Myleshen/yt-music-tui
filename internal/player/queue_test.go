package player

import (
	"testing"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

func makeTracks(n int) []ytmusic.Track {
	tracks := make([]ytmusic.Track, n)
	for i := range tracks {
		tracks[i] = ytmusic.Track{
			VideoID: "vid" + string(rune('A'+i)),
			Title:   "Track " + string(rune('A'+i)),
			Artist:  "Artist",
		}
	}
	return tracks
}

func TestNewQueue_clampsIndex(t *testing.T) {
	tracks := makeTracks(3)

	q := NewQueue(tracks, -1)
	if q.CurrentIndex() != 0 {
		t.Errorf("expected index 0 for -1, got %d", q.CurrentIndex())
	}

	q = NewQueue(tracks, 99)
	if q.CurrentIndex() != 2 {
		t.Errorf("expected index 2 for out-of-range, got %d", q.CurrentIndex())
	}
}

func TestQueue_Current(t *testing.T) {
	tracks := makeTracks(3)
	q := NewQueue(tracks, 1)

	tr, ok := q.Current()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tr.VideoID != "vidB" {
		t.Errorf("expected vidB, got %s", tr.VideoID)
	}
}

func TestQueue_Current_empty(t *testing.T) {
	q := NewQueue(nil, 0)
	_, ok := q.Current()
	if ok {
		t.Error("expected ok=false for empty queue")
	}
}

func TestQueue_Next(t *testing.T) {
	tracks := makeTracks(3)
	q := NewQueue(tracks, 0)

	tr, ok := q.Next()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tr.VideoID != "vidB" {
		t.Errorf("expected vidB, got %s", tr.VideoID)
	}
	if q.CurrentIndex() != 1 {
		t.Errorf("expected index 1, got %d", q.CurrentIndex())
	}
}

func TestQueue_Next_atEnd(t *testing.T) {
	tracks := makeTracks(3)
	q := NewQueue(tracks, 2)

	_, ok := q.Next()
	if ok {
		t.Error("expected ok=false at end of queue")
	}
	if q.CurrentIndex() != 2 {
		t.Error("index should not change when Next() fails")
	}
}

func TestQueue_Prev(t *testing.T) {
	tracks := makeTracks(3)
	q := NewQueue(tracks, 2)

	tr, ok := q.Prev()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tr.VideoID != "vidB" {
		t.Errorf("expected vidB, got %s", tr.VideoID)
	}
}

func TestQueue_Prev_atStart(t *testing.T) {
	tracks := makeTracks(3)
	q := NewQueue(tracks, 0)

	_, ok := q.Prev()
	if ok {
		t.Error("expected ok=false at start of queue")
	}
}

func TestQueue_HasNextHasPrev(t *testing.T) {
	tracks := makeTracks(3)

	q := NewQueue(tracks, 0)
	if !q.HasNext() {
		t.Error("expected HasNext=true at index 0")
	}
	if q.HasPrev() {
		t.Error("expected HasPrev=false at index 0")
	}

	q = NewQueue(tracks, 2)
	if q.HasNext() {
		t.Error("expected HasNext=false at last index")
	}
	if !q.HasPrev() {
		t.Error("expected HasPrev=true at last index")
	}
}

func TestQueue_PeekNext(t *testing.T) {
	tracks := makeTracks(3)
	q := NewQueue(tracks, 0)

	tr, ok := q.PeekNext()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tr.VideoID != "vidB" {
		t.Errorf("expected vidB, got %s", tr.VideoID)
	}
	// Index must not change
	if q.CurrentIndex() != 0 {
		t.Error("PeekNext must not advance the index")
	}
}

func TestQueue_Tracks_returnsCopy(t *testing.T) {
	tracks := makeTracks(3)
	q := NewQueue(tracks, 0)

	copy1 := q.Tracks()
	copy1[0].Title = "mutated"

	copy2 := q.Tracks()
	if copy2[0].Title == "mutated" {
		t.Error("Tracks() must return a copy, not a reference to the internal slice")
	}
}

func TestQueue_Len(t *testing.T) {
	q := NewQueue(makeTracks(5), 0)
	if q.Len() != 5 {
		t.Errorf("expected Len=5, got %d", q.Len())
	}
}

func TestQueue_Neighbors_middle(t *testing.T) {
	q := NewQueue(makeTracks(6), 3) // vidD, neighbors: C | E,F

	got := q.Neighbors(2, 1)
	if len(got) != 3 {
		t.Fatalf("expected 3 neighbors, got %d", len(got))
	}
	want := []string{"vidC", "vidE", "vidF"}
	for i, w := range want {
		if got[i].VideoID != w {
			t.Errorf("neighbor %d: got %s want %s", i, got[i].VideoID, w)
		}
	}
	// Must not mutate the current index.
	if q.CurrentIndex() != 3 {
		t.Errorf("Neighbors must not change the index, got %d", q.CurrentIndex())
	}
}

func TestQueue_Neighbors_start(t *testing.T) {
	q := NewQueue(makeTracks(4), 0) // no previous

	got := q.Neighbors(2, 1)
	if len(got) != 2 {
		t.Fatalf("expected 2 neighbors at start, got %d", len(got))
	}
	if got[0].VideoID != "vidB" || got[1].VideoID != "vidC" {
		t.Errorf("unexpected neighbors: %v", got)
	}
}

func TestQueue_Neighbors_end(t *testing.T) {
	q := NewQueue(makeTracks(3), 2) // last track, only previous

	got := q.Neighbors(2, 1)
	if len(got) != 1 {
		t.Fatalf("expected 1 neighbor at end, got %d", len(got))
	}
	if got[0].VideoID != "vidB" {
		t.Errorf("unexpected neighbor: %v", got)
	}
}

func TestQueue_Neighbors_empty(t *testing.T) {
	q := NewQueue(nil, 0)
	if got := q.Neighbors(2, 1); len(got) != 0 {
		t.Errorf("expected no neighbors for empty queue, got %d", len(got))
	}
}

func TestQueue_Upcoming(t *testing.T) {
	q := NewQueue(makeTracks(6), 2) // current vidC, upcoming D,E,F

	got := q.Upcoming(2)
	if len(got) != 2 {
		t.Fatalf("expected 2 upcoming, got %d", len(got))
	}
	if got[0].VideoID != "vidD" || got[1].VideoID != "vidE" {
		t.Errorf("unexpected upcoming: %v", got)
	}
	// Must not mutate the index.
	if q.CurrentIndex() != 2 {
		t.Errorf("Upcoming must not change the index, got %d", q.CurrentIndex())
	}

	// Asking for more than remain returns only what exists.
	if got := q.Upcoming(99); len(got) != 3 {
		t.Errorf("expected 3 remaining, got %d", len(got))
	}

	// At the end there is nothing upcoming.
	q = NewQueue(makeTracks(3), 2)
	if got := q.Upcoming(5); len(got) != 0 {
		t.Errorf("expected no upcoming at end, got %d", len(got))
	}

	// n <= 0 and empty queue yield nothing.
	if got := q.Upcoming(0); got != nil {
		t.Errorf("Upcoming(0) = %v, want nil", got)
	}
	if got := NewQueue(nil, 0).Upcoming(3); got != nil {
		t.Errorf("empty queue Upcoming = %v, want nil", got)
	}
}

func TestQueue_SetCurrent(t *testing.T) {
	q := NewQueue(makeTracks(5), 0)

	tr, ok := q.SetCurrent(3)
	if !ok || tr.VideoID != "vidD" {
		t.Fatalf("SetCurrent(3) = (%v,%v), want vidD", tr.VideoID, ok)
	}
	if q.CurrentIndex() != 3 {
		t.Errorf("expected index 3, got %d", q.CurrentIndex())
	}

	// Out-of-range indices clamp rather than fail.
	tr, ok = q.SetCurrent(99)
	if !ok || tr.VideoID != "vidE" {
		t.Errorf("SetCurrent(99) = (%v,%v), want vidE", tr.VideoID, ok)
	}
	tr, ok = q.SetCurrent(-4)
	if !ok || tr.VideoID != "vidA" {
		t.Errorf("SetCurrent(-4) = (%v,%v), want vidA", tr.VideoID, ok)
	}

	// Empty queue cannot select.
	if _, ok := NewQueue(nil, 0).SetCurrent(0); ok {
		t.Error("SetCurrent on empty queue should report ok=false")
	}
}
