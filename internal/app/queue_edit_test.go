package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Myleshen/yt-music-tui/internal/player"
	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// trackIDs returns the videoIDs of a track slice in order.
func trackIDs(tracks []ytmusic.Track) []string {
	out := make([]string, len(tracks))
	for i, t := range tracks {
		out[i] = t.VideoID
	}
	return out
}

func TestReorderUpNext_MovesSelectionDown(t *testing.T) {
	q := player.NewQueue(mkTracks(4), 0) // vA vB vC vD
	m := Model{queue: q}
	m.upNextCursor = 0 // selection = first upcoming (vB)

	cmds := m.reorderUpNext(true) // move vB after vC
	if len(cmds) == 0 {
		t.Fatal("expected an OSD command")
	}
	if got := trackIDs(q.Upcoming(4)); !reflect.DeepEqual(got, []string{"vC", "vB", "vD"}) {
		t.Errorf("upcoming = %v, want [vC vB vD]", got)
	}
	if m.upNextCursor != 1 {
		t.Errorf("cursor = %d, want 1 (follows the moved track)", m.upNextCursor)
	}
	if !m.osdVisible {
		t.Error("reorder should show a transient OSD")
	}
	if tr, _ := q.Current(); tr.VideoID != "vA" {
		t.Errorf("current = %s, want vA", tr.VideoID)
	}
}

func TestReorderUpNext_MovesSelectionUp(t *testing.T) {
	q := player.NewQueue(mkTracks(4), 0)
	m := Model{queue: q}
	m.upNextCursor = 2 // selection = third upcoming (vD)

	if cmds := m.reorderUpNext(false); len(cmds) == 0 {
		t.Fatal("expected an OSD command")
	}
	if got := trackIDs(q.Upcoming(4)); !reflect.DeepEqual(got, []string{"vB", "vD", "vC"}) {
		t.Errorf("upcoming = %v, want [vB vD vC]", got)
	}
	if m.upNextCursor != 1 {
		t.Errorf("cursor = %d, want 1", m.upNextCursor)
	}
}

func TestRemoveUpNext(t *testing.T) {
	q := player.NewQueue(mkTracks(4), 0)
	m := Model{queue: q}
	m.upNextCursor = 0 // selection = vB

	if cmds := m.removeUpNext(); len(cmds) == 0 {
		t.Fatal("expected an OSD command")
	}
	if q.Len() != 3 {
		t.Errorf("Len = %d, want 3", q.Len())
	}
	if got := trackIDs(q.Upcoming(4)); !reflect.DeepEqual(got, []string{"vC", "vD"}) {
		t.Errorf("upcoming = %v, want [vC vD]", got)
	}
	if tr, _ := q.Current(); tr.VideoID != "vA" {
		t.Errorf("current = %s, want vA", tr.VideoID)
	}
}

func TestClearUpcomingAction(t *testing.T) {
	q := player.NewQueue(mkTracks(4), 0)
	m := Model{queue: q}
	m.upNextCursor = 2

	if cmds := m.clearUpcoming(); len(cmds) == 0 {
		t.Fatal("expected an OSD command")
	}
	if q.Len() != 1 {
		t.Errorf("Len = %d, want 1", q.Len())
	}
	if m.upNextCursor != 0 {
		t.Errorf("cursor = %d, want 0", m.upNextCursor)
	}
	if tr, _ := q.Current(); tr.VideoID != "vA" {
		t.Errorf("current = %s, want vA (playback untouched)", tr.VideoID)
	}
	// Clearing again is a no-op.
	if cmds := m.clearUpcoming(); cmds != nil {
		t.Errorf("second clearUpcoming = %v, want nil", cmds)
	}
}

func TestQueueEdits_NoQueueAreNoOps(t *testing.T) {
	m := Model{}
	if cmds := m.reorderUpNext(true); cmds != nil {
		t.Errorf("reorderUpNext without a queue = %v", cmds)
	}
	if cmds := m.removeUpNext(); cmds != nil {
		t.Errorf("removeUpNext without a queue = %v", cmds)
	}
	if cmds := m.clearUpcoming(); cmds != nil {
		t.Errorf("clearUpcoming without a queue = %v", cmds)
	}
}

func TestRatingIcon(t *testing.T) {
	cases := map[ytmusic.Rating]string{
		ytmusic.RatingLike:        "♥",
		ytmusic.RatingDislike:     "✖",
		ytmusic.RatingIndifferent: "♡",
		"":                        "♡",
	}
	for r, want := range cases {
		if got := ratingIcon(r); got != want {
			t.Errorf("ratingIcon(%q) = %q, want %q", r, got, want)
		}
	}
}

func TestToggleRating_LikeOptimisticThenClear(t *testing.T) {
	client, err := ytmusic.NewClient("SAPISID=abc123")
	if err != nil {
		t.Fatal(err)
	}
	q := player.NewQueue(mkTracks(2), 0)
	m := Model{queue: q, ytClient: client, ratings: map[string]ytmusic.Rating{}}

	if cmds := m.toggleRating(ytmusic.RatingLike); len(cmds) == 0 {
		t.Fatal("expected commands")
	}
	if got := m.ratingOf("vA"); got != ytmusic.RatingLike {
		t.Errorf("rating = %q, want LIKE", got)
	}
	if !m.osdVisible || !strings.Contains(m.osdText, "liked") {
		t.Errorf("OSD = %q (visible=%v), want a liked message", m.osdText, m.osdVisible)
	}

	// Toggling like again clears the rating (unlike).
	m.toggleRating(ytmusic.RatingLike)
	if got := m.ratingOf("vA"); got != ytmusic.RatingIndifferent {
		t.Errorf("rating = %q, want INDIFFERENT", got)
	}
}

func TestToggleRating_Dislike(t *testing.T) {
	client, _ := ytmusic.NewClient("SAPISID=abc123")
	m := Model{queue: player.NewQueue(mkTracks(1), 0), ytClient: client, ratings: map[string]ytmusic.Rating{}}
	m.toggleRating(ytmusic.RatingDislike)
	if got := m.ratingOf("vA"); got != ytmusic.RatingDislike {
		t.Errorf("rating = %q, want DISLIKE", got)
	}
	// A like overrides a dislike.
	m.toggleRating(ytmusic.RatingLike)
	if got := m.ratingOf("vA"); got != ytmusic.RatingLike {
		t.Errorf("rating = %q, want LIKE", got)
	}
}

func TestToggleRating_RequiresAuth(t *testing.T) {
	client, _ := ytmusic.NewClient("")
	m := Model{queue: player.NewQueue(mkTracks(1), 0), ytClient: client, ratings: map[string]ytmusic.Rating{}}
	m.toggleRating(ytmusic.RatingLike)
	if got := m.ratingOf("vA"); got != ytmusic.RatingIndifferent {
		t.Errorf("unauthenticated toggle changed rating to %q", got)
	}
	if !m.osdError {
		t.Error("unauthenticated toggle should show an error toast")
	}
}

func TestRatingMsg_RollsBackOnError(t *testing.T) {
	m := Model{ratings: map[string]ytmusic.Rating{"vA": ytmusic.RatingLike}}
	nm, _ := m.Update(ratingMsg{videoID: "vA", prev: ytmusic.RatingIndifferent, err: errors.New("boom")})
	got := nm.(Model)
	if got.ratingOf("vA") != ytmusic.RatingIndifferent {
		t.Errorf("rating = %q, want rolled back to INDIFFERENT", got.ratingOf("vA"))
	}
	if !got.osdError {
		t.Error("a failed rating should show an error toast")
	}
}

func TestSeedLiked_DoesNotOverrideKnown(t *testing.T) {
	m := Model{ratings: map[string]ytmusic.Rating{"vA": ytmusic.RatingIndifferent}}
	// An explicit unlike removes the entry; simulate by deleting then seeding.
	delete(m.ratings, "vA")
	m.seedLiked(mkTracks(2))
	if m.ratingOf("vA") != ytmusic.RatingLike || m.ratingOf("vB") != ytmusic.RatingLike {
		t.Error("seedLiked should mark unknown tracks as liked")
	}
	// A known dislike is preserved.
	m.ratings["vB"] = ytmusic.RatingDislike
	m.seedLiked(mkTracks(2))
	if m.ratingOf("vB") != ytmusic.RatingDislike {
		t.Error("seedLiked must not overwrite a known rating")
	}
}

func TestMultiSelect_MarkAndCommitAppendsToQueue(t *testing.T) {
	q := player.NewQueue(mkTracks(1), 0) // vA playing
	m := Model{
		tab:           TabLibrary,
		queue:         q,
		mpv:           &player.MPV{},
		libraryList:   newList("Liked Songs"),
		libraryTracks: mkTracks(3), // vA vB vC
		marks:         map[int]bool{},
	}
	m.libraryList.SetItems(m.trackItems(m.libraryTracks))

	m.toggleSelect()
	if !m.selectMode {
		t.Fatal("toggleSelect should enable select mode")
	}
	m.libraryList.Select(0)
	m.toggleMark() // mark vA
	m.libraryList.Select(2)
	m.toggleMark() // mark vC
	if len(m.marks) != 2 {
		t.Fatalf("marks = %v, want 2 entries", m.marks)
	}

	cmds := m.commitSelect()
	if len(cmds) == 0 {
		t.Fatal("expected an OSD command")
	}
	if q.Len() != 3 {
		t.Errorf("queue Len = %d, want 3 (2 appended)", q.Len())
	}
	if m.selectMode || len(m.marks) != 0 {
		t.Errorf("select mode not reset: mode=%v marks=%v", m.selectMode, m.marks)
	}
}

func TestMultiSelect_UnmarkRemoves(t *testing.T) {
	m := Model{
		tab:           TabLibrary,
		libraryList:   newList("Liked Songs"),
		libraryTracks: mkTracks(2),
		marks:         map[int]bool{},
	}
	m.libraryList.SetItems(m.trackItems(m.libraryTracks))
	m.toggleSelect()
	m.libraryList.Select(0)
	m.toggleMark()
	if !m.marks[0] {
		t.Fatal("row 0 should be marked")
	}
	// toggleMark advances the cursor to 1; go back and unmark 0.
	m.libraryList.Select(0)
	m.toggleMark()
	if m.marks[0] {
		t.Error("row 0 should be unmarked after a second toggle")
	}
}

func TestMultiSelect_StartsPlaybackWithoutQueue(t *testing.T) {
	m := Model{
		tab:           TabSearch,
		searchList:    newList(""),
		searchResults: mkTracks(2),
		marks:         map[int]bool{1: true},
	}
	m.searchList.SetItems(m.trackItems(m.searchResults))
	cmds := m.commitSelect()
	if len(cmds) == 0 {
		t.Fatal("expected a start-playback command when nothing is playing")
	}
	if m.selectMode {
		t.Error("select mode should exit after commit")
	}
}

func TestExitSelect_ClearsStatus(t *testing.T) {
	m := Model{tab: TabLibrary, libraryList: newList("Liked Songs"), libraryTracks: mkTracks(1), marks: map[int]bool{0: true}, selectMode: true}
	m.libraryList.SetItems(m.trackItems(m.libraryTracks))
	m.status = "select: 1 marked"
	m.exitSelect()
	if m.selectMode || len(m.marks) != 0 {
		t.Errorf("exitSelect did not reset: mode=%v marks=%v", m.selectMode, m.marks)
	}
	if m.status != "" {
		t.Errorf("status = %q, want empty", m.status)
	}
}
