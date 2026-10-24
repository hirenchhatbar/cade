package main

import (
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"
)

// Every shown binding needs text for the places it appears in.
func TestBindingsDocumented(t *testing.T) {
	for _, bs := range [][]binding{sidebarKeys, paneKeys} {
		for _, b := range bs {
			if b.label != "" && b.help == "" {
				t.Errorf("[%s] has no help text", b.label)
			}
			if b.label == "" && b.run == nil {
				t.Errorf("binding %v is neither shown nor runnable", b.keys)
			}
		}
	}
}

// No key may trigger two sidebar bindings, or the second would be dead.
func TestBindingsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, b := range sidebarKeys {
		for _, k := range b.keys {
			if seen[k] {
				t.Errorf("key %q bound twice", k)
			}
			seen[k] = true
		}
	}
}

func TestSidebarKeysRoute(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}}, terms: nil}
	press := func(k vaxis.Key) { a.key(k) }
	press(vaxis.Key{Keycode: 'j', Text: "j"})
	if a.cursor != 1 {
		t.Fatalf("j: cursor %d", a.cursor)
	}
	press(vaxis.Key{Keycode: vaxis.KeyEnd})
	if a.cursor != 2 {
		t.Fatalf("End: cursor %d", a.cursor)
	}
	press(vaxis.Key{Keycode: '?', Text: "?"})
	if !a.help {
		t.Fatal("? should open help")
	}
	press(vaxis.Key{Keycode: 'x', Text: "x"})
	if a.help {
		t.Fatal("any key should close help")
	}
	// kitty protocol reports ? as Shift+/, which vaxis also matches against "/"
	press(vaxis.Key{Keycode: '/', ShiftedCode: '?', Modifiers: vaxis.ModShift, Text: "?"})
	if !a.help || a.search != nil {
		t.Fatalf("Shift+/: help %v search %v", a.help, a.search)
	}
	press(vaxis.Key{Keycode: 'x', Text: "x"})
	if a.key(vaxis.Key{Keycode: 'q', Text: "q"}) != true {
		t.Fatal("q should quit")
	}
}

func TestHelpAndHintsFromTable(t *testing.T) {
	var help []string
	for _, l := range helpSections() {
		help = append(help, l[0])
	}
	for _, label := range []string{"j/k", "d/dd", "?", focusLbl} {
		if !strings.Contains(strings.Join(help, "|"), label) {
			t.Errorf("help is missing [%s]", label)
		}
	}
	a := &app{}
	hidden := a.hintPairs(sidebarKeys, true)
	if len(hidden) != 2 || hidden[0][0] != "?" || hidden[1][0] != "H" {
		t.Fatalf("hidden hints = %v", hidden)
	}
}

func TestCLI(t *testing.T) {
	var out, errOut strings.Builder
	if code := cli([]string{"--keys"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "| `d/dd` |") || !strings.Contains(out.String(), "### Claude pane") {
		t.Fatalf("--keys: code %d\n%s", code, out.String())
	}
	out.Reset()
	if code := cli([]string{"-h"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "--keys") || !strings.HasPrefix(out.String(), logo) {
		t.Fatalf("--help: code %d", code)
	}
	if code := cli([]string{"--nope"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "unknown option") {
		t.Fatalf("unknown: code %d", code)
	}
}

func TestSearchKeys(t *testing.T) {
	a := &app{tree: tree{projects: []*project{
		{Name: "a", Dir: "/a", sessions: []session{{ID: "s1", Title: "fix login"}, {ID: "s2", Title: "docs"}}},
		{Name: "b", Dir: "/b", Collapsed: true, sessions: []session{{ID: "s3", Title: "refactor logger"}}},
	}}}
	typ := func(s string) {
		for _, r := range s {
			a.key(vaxis.Key{Keycode: r, Text: string(r)})
		}
	}
	typ("/log")
	if a.search == nil || a.search.query != "log" {
		t.Fatalf("search = %+v", a.search)
	}
	hits := a.searchHits()
	if len(hits) != 2 || hits[0].item != (row{0, 0}) || hits[1].item != (row{1, 0}) {
		t.Fatalf("hits = %+v", hits) // collapsed project b is searched too
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyDown})
	a.key(vaxis.Key{Keycode: vaxis.KeyDown})
	if a.search.sel != 1 {
		t.Fatalf("sel = %d, want clamped to 1", a.search.sel)
	}
	typ("j") // typed, not a cursor move
	if a.search.query != "logj" || a.cursor != 0 {
		t.Fatalf("query %q cursor %d", a.search.query, a.cursor)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyEsc})
	if a.search != nil {
		t.Fatal("Esc should close search")
	}
}

func TestAttention(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", Collapsed: true, sessions: []session{{ID: "s1"}, {ID: "s2"}}}}}}
	if !a.markAttn("s2") || !a.markAttn("s1") || a.markAttn("s2") {
		t.Fatal("markAttn should add each session once")
	}
	if strings.Join(a.attn, ",") != "s2,s1" || a.waiting(a.projects[0]) != 2 {
		t.Fatalf("attn %v", a.attn)
	}
	a.active = "s2"
	a.setFocus(true)
	if strings.Join(a.attn, ",") != "s1" {
		t.Fatalf("focusing should clear the active session: %v", a.attn)
	}
	a.attn = []string{"gone"}
	a.focusWaiting()
	if len(a.attn) != 0 || a.msg == "" {
		t.Fatalf("missing sessions should drop out: attn %v msg %q", a.attn, a.msg)
	}
}
