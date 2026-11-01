package mediactl

import (
	"testing"
	"time"
)

func TestClampUnit(t *testing.T) {
	for _, c := range []struct{ in, want float64 }{
		{-1, 0}, {0, 0}, {0.5, 0.5}, {1, 1}, {2, 1},
	} {
		if got := clampUnit(c.in); got != c.want {
			t.Errorf("clampUnit(%v) = %v, want %v", c.in, got, c.want)
		}
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
	b = &Snapshot{VideoID: "v", Title: "T", Artist: "A", Duration: time.Second, ArtURL: "file:///art.jpg"}
	if metadataEqual(a, b) {
		t.Error("different artwork should not compare equal")
	}
	if metadataEqual(nil, a) || metadataEqual(a, nil) {
		t.Error("nil snapshots only equal other nil snapshots")
	}
	if !metadataEqual(nil, nil) {
		t.Error("two nil snapshots should compare equal")
	}
}
