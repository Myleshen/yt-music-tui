package player

import (
	"reflect"
	"testing"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// assertPermutation fails unless order is a permutation of 0..n-1.
func assertPermutation(t *testing.T, order []int, n int) {
	t.Helper()
	if len(order) != n {
		t.Fatalf("order has %d entries, want %d", len(order), n)
	}
	seen := make([]bool, n)
	for _, idx := range order {
		if idx < 0 || idx >= n || seen[idx] {
			t.Fatalf("order %v is not a permutation of 0..%d", order, n-1)
		}
		seen[idx] = true
	}
}

func TestQueue_ShufflePreservesCurrentAndRestores(t *testing.T) {
	q := NewQueue(makeTracks(8), 3)
	orig := q.CurrentIndex()

	q.SetShuffle(true)
	if !q.Shuffled() {
		t.Fatal("expected Shuffled=true after SetShuffle(true)")
	}
	ord := q.Order()
	assertPermutation(t, ord, 8)

	// The already-played prefix (including the current track) keeps its place.
	for i := 0; i <= orig; i++ {
		if ord[i] != i {
			t.Fatalf("shuffle changed the played prefix: order[%d]=%d, want %d", i, ord[i], i)
		}
	}
	// The current track is unchanged.
	if q.CurrentIndex() != orig {
		t.Fatalf("shuffle changed the current track: %d → %d", orig, q.CurrentIndex())
	}

	// Walking to the start and then to the end must reproduce the playback
	// order exactly, with no repeats and no skipped tracks.
	for q.HasPrev() {
		q.Prev()
	}
	if q.CurrentIndex() != ord[0] {
		t.Fatalf("walk to start ended at %d, want %d", q.CurrentIndex(), ord[0])
	}
	walk := []int{q.CurrentIndex()}
	for q.HasNext() {
		if _, ok := q.Next(); !ok {
			t.Fatal("Next reported false while HasNext was true")
		}
		walk = append(walk, q.CurrentIndex())
	}
	if !reflect.DeepEqual(walk, ord) {
		t.Fatalf("walk = %v, want playback order %v", walk, ord)
	}

	// Turning shuffle off restores the identity order without moving the
	// current track or losing the list.
	cur := q.CurrentIndex()
	q.SetShuffle(false)
	if q.Shuffled() {
		t.Fatal("expected Shuffled=false after SetShuffle(false)")
	}
	assertPermutation(t, q.Order(), 8)
	for i, idx := range q.Order() {
		if i != idx {
			t.Fatalf("order not restored to identity: order=%v", q.Order())
		}
	}
	if q.CurrentIndex() != cur {
		t.Fatalf("unshuffle moved the current track: %d → %d", cur, q.CurrentIndex())
	}
}

func TestQueue_ShuffleUpcomingOrder(t *testing.T) {
	q := NewQueue(makeTracks(6), 1)
	q.SetShuffle(true)
	ord := q.Order()

	idx := q.UpcomingIndexes(4)
	if len(idx) != 4 {
		t.Fatalf("UpcomingIndexes(4) = %v, want 4 entries", idx)
	}
	if !reflect.DeepEqual(idx, ord[2:6]) {
		t.Fatalf("UpcomingIndexes = %v, want order tail %v", idx, ord[2:6])
	}

	// Upcoming(n) must mirror the same tracks in the same order.
	tracks := q.Upcoming(4)
	for i, got := range tracks {
		want, _ := q.TrackAt(idx[i])
		if got.VideoID != want.VideoID {
			t.Errorf("Upcoming[%d] = %s, want %s", i, got.VideoID, want.VideoID)
		}
	}

	// UpcomingIndex(0) is the first upcoming in playback order.
	if got, ok := q.UpcomingIndex(0); !ok || got != ord[2] {
		t.Errorf("UpcomingIndex(0) = (%d,%v), want (%d,true)", got, ok, ord[2])
	}
	if _, ok := q.UpcomingIndex(999); ok {
		t.Error("UpcomingIndex past the end should report ok=false")
	}
	if q.UpcomingCount() != 4 {
		t.Errorf("UpcomingCount = %d, want 4", q.UpcomingCount())
	}
}

func TestQueue_RestoreOrder(t *testing.T) {
	q := NewQueue(makeTracks(4), 0)
	order := []int{3, 1, 0, 2}
	q.RestoreOrder(order, true)
	if !q.Shuffled() {
		t.Fatal("expected shuffle flag restored")
	}
	if !reflect.DeepEqual(q.Order(), order) {
		t.Fatalf("Order = %v, want restored %v", q.Order(), order)
	}

	// An invalid permutation is rejected in favour of a freshly derived order.
	q.RestoreOrder([]int{0, 0, 1}, true)
	assertPermutation(t, q.Order(), 4)

	// Restoring the identity order turns shuffle off.
	q2 := NewQueue(makeTracks(4), 2)
	q2.RestoreOrder([]int{0, 1, 2, 3}, false)
	if q2.Shuffled() {
		t.Fatal("expected Shuffled=false after restoring identity order")
	}
	if q2.CurrentIndex() != 2 {
		t.Fatalf("restore moved the current track: %d", q2.CurrentIndex())
	}
}

func TestQueue_Append(t *testing.T) {
	q := NewQueue(makeTracks(2), 0)
	cur := q.CurrentIndex()

	added := q.Append([]ytmusic.Track{
		{VideoID: "x1"},
		{VideoID: "x2"},
		{VideoID: "x3"},
	})
	if added != 3 {
		t.Fatalf("Append returned %d, want 3", added)
	}
	if q.Len() != 5 {
		t.Fatalf("Len = %d, want 5", q.Len())
	}
	if q.CurrentIndex() != cur {
		t.Fatalf("Append moved the current track: %d → %d", cur, q.CurrentIndex())
	}
	assertPermutation(t, q.Order(), 5)

	// The appended tracks are reachable in order.
	got := q.UpcomingIndexes(3)
	want := []int{1, 2, 3, 4}[0:3]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("upcoming after append = %v, want %v", got, want)
	}

	// Appending nothing is a no-op.
	if n := q.Append(nil); n != 0 || q.Len() != 5 {
		t.Fatalf("Append(nil) = %d, len = %d", n, q.Len())
	}
}
