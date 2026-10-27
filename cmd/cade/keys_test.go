package main

import (
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/term"
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
	a.key(vaxis.Key{Keycode: 'f', Modifiers: vaxis.ModCtrl})
	if len(a.attn) != 0 || len(a.toasts) == 0 {
		t.Fatalf("missing sessions should drop out: attn %v toasts %v", a.attn, a.toasts)
	}
}

// F2 puts the app's shell over the session pane, and F2 again goes back.
func TestShellToggle(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]*term.Model{"s1": term.New(), shellID: term.New()}, active: "s1", inTerm: true}
	f2 := vaxis.Key{Keycode: vaxis.KeyF02}
	a.key(f2)
	if a.active != shellID || !a.inTerm {
		t.Fatalf("F2: active %q inTerm %v", a.active, a.inTerm)
	}
	a.key(f2)
	if a.active != "s1" {
		t.Fatalf("F2 again: active %q", a.active)
	}
	// with no pane before it, F2 hides the shell to the empty pane, still running
	a.closeTerm("s1")
	a.key(f2)
	a.key(f2)
	if a.active != "" || a.inTerm || a.terms[shellID] == nil {
		t.Fatalf("hide: active %q inTerm %v", a.active, a.inTerm)
	}
	a.key(f2)
	if a.active != shellID {
		t.Fatalf("show again: active %q", a.active)
	}
}

// Pasted text in the sidebar must not run shortcuts, e.g. q quitting.
func TestPasteIsNotShortcut(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}}}
	for _, r := range "jqG?" {
		a.key(vaxis.Key{Keycode: r, Text: string(r), EventType: vaxis.EventPaste})
	}
	if a.quit || a.help || a.cursor != 0 {
		t.Fatalf("paste ran shortcuts: quit %v help %v cursor %d", a.quit, a.help, a.cursor)
	}
}

// Copied text loses trailing spaces per line and the shared indent only.
func TestTrimCopy(t *testing.T) {
	for in, want := range map[string]string{
		"  hello  ":                      "hello",
		"    if x {  \n      y()\n    }": "if x {\n  y()\n}",
		"  a  \n\n  b\t ":                "a\n\nb\t",
		"\tx  ":                          "\tx",
	} {
		if got := trimCopy(in); got != want {
			t.Errorf("trimCopy(%q) = %q, want %q", in, got, want)
		}
	}
}

// F4 and Ctrl+Z hide the sidebar while the pane has focus; the shell keeps Ctrl+Z.
func TestZoom(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]*term.Model{"s1": term.New(), shellID: term.New()}, active: "s1"}
	a.key(vaxis.Key{Keycode: vaxis.KeyF04})
	if !a.zoom || !a.inTerm || a.paneX() != 0 {
		t.Fatalf("F4: zoom %v inTerm %v x %d", a.zoom, a.inTerm, a.paneX())
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyF12}) // to the sidebar: it shows again
	if !a.zoom || a.paneX() != sideW+1 {
		t.Fatalf("sidebar focus: zoom %v x %d", a.zoom, a.paneX())
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyF12})
	ctrlZ := vaxis.Key{Keycode: 'z', Modifiers: vaxis.ModCtrl}
	a.key(ctrlZ)
	if a.zoom || a.paneX() != sideW+1 {
		t.Fatal("Ctrl+Z should unzoom")
	}
	a.active = shellID
	a.key(ctrlZ)
	if a.zoom {
		t.Fatal("Ctrl+Z belongs to the shell")
	}
}

// Ctrl+Shift+Q quits from the pane and the diff, asking first while sessions run.
func TestQuitAsks(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]*term.Model{"s1": term.New()}, active: "s1", inTerm: true}
	q := vaxis.Key{Keycode: 'q', ShiftedCode: 'Q', Modifiers: vaxis.ModCtrl | vaxis.ModShift}
	if a.key(q) || a.confirm == nil {
		t.Fatalf("running session: quit without asking, confirm %v", a.confirm)
	}
	if a.key(vaxis.Key{Keycode: 'n', Text: "n"}) || a.confirm != nil {
		t.Fatal("n should cancel")
	}
	a.diff = &diffView{}
	a.key(q)
	if !a.key(vaxis.Key{Keycode: 'y', Text: "y"}) {
		t.Fatal("y in the diff should quit")
	}
	a = &app{inTerm: true}
	if !a.key(q) {
		t.Fatal("nothing running: should quit at once")
	}
}
