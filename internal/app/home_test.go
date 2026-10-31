package app

import (
	"testing"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

func sampleHomeSections() []ytmusic.Section {
	return []ytmusic.Section{
		{
			Title:  "Listen again",
			Tracks: []ytmusic.Track{{VideoID: "v1", Title: "One"}, {VideoID: "v2", Title: "Two"}},
		},
		{
			Title: "Genres",
			Collections: []ytmusic.Collection{
				{Title: "Chill", BrowseID: "FEmusic_moods_and_genres_category", Params: "p"},
			},
		},
	}
}

// TestNewHomeViewRows verifies section headers are inserted but never selected,
// and that the cursor starts on the first real entry.
func TestNewHomeViewRows(t *testing.T) {
	v := newHomeView("Home", sampleHomeSections())
	// 2 headers + 3 entries.
	if len(v.rows) != 5 {
		t.Fatalf("rows = %d, want 5: %+v", len(v.rows), v.rows)
	}
	if v.rows[0].entry != -1 || v.rows[3].entry != -1 {
		t.Errorf("expected header rows at 0 and 3: %+v", v.rows)
	}
	if v.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (first entry)", v.cursor)
	}
}

// TestHomeViewMoveSkipsHeaders verifies j/k navigation steps over headers and
// that selected() returns the right entry.
func TestHomeViewMoveSkipsHeaders(t *testing.T) {
	v := newHomeView("Home", sampleHomeSections())

	_, e, ok := v.selected()
	if !ok || e.kind != entryTrack || e.track.VideoID != "v1" {
		t.Fatalf("initial selection = %+v ok=%v", e, ok)
	}

	v.move(true) // v1 -> v2
	_, e, _ = v.selected()
	if e.kind != entryTrack || e.track.VideoID != "v2" {
		t.Fatalf("after down = %+v", e)
	}

	v.move(true) // v2 -> Chill (skips the "Genres" header)
	_, e, _ = v.selected()
	if e.kind != entryCollection || e.collection.Title != "Chill" {
		t.Fatalf("after second down = %+v", e)
	}

	v.move(true) // already at the last entry: stays put
	_, e, _ = v.selected()
	if e.collection.Title != "Chill" {
		t.Fatalf("clamped selection = %+v", e)
	}

	v.move(false) // Chill -> v2
	_, e, _ = v.selected()
	if e.track.VideoID != "v2" {
		t.Fatalf("after up = %+v", e)
	}
}

// TestHomeViewSelectedTrackIndex checks the track index maps into the section's
// playable queue.
func TestHomeViewSelectedTrackIndex(t *testing.T) {
	v := newHomeView("Home", sampleHomeSections())
	v.move(true) // to v2
	sec, e, ok := v.selected()
	if !ok {
		t.Fatal("expected a selection")
	}
	if e.trackIdx != 1 || len(sec.tracks) != 2 || sec.tracks[e.trackIdx].VideoID != "v2" {
		t.Errorf("trackIdx = %d, queue = %+v", e.trackIdx, sec.tracks)
	}
}

// TestHomeViewEmpty ensures an empty feed does not panic and reports no
// selection.
func TestHomeViewEmpty(t *testing.T) {
	v := newHomeView("Home", nil)
	if _, _, ok := v.selected(); ok {
		t.Error("empty view should have no selection")
	}
	v.move(true)
	v.move(false)
}
