package player

import (
	"reflect"
	"testing"
)

// upcomingIDs returns the videoIDs of the tracks after the current one, in
// playback order.
func upcomingIDs(q *Queue, n int) []string {
	idx := q.UpcomingIndexes(n)
	out := make([]string, 0, len(idx))
	for _, i := range idx {
		tr, _ := q.TrackAt(i)
		out = append(out, tr.VideoID)
	}
	return out
}

func TestQueue_MoveDown_UnshuffledFlattensTracks(t *testing.T) {
	q := NewQueue(makeTracks(4), 0) // A B C D, current A

	if !q.MoveDown(1) { // move B after C
		t.Fatal("MoveDown(1) should succeed")
	}
	// Unshuffled: the track slice itself is reordered so persistence keeps it.
	if got := []string{q.Tracks()[1].VideoID, q.Tracks()[2].VideoID}; !reflect.DeepEqual(got, []string{"vidC", "vidB"}) {
		t.Errorf("tracks = %v, want C then B", got)
	}
	if !reflect.DeepEqual(upcomingIDs(q, 4), []string{"vidC", "vidB", "vidD"}) {
		t.Errorf("upcoming = %v, want [C B D]", upcomingIDs(q, 4))
	}
	if q.CurrentIndex() != 0 {
		t.Errorf("current moved to %d, want 0", q.CurrentIndex())
	}
	// Order stays the identity when shuffle is off.
	for i, idx := range q.Order() {
		if i != idx {
			t.Fatalf("order not identity: %v", q.Order())
		}
	}
}

func TestQueue_MoveUp_UnshuffledFlattensTracks(t *testing.T) {
	q := NewQueue(makeTracks(4), 0) // A B C D

	if !q.MoveUp(3) { // move D before C
		t.Fatal("MoveUp(3) should succeed")
	}
	if got := []string{q.Tracks()[1].VideoID, q.Tracks()[2].VideoID, q.Tracks()[3].VideoID}; !reflect.DeepEqual(got, []string{"vidB", "vidD", "vidC"}) {
		t.Errorf("tracks = %v, want B D C", got)
	}
}

func TestQueue_MoveUpDown_ShuffleKeepsTrackSlice(t *testing.T) {
	q := NewQueue(makeTracks(5), 1) // current B
	q.SetShuffle(true)
	before := q.Tracks()

	// Move the last track in playback order up one (deterministic regardless
	// of how the shuffle landed).
	ord := q.Order()
	last := ord[len(ord)-1]
	if !q.MoveUp(last) {
		t.Fatalf("MoveUp(last=%d) should succeed", last)
	}
	if !reflect.DeepEqual(q.Tracks(), before) {
		t.Error("shuffled reorder must not rewrite the track slice")
	}
	assertPermutation(t, q.Order(), 5)
	if q.CurrentIndex() != 1 {
		t.Errorf("current changed to %d, want 1", q.CurrentIndex())
	}
	// The played prefix (up to and including current) is untouched.
	ord = q.Order()
	for i := 0; i <= 1; i++ {
		if ord[i] != i {
			t.Fatalf("played prefix changed: order=%v", ord)
		}
	}
}

func TestQueue_MoveUp_RefusesCurrentAndFirstUpcoming(t *testing.T) {
	q := NewQueue(makeTracks(4), 1) // current B at order pos 1
	if q.MoveUp(1) {
		t.Error("MoveUp(current) must be refused")
	}
	if q.MoveUp(2) {
		t.Error("MoveUp(first upcoming) must be refused (would not move)")
	}
	if !q.MoveUp(3) { // B C D -> B D C
		t.Error("MoveUp(3) should move the last upcoming track up")
	}
	if got := upcomingIDs(q, 4); !reflect.DeepEqual(got, []string{"vidD", "vidC"}) {
		t.Errorf("upcoming = %v, want [D C]", got)
	}
}

func TestQueue_MoveDown_RefusesCurrentAndLast(t *testing.T) {
	q := NewQueue(makeTracks(4), 1)
	if q.MoveDown(0) {
		t.Error("MoveDown of a played track must be refused")
	}
	if q.MoveDown(1) {
		t.Error("MoveDown(current) must be refused")
	}
	if q.MoveDown(3) {
		t.Error("MoveDown(last) must be refused")
	}
	if !q.MoveDown(2) { // B C D -> B D C
		t.Error("MoveDown(2) should succeed")
	}
}

func TestQueue_MoveUp_RefusesFirstUpcoming(t *testing.T) {
	q := NewQueue(makeTracks(4), 0) // A B C D
	if q.MoveUp(1) {
		t.Error("moving the first upcoming track up must be refused")
	}
	if !q.MoveUp(2) { // A C B D
		t.Error("MoveUp(2) should succeed")
	}
}

func TestQueue_Remove_Upcoming(t *testing.T) {
	q := NewQueue(makeTracks(4), 0) // A B C D
	if !q.Remove(2) {               // remove C
		t.Fatal("Remove(2) should succeed")
	}
	if q.Len() != 3 {
		t.Fatalf("Len = %d, want 3", q.Len())
	}
	if !reflect.DeepEqual(upcomingIDs(q, 4), []string{"vidB", "vidD"}) {
		t.Errorf("upcoming = %v, want [B D]", upcomingIDs(q, 4))
	}
	if q.CurrentIndex() != 0 {
		t.Errorf("current = %d, want 0", q.CurrentIndex())
	}
	assertPermutation(t, q.Order(), 3)
}

func TestQueue_Remove_BeforeCurrentShiftsIndex(t *testing.T) {
	q := NewQueue(makeTracks(4), 2) // current C
	if !q.Remove(0) {               // remove A
		t.Fatal("Remove(0) should succeed")
	}
	if q.CurrentIndex() != 1 {
		t.Fatalf("current = %d, want 1 (shifted)", q.CurrentIndex())
	}
	if tr, ok := q.Current(); !ok || tr.VideoID != "vidC" {
		t.Errorf("current track = %v, want vidC", tr.VideoID)
	}
	if q.Len() != 3 {
		t.Errorf("Len = %d, want 3", q.Len())
	}
}

func TestQueue_Remove_CurrentRefused(t *testing.T) {
	q := NewQueue(makeTracks(3), 1)
	if q.Remove(1) {
		t.Error("removing the current track must be refused")
	}
	if q.Remove(9) || q.Remove(-1) {
		t.Error("out-of-range Remove must be refused")
	}
	if q.Len() != 3 {
		t.Errorf("Len = %d, want 3", q.Len())
	}
}

func TestQueue_ClearUpcoming(t *testing.T) {
	q := NewQueue(makeTracks(5), 2) // A B C D E, current C
	q.Next()                        // advance to D so C is "played"
	// Now current D (index 3); played prefix A,B,C,D; upcoming E.
	if n := q.ClearUpcoming(); n != 1 {
		t.Fatalf("ClearUpcoming removed %d, want 1", n)
	}
	if q.Len() != 4 {
		t.Errorf("Len = %d, want 4", q.Len())
	}
	if tr, _ := q.Current(); tr.VideoID != "vidD" {
		t.Errorf("current = %s, want vidD", tr.VideoID)
	}
	if q.HasNext() {
		t.Error("no track should remain after the current one")
	}
	if q.UpcomingCount() != 0 {
		t.Errorf("UpcomingCount = %d, want 0", q.UpcomingCount())
	}
	// Nothing left to clear.
	if n := q.ClearUpcoming(); n != 0 {
		t.Errorf("second ClearUpcoming removed %d, want 0", n)
	}
}

func TestQueue_MutationsKeepInvariants(t *testing.T) {
	q := NewQueue(makeTracks(8), 2)
	q.SetShuffle(true)
	ops := []func() bool{
		func() bool { return q.MoveDown(5) },
		func() bool { return q.MoveUp(7) },
		func() bool { return q.Remove(4) },
		func() bool { return q.MoveDown(3) },
		func() bool { return q.Remove(6) },
	}
	for _, op := range ops {
		op()
		assertPermutation(t, q.Order(), q.Len())
		if q.CurrentIndex() < 0 || q.CurrentIndex() >= q.Len() {
			t.Fatalf("current index out of range: %d", q.CurrentIndex())
		}
		if _, ok := q.Current(); !ok {
			t.Fatal("Current must resolve after each mutation")
		}
	}
}
