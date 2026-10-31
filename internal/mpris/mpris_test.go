package mpris

import (
	"regexp"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

var validPathElem = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func TestTrackPath(t *testing.T) {
	if p := trackPath(""); p != NoTrack {
		t.Errorf("trackPath(\"\") = %q, want NoTrack", p)
	}

	p := trackPath("abc-123_XYZ")
	want := "/org/mpris/MediaPlayer2/Track/abc_123_XYZ"
	if string(p) != want {
		t.Errorf("trackPath = %q, want %q", p, want)
	}
	if p[0] != '/' {
		t.Errorf("track path must be absolute: %q", p)
	}
	// The final element must contain only valid object-path characters.
	tail := string(p)[len("/org/mpris/MediaPlayer2/Track/"):]
	if !validPathElem.MatchString(tail) {
		t.Errorf("track path element %q contains invalid characters", tail)
	}
}

func TestMetadataShape(t *testing.T) {
	m := metadata(Snapshot{
		HasTrack: true,
		VideoID:  "vid",
		Title:    "Song",
		Artist:   "Artist",
		Album:    "Album",
		Duration: 65 * time.Second,
	})
	if got, _ := m["xesam:title"].Value().(string); got != "Song - Artist" {
		t.Errorf("title = %q, want %q", got, "Song - Artist")
	}
	if got, _ := m["mpris:length"].Value().(int64); got != 65_000_000 {
		t.Errorf("length = %d, want 65_000_000", got)
	}
	if got, _ := m["xesam:artist"].Value().([]string); len(got) != 1 || got[0] != "Artist" {
		t.Errorf("artist = %v", got)
	}

	// Without a track the NoTrack sentinel is used.
	empty := metadata(Snapshot{})
	if got, _ := empty["mpris:trackid"].Value().(dbus.ObjectPath); got != NoTrack {
		t.Errorf("no-track id = %q, want %q", got, NoTrack)
	}
}

func TestMetadataEqual(t *testing.T) {
	a := &Snapshot{VideoID: "v", Title: "T", Artist: "A", Duration: time.Second}
	b := &Snapshot{VideoID: "v", Title: "T", Artist: "A", Duration: time.Second}
	if !metadataEqual(a, b) {
		t.Error("identical metadata should compare equal")
	}
	b.Title = "Other"
	if metadataEqual(a, b) {
		t.Error("different titles should not compare equal")
	}
}

func TestClampUnit(t *testing.T) {
	for _, c := range []struct{ in, want float64 }{
		{-1, 0}, {0, 0}, {0.5, 0.5}, {1, 1}, {2, 1},
	} {
		if got := clampUnit(c.in); got != c.want {
			t.Errorf("clampUnit(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestNilServiceIsSafe(t *testing.T) {
	var s *Service
	s.SetSend(func(Command) {})
	s.Update(Snapshot{})
	s.Close()
}

// TestRegisterAndUpdate exercises the real D-Bus registration and property
// propagation. It skips when no session bus is available so the unit suite
// stays runnable in headless environments.
func TestRegisterAndUpdate(t *testing.T) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Skipf("no session bus: %v", err)
	}
	defer conn.Close()

	s, err := register(conn, "org.mpris.MediaPlayer2.ytm.test")
	if err != nil {
		t.Skipf("cannot register test name: %v", err)
	}
	defer s.Close()

	s.Update(Snapshot{
		HasTrack: true,
		Playing:  true,
		VideoID:  "vid",
		Title:    "Song",
		Artist:   "Artist",
		Duration: 30 * time.Second,
		Volume:   0.5,
		CanNext:  true,
	})

	obj := conn.Object("org.mpris.MediaPlayer2.ytm.test", ObjectPath)

	var status string
	if err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, playerIface, "PlaybackStatus").Store(&status); err != nil {
		t.Fatalf("Get PlaybackStatus: %v", err)
	}
	if status != "Playing" {
		t.Errorf("PlaybackStatus = %q, want Playing", status)
	}

	var meta map[string]dbus.Variant
	if err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, playerIface, "Metadata").Store(&meta); err != nil {
		t.Fatalf("Get Metadata: %v", err)
	}
	if got, _ := meta["xesam:title"].Value().(string); got != "Song - Artist" {
		t.Errorf("metadata title = %q, want %q", got, "Song - Artist")
	}
}
