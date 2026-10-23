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
