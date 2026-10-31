package app

import "testing"

func TestListRowIndex(t *testing.T) {
	// listTop=2 (below the tab header), 1 title row, items of 3 rows, 4 per
	// page, first page, 10 items.
	const top, title, item, perPage, page, count = 2, 1, 3, 4, 0, 10

	cases := []struct {
		y      int
		want   int
		wantOK bool
	}{
		{0, 0, false},  // in the tab header
		{2, 0, false},  // the title row
		{3, 0, true},   // first item, first line
		{5, 0, true},   // first item, last line
		{6, 1, true},   // second item
		{9, 2, true},   // third item
		{12, 3, true},  // fourth (last) item on the page
		{15, 0, false}, // past the page's items (padding)
		{top + title + 9, 3, true},
	}
	for _, c := range cases {
		got, ok := listRowIndex(c.y, top, title, item, perPage, page, count)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("listRowIndex(y=%d) = (%d,%v), want (%d,%v)", c.y, got, ok, c.want, c.wantOK)
		}
	}

	// Page 1 maps to the next block of items.
	if got, ok := listRowIndex(3, top, title, item, perPage, 1, count); !ok || got != 4 {
		t.Errorf("page 1 first row = (%d,%v), want (4,true)", got, ok)
	}

	// A click past the item count is rejected even if it is inside the page.
	if _, ok := listRowIndex(3, top, title, item, perPage, 0, 0); ok {
		t.Error("empty list should reject clicks")
	}
	if _, ok := listRowIndex(3, top, title, item, 0, 0, 10); ok {
		t.Error("zero page size should reject clicks")
	}
}
