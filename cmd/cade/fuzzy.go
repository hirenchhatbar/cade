package main

import (
	"slices"
	"strings"
	"unicode"
)

// fuzzyMatch is an item that matched a query, with the matched rune positions for highlighting.
type fuzzyMatch[T any] struct {
	item  T
	pos   []int
	score int
}

// fuzzyFilter keeps the items whose key matches query, best first; ties keep input order,
// so an empty query keeps every item in place.
func fuzzyFilter[T any](query string, items []T, key func(T) string) []fuzzyMatch[T] {
	var out []fuzzyMatch[T]
	for _, it := range items {
		if score, pos, ok := fuzzy(key(it), query); ok {
			out = append(out, fuzzyMatch[T]{it, pos, score})
		}
	}
	slices.SortStableFunc(out, func(a, b fuzzyMatch[T]) int { return b.score - a.score })
	return out
}

// fuzzy matches query against s case-insensitively and returns a score and the matched
// rune positions. Exact beats prefix beats substring beats scattered letters; scattered
// matches score higher when consecutive, on word starts and early in s.
func fuzzy(s, query string) (score int, pos []int, ok bool) {
	// ToLower maps rune to rune, so positions in r are positions in s.
	r, q := []rune(strings.ToLower(s)), []rune(strings.ToLower(query))
	if len(q) == 0 {
		return 0, nil, true
	}
	if i := runeIndex(r, q); i >= 0 {
		for j := range q {
			pos = append(pos, i+j)
		}
		switch {
		case len(r) == len(q):
			return 1000, pos, true
		case i == 0:
			return 900, pos, true
		case wordStart(r, i):
			return 850 - i, pos, true
		}
		return 800 - i, pos, true
	}
	score = 500
	for i := 0; i < len(r) && len(pos) < len(q); i++ {
		if r[i] != q[len(pos)] {
			continue
		}
		if len(pos) > 0 && pos[len(pos)-1] == i-1 {
			score += 12
		}
		if wordStart(r, i) {
			score += 10
		}
		pos = append(pos, i)
	}
	if len(pos) < len(q) {
		return 0, nil, false
	}
	return score - 2*pos[0] - (pos[len(pos)-1] - pos[0]), pos, true
}

func runeIndex(r, q []rune) int {
	for i := 0; i+len(q) <= len(r); i++ {
		if slices.Equal(r[i:i+len(q)], q) {
			return i
		}
	}
	return -1
}

// wordStart reports whether r[i] begins a word: the first rune, or one after a separator like - _ . or space.
func wordStart(r []rune, i int) bool {
	return i == 0 || !unicode.IsLetter(r[i-1]) && !unicode.IsDigit(r[i-1])
}
