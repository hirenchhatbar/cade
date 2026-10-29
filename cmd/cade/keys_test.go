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

// The shell splits the pane under the session it was opened over; Alt+↑/↓ move
// focus between them, F11 makes the shell full height, and its split height is set.
func TestShellSplit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}, {ID: "s2"}}}}},
		terms: map[string]*term.Model{"s1": term.New(), "s2": term.New(), shellID: term.New()}, active: "s1", inTerm: true}
	a.key(vaxis.Key{Keycode: vaxis.KeyF02})
	if ps := a.paneTerms(42); len(ps) != 2 || ps[0] != (paneTerm{"s1", 1, 20}) || ps[1] != (paneTerm{shellID, 22, 20}) {
		t.Fatalf("split: %v", ps)
	}
	if p := a.paneTermAt(21, 42); p.key != shellID { // the shell's title bar
		t.Fatalf("row 21: %q", p.key)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyUp, Modifiers: vaxis.ModAlt})
	if a.active != "s1" || !a.inTerm || !a.shellOn {
		t.Fatalf("Alt+Up: active %q inTerm %v shellOn %v", a.active, a.inTerm, a.shellOn)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyDown, Modifiers: vaxis.ModAlt})
	if a.active != shellID {
		t.Fatalf("Alt+Down: active %q", a.active)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyF11})
	if ps := a.paneTerms(42); !a.st.ShellFull || len(ps) != 1 || ps[0].key != shellID || loadState(statePath()).ShellFull != true {
		t.Fatalf("F11: full %v panes %v", a.st.ShellFull, ps)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyF11})
	a.cfg.Shell.Height = 30
	if ps := a.paneTerms(42); a.st.ShellFull || len(ps) != 2 || ps[1].h != 12 {
		t.Fatalf("F11 again: full %v panes %v", a.st.ShellFull, ps)
	}
	a.closeTerm("s1") // the session above ends: the shell fills the pane
	if ps := a.paneTerms(42); a.active != shellID || !a.shellOn || len(ps) != 1 {
		t.Fatalf("session closed: active %q panes %v", a.active, ps)
	}
	a.show("s2") // another session hides the shell
	if a.shellOn || a.split() {
		t.Fatal("s2 should hide the shell")
	}
}

// The diff splits under the session like the shell, with its own height and
// F11; it takes the shell's place, and keys go to the side with focus.
func TestDiffSplit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]*term.Model{"s1": term.New(), shellID: term.New()}, active: "s1", inTerm: true}
	a.cfg.Diff.Height = 25
	a.key(vaxis.Key{Keycode: vaxis.KeyF02})
	a.diff = &diffView{dir: "/a", busy: true} // what F3 opens, without running git
	a.shellOn = false
	a.toggleShell() // F2 over the diff: the shell takes its place
	if a.diff != nil || !a.shellOn {
		t.Fatalf("F2: diff %v shellOn %v", a.diff, a.shellOn)
	}
	a.shellOn, a.active = false, "s1"
	a.diff = &diffView{dir: "/a", busy: true, back: true, focus: true}
	a.setFocus(false)
	if ps := a.paneTerms(42); len(ps) != 2 || ps[0] != (paneTerm{"s1", 1, 30}) || ps[1] != (paneTerm{diffID, 32, 10}) {
		t.Fatalf("split: %v", ps)
	}
	a.key(vaxis.Key{Keycode: 'j', Text: "j"})
	if a.diff.pg.Offset != 1 {
		t.Fatal("j should scroll the focused diff")
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyUp, Modifiers: vaxis.ModAlt})
	if a.diff.focus || !a.inTerm || a.diffFocused() {
		t.Fatalf("Alt+Up: diff focus %v inTerm %v", a.diff.focus, a.inTerm)
	}
	a.key(vaxis.Key{Keycode: 'j', Text: "j"}) // to Claude now
	if a.diff.pg.Offset != 1 {
		t.Fatal("j reached the diff")
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyDown, Modifiers: vaxis.ModAlt})
	a.key(vaxis.Key{Keycode: vaxis.KeyF11})
	if ps := a.paneTerms(42); !a.st.DiffFull || a.st.ShellFull || len(ps) != 1 || ps[0].key != diffID || !a.diff.focus {
		t.Fatalf("F11: state %+v panes %v", a.st, ps)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyF11})
	a.key(vaxis.Key{Keycode: vaxis.KeyUp, Modifiers: vaxis.ModAlt})
	a.key(vaxis.Key{Keycode: vaxis.KeyF03}) // from the session: closes it, focus stays
	if a.diff != nil || !a.inTerm || a.active != "s1" {
		t.Fatalf("F3: diff %v inTerm %v", a.diff, a.inTerm)
	}
	a.closeTerm("s1")
	a.diff = &diffView{dir: "/a", busy: true, focus: true} // no session: the diff fills the pane
	if ps := a.paneTerms(42); len(ps) != 1 || ps[0].key != diffID {
		t.Fatalf("no session: %v", ps)
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

// The sidebar on the right puts the pane at column 0, ending before the separator.
func TestSidebarRight(t *testing.T) {
	a := &app{st: state{SidebarRight: true}}
	if x, end := a.paneSpan(100); x != 0 || end != 100-sideDef-1 || a.sideX(100) != 100-sideDef {
		t.Fatalf("pane %d..%d, sidebar at %d", x, end, a.sideX(100))
	}
	a.zoom, a.inTerm = true, true
	if x, end := a.paneSpan(100); x != 0 || end != 100 {
		t.Fatalf("zoomed pane %d..%d", x, end)
	}
}

// A remembered sidebar width moves the pane; widths stay between sideMin and a sideMin-wide pane.
func TestSidebarWidth(t *testing.T) {
	a := &app{st: state{SidebarW: 50}}
	if x, _ := a.paneSpan(100); x != 51 || a.sepX(100) != 50 {
		t.Fatalf("pane at %d, separator at %d", x, a.sepX(100))
	}
	a.st.SidebarRight = true
	if _, end := a.paneSpan(100); end != 49 || a.sepX(100) != 49 || a.sideX(100) != 50 {
		t.Fatalf("right: pane end %d, separator at %d, sidebar at %d", end, a.sepX(100), a.sideX(100))
	}
	for _, c := range [][3]int{{10, 100, sideMin}, {50, 100, 50}, {90, 100, 100 - sideMin - 1}} {
		if got := clampSide(c[0], c[1]); got != c[2] {
			t.Errorf("clampSide(%d, %d) = %d, want %d", c[0], c[1], got, c[2])
		}
	}
}

// F4 and Ctrl+Z hide the sidebar while the pane has focus; the shell keeps Ctrl+Z.
func TestZoom(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]*term.Model{"s1": term.New(), shellID: term.New()}, active: "s1"}
	a.key(vaxis.Key{Keycode: vaxis.KeyF04})
	if x, _ := a.paneSpan(100); !a.zoom || !a.inTerm || x != 0 {
		t.Fatalf("F4: zoom %v inTerm %v x %d", a.zoom, a.inTerm, x)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyF12}) // to the sidebar: it shows again
	if x, _ := a.paneSpan(100); !a.zoom || x != sideDef+1 {
		t.Fatalf("sidebar focus: zoom %v x %d", a.zoom, x)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyF12})
	ctrlZ := vaxis.Key{Keycode: 'z', Modifiers: vaxis.ModCtrl}
	a.key(ctrlZ)
	if x, _ := a.paneSpan(100); a.zoom || x != sideDef+1 {
		t.Fatal("Ctrl+Z should unzoom")
	}
	a.active = shellID
	a.key(ctrlZ)
	if a.zoom {
		t.Fatal("Ctrl+Z belongs to the shell")
	}
}

// Ctrl+↑/↓ and Ctrl+⇧↑/↓ move the sidebar cursor from the pane and focus the sidebar.
func TestCursorKeysFromPane(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}, {Name: "b", Dir: "/b"}}},
		terms: map[string]*term.Model{"s1": term.New()}, active: "s1", inTerm: true}
	press := func(code rune, mods vaxis.ModifierMask, want int) {
		t.Helper()
		a.inTerm = true
		a.key(vaxis.Key{Keycode: code, Modifiers: mods})
		if a.cursor != want || a.inTerm || a.active != "s1" {
			t.Fatalf("cursor %d want %d, inTerm %v active %q", a.cursor, want, a.inTerm, a.active)
		}
	}
	press(vaxis.KeyDown, vaxis.ModCtrl, 1)
	press(vaxis.KeyDown, vaxis.ModCtrl|vaxis.ModShift, 3)
	press(vaxis.KeyUp, vaxis.ModCtrl|vaxis.ModShift, 0)
	press(vaxis.KeyUp, vaxis.ModCtrl, 0)
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
	no := false
	a = &app{terms: map[string]*term.Model{"s1": term.New()}, active: "s1", inTerm: true}
	a.cfg.Quit.Confirm = &no
	if !a.key(q) {
		t.Fatal("quit.confirm false: should quit at once")
	}
}

// F5 puts the app's editor in the pane and F5 again goes back; the editor keeps
// Ctrl+F, and when it exits the pane returns to the session.
func TestEditorToggle(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]*term.Model{"s1": term.New(), editorID: term.New()}, active: "s1", inTerm: true,
		attn: []string{"s1"}}
	f5 := vaxis.Key{Keycode: vaxis.KeyF05}
	a.key(f5)
	if a.active != editorID || !a.inTerm {
		t.Fatalf("F5: active %q inTerm %v", a.active, a.inTerm)
	}
	a.attn = []string{"s1"}
	a.key(vaxis.Key{Keycode: 'f', Modifiers: vaxis.ModCtrl})
	if a.active != editorID {
		t.Fatalf("Ctrl+F in the editor should go to it, active %q", a.active)
	}
	a.key(f5)
	if a.active != "s1" {
		t.Fatalf("F5 again: active %q", a.active)
	}
	// F2 over the editor splits the shell under it; F5 hides both
	a.terms[shellID] = term.New()
	a.key(f5)
	a.key(vaxis.Key{Keycode: vaxis.KeyF02})
	if ps := a.paneTerms(42); len(ps) != 2 || ps[0].key != editorID || ps[1].key != shellID {
		t.Fatalf("shell under editor: %v", ps)
	}
	a.key(f5)
	if a.active != "s1" || a.shellOn {
		t.Fatalf("F5 from the shell: active %q shellOn %v", a.active, a.shellOn)
	}
	a.key(f5)
	a.closeTerm(editorID) // the editor quit
	if a.active != "s1" {
		t.Fatalf("editor closed: active %q", a.active)
	}
}

// The settings row edits editor.cmd as text; Esc keeps the old value.
func TestEditorSetting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("VISUAL", "vim")
	a := &app{set: &settings{sel: setEditor}}
	if a.editorCmd() != "vim" {
		t.Fatalf("default: %q", a.editorCmd())
	}
	a.settingsKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	for _, r := range "nvim ." {
		a.settingsKey(vaxis.Key{Keycode: r, Text: string(r)})
	}
	a.settingsKey(vaxis.Key{Keycode: 'q', Text: "q"}) // typed, not close
	a.settingsKey(vaxis.Key{Keycode: vaxis.KeyBackspace})
	a.settingsKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	if cfg, _, _ := parseConfig(configPath()); a.set == nil || cfg.Editor.Cmd != "nvim ." || a.editorCmd() != "nvim ." {
		t.Fatalf("saved %q, in use %q", cfg.Editor.Cmd, a.editorCmd())
	}
	a.settingsKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	a.settingsKey(vaxis.Key{Keycode: 'x', Text: "x"})
	a.settingsKey(vaxis.Key{Keycode: vaxis.KeyEsc})
	if a.set == nil || a.set.typing || a.editorCmd() != "nvim ." {
		t.Fatalf("Esc: set %v cmd %q", a.set, a.editorCmd())
	}
}
