package main

import (
	"slices"
	"testing"
)

func TestFuzzy(t *testing.T) {
	for _, c := range []struct {
		s, q string
		pos  []int
		ok   bool
	}{
		{"cade", "", nil, true},
		{"Cade", "cade", []int{0, 1, 2, 3}, true},
		{"my-project", "proj", []int{3, 4, 5, 6}, true},
		{"my-project", "mp", []int{0, 3}, true},
		{"café-x", "éx", []int{3, 5}, true}, // positions are runes, not bytes
		{"cade", "cx", nil, false},
	} {
		_, pos, ok := fuzzy(c.s, c.q)
		if ok != c.ok || !slices.Equal(pos, c.pos) {
			t.Errorf("fuzzy(%q, %q) = %v %v, want %v %v", c.s, c.q, pos, ok, c.pos, c.ok)
		}
	}
}

func TestFuzzyFilterRanks(t *testing.T) {
	items := []string{"xproj", "p-r-o-j", "proj-cli", "proj", "other"}
	var got []string
	for _, m := range fuzzyFilter("proj", items, func(s string) string { return s }) {
		got = append(got, m.item)
	}
	if want := []string{"proj", "proj-cli", "xproj", "p-r-o-j"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
