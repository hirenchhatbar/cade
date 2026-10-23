package main

import "testing"

func TestThumb(t *testing.T) {
	cases := []struct{ h, off, m, top, size int }{
		{10, 0, 0, 0, 10},  // no scrollback: full bar
		{10, 0, 90, 9, 1},  // at bottom
		{10, 90, 90, 0, 1}, // at top
		{10, 10, 10, 0, 5}, // half content visible, at top
		{10, 0, 10, 5, 5},  // half content visible, at bottom
	}
	for _, c := range cases {
		if top, size := thumb(c.h, c.off, c.m); top != c.top || size != c.size {
			t.Errorf("thumb(%d,%d,%d) = %d,%d want %d,%d", c.h, c.off, c.m, top, size, c.top, c.size)
		}
	}
	if offsetAt(0, 10, 90) != 90 || offsetAt(9, 10, 90) != 0 {
		t.Error("offsetAt endpoints wrong")
	}
}
