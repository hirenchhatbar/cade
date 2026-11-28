package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
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
	shown, all := a.hints(sidebarKeys, false), a.hints(sidebarKeys, true)
	if shown[0] != "[?] help" || !slices.Contains(shown, "[S] status bar") || slices.Contains(shown, "[⇥] pane") {
		t.Fatalf("hints = %v", shown)
	}
	if !slices.Contains(all, "[⇥] pane") { // all ignores show, so the bar's height can't change with focus
		t.Fatalf("all hints = %v", all)
	}
}

func TestShiftSTogglesBarNotStar(t *testing.T) {
	a := &app{}
	if b := a.match(sidebarKeys, vaxis.Key{Keycode: 's', ShiftedCode: 'S', Text: "S", Modifiers: vaxis.ModShift}); b == nil || b.label != "S" {
		t.Fatalf("Shift+s matched %v", b)
	}
	if b := a.match(sidebarKeys, vaxis.Key{Keycode: 's', Text: "s"}); b == nil || b.label != "s/*" {
		t.Fatalf("s matched %v", b)
	}
}

func TestLaunchOpts(t *testing.T) {
	for _, c := range []struct {
		args    []string
		mobile  bool
		density string
		ok      bool
	}{
		{[]string{"-m"}, true, "", true},
		{[]string{"--density=comfortable"}, false, "comfortable", true},
		{[]string{"-d", "compact", "--mobile"}, true, "compact", true},
		{[]string{"-m", "--density", "comfortable"}, true, "comfortable", true},
		{[]string{"--density=roomy"}, false, "", false},
		{[]string{"-d"}, false, "", false},
		{[]string{"--mobile=1"}, false, "", false},
	} {
		mobile, density, err := launchOpts(c.args)
		if (err == nil) != c.ok || mobile != c.mobile || density != c.density {
			t.Errorf("%v: %v %q %v", c.args, mobile, density, err)
		}
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
	items := func() []row {
		var rs []row
		for _, h := range a.searchHits() {
			rs = append(rs, h.item)
		}
		return rs
	}
	// a tree: each project's header, then its hits; collapsed project b is searched too
	if got, want := items(), []row{{0, rowHeader}, {0, 0}, {1, rowHeader}, {1, 0}}; !slices.Equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
	for range 5 {
		a.key(vaxis.Key{Keycode: vaxis.KeyDown})
	}
	if a.search.sel != 3 {
		t.Fatalf("sel = %d, want clamped to 3", a.search.sel)
	}
	typ("j") // typed, not a cursor move
	if a.search.query != "logj" || a.cursor != 0 {
		t.Fatalf("query %q cursor %d", a.search.query, a.cursor)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyEsc})
	if a.search != nil {
		t.Fatal("Esc should close search")
	}

	typ("/b") // only project b's name matches: its header, none of its sessions
	if got, want := items(), []row{{1, rowHeader}}; !slices.Equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyEnter})
	if a.search != nil || a.current() != (row{1, rowHeader}) || !a.projects[1].Collapsed {
		t.Fatalf("Enter on a project: search %v, cursor on %v, collapsed %v", a.search, a.current(), a.projects[1].Collapsed)
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
		terms: map[string]pane{"s1": term.New(), shellID: term.New()}, active: "s1", inTerm: true}
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
		terms: map[string]pane{"s1": term.New(), "s2": term.New(), shellID: term.New()}, active: "s1", inTerm: true}
	a.key(vaxis.Key{Keycode: vaxis.KeyF02})
	if ps := a.paneTerms(testW, 42); len(ps) != 2 || ps[0] != stacked(a, "s1", 1, 20) || ps[1] != stacked(a, shellID, 22, 20) {
		t.Fatalf("split: %v", ps)
	}
	if p := a.paneTermAt(0, 21, testW, 42); p.key != shellID { // the shell's title bar
		t.Fatalf("row 21: %q", p.key)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyUp, Modifiers: vaxis.ModAlt})
	if a.active != "s1" || !a.inTerm || a.low != shellID {
		t.Fatalf("Alt+Up: active %q inTerm %v low %q", a.active, a.inTerm, a.low)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyDown, Modifiers: vaxis.ModAlt})
	if a.active != shellID {
		t.Fatalf("Alt+Down: active %q", a.active)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyF11})
	if ps := a.paneTerms(testW, 42); !a.st.ShellFull || len(ps) != 1 || ps[0].key != shellID || loadState(statePath()).ShellFull != true {
		t.Fatalf("F11: full %v panes %v", a.st.ShellFull, ps)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyF11})
	a.cfg.Shell.Height = 30
	if ps := a.paneTerms(testW, 42); a.st.ShellFull || len(ps) != 2 || ps[1].h != 12 {
		t.Fatalf("F11 again: full %v panes %v", a.st.ShellFull, ps)
	}
	a.closeTerm("s1") // the session above ends: the shell fills the pane
	if ps := a.paneTerms(testW, 42); a.active != shellID || a.low != shellID || len(ps) != 1 {
		t.Fatalf("session closed: active %q panes %v", a.active, ps)
	}
	a.show("s2") // another session: the shell moves under it
	if ps := a.paneTerms(testW, 42); a.low != shellID || a.back != "s2" || len(ps) != 2 || ps[0].key != "s2" {
		t.Fatalf("s2: low %q back %q panes %v", a.low, a.back, ps)
	}
	a.st.ShellFull = true
	a.show("s1") // over a full-height shell: hides it
	if a.low != "" {
		t.Fatal("s1 should hide the full-height shell")
	}
}

const testW = 200 // screen width the split tests lay the pane out on

// stacked is the pane term key on rows y to y+h, as wide as the pane.
func stacked(a *app, key string, y, h int) paneTerm {
	x, end := a.paneSpan(testW)
	return paneTerm{key, x, y, end - x, h}
}

// F6 puts the shell beside the session after a line, stacked on a narrow pane;
// F11 on the session fills the pane with it; Alt+Shift+arrows and a drag on the line resize.
func TestSideBySide(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]pane{"s1": term.New(), shellID: term.New()}, active: "s1", inTerm: true}
	a.key(vaxis.Key{Keycode: vaxis.KeyF02})
	a.key(vaxis.Key{Keycode: vaxis.KeyF06})
	x, end := a.paneSpan(testW)
	lw := (end - x - 1) / 2
	if ps := a.paneTerms(testW, 42); !a.st.SplitCols || !loadState(statePath()).SplitCols || len(ps) != 2 ||
		ps[0] != (paneTerm{"s1", x, 1, end - x - 1 - lw, 41}) || ps[1] != (paneTerm{shellID, end - lw, 1, lw, 41}) {
		t.Fatalf("F6: %v", ps)
	}
	if p := a.paneTermAt(x, 30, testW, 42); p.key != "s1" || !a.onSplitLine(end-lw-1, 30, testW, 42) {
		t.Fatalf("col %d: %q", x, p.key)
	}
	if ps := a.paneTerms(x+2*splitMinW, 42); len(ps) != 2 || ps[0].y == ps[1].y {
		t.Fatalf("narrow pane should stack: %v", ps)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyLeft, Modifiers: vaxis.ModAlt | vaxis.ModShift})
	if cfg, _, _ := parseConfig(configPath()); a.cfg.Shell.Height != 60 || cfg.Shell.Height != 60 {
		t.Fatalf("Alt+Shift+Left: %d", a.cfg.Shell.Height)
	}
	a.dragging = true
	a.dragSplit(end-1-(end-x-1)/4, 30, testW, 42)
	if cfg, _, _ := parseConfig(configPath()); a.cfg.Shell.Height != 25 || cfg.Shell.Height != 60 {
		t.Fatalf("drag: %d, saved %d", a.cfg.Shell.Height, cfg.Shell.Height)
	}
	a.dragging = false // the release saves
	a.dragSplit(end-1-(end-x-1)/4, 30, testW, 42)
	if cfg, _, _ := parseConfig(configPath()); cfg.Shell.Height != 25 {
		t.Fatalf("release: saved %d", cfg.Shell.Height)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyUp, Modifiers: vaxis.ModAlt})
	a.key(vaxis.Key{Keycode: vaxis.KeyF11})
	if ps := a.paneTerms(testW, 42); !a.upFull || a.st.ShellFull || a.low != shellID || len(ps) != 1 || ps[0].key != "s1" {
		t.Fatalf("F11 on the session: %v", ps)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyF11})
	if ps := a.paneTerms(testW, 42); a.upFull || len(ps) != 2 || a.active != "s1" {
		t.Fatalf("F11 again: %v", ps)
	}
}

// The diff splits under the session like the shell, with its own height and
// F11; it takes the shell's place, and keys go to the side with focus.
func TestDiffSplit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]pane{"s1": term.New(), shellID: term.New()}, active: "s1", inTerm: true}
	a.cfg.Diff.Height = 25
	a.key(vaxis.Key{Keycode: vaxis.KeyF02})
	a.diff = &diffView{dir: "/a", busy: true} // what F3 opens, without running git
	a.low = ""
	a.toggleShell() // F2 over the diff: the shell takes its place
	if a.diff != nil || a.low != shellID {
		t.Fatalf("F2: diff %v low %q", a.diff, a.low)
	}
	a.low, a.active = "", "s1"
	a.diff = &diffView{dir: "/a", busy: true, back: true, focus: true}
	a.setFocus(false)
	if ps := a.paneTerms(testW, 42); len(ps) != 2 || ps[0] != stacked(a, "s1", 1, 30) || ps[1] != stacked(a, diffID, 32, 10) {
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
	if ps := a.paneTerms(testW, 42); !a.st.DiffFull || a.st.ShellFull || len(ps) != 1 || ps[0].key != diffID || !a.diff.focus {
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
	if ps := a.paneTerms(testW, 42); len(ps) != 1 || ps[0].key != diffID {
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

// F5 and Ctrl+Z hide the sidebar while the pane has focus; the shell keeps Ctrl+Z.
func TestZoom(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]pane{"s1": term.New(), shellID: term.New()}, active: "s1"}
	a.key(vaxis.Key{Keycode: vaxis.KeyF05})
	if x, _ := a.paneSpan(100); !a.zoom || !a.inTerm || x != 0 {
		t.Fatalf("F5: zoom %v inTerm %v x %d", a.zoom, a.inTerm, x)
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
		terms: map[string]pane{"s1": term.New()}, active: "s1", inTerm: true}
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

// Ctrl+Shift+Q quits from the pane and the diff, asking first while the shell
// runs; Claude sessions keep running in the server, so they never ask.
func TestQuitAsks(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]pane{"s1": term.New(), shellID: term.New()}, active: "s1", inTerm: true}
	q := vaxis.Key{Keycode: 'q', ShiftedCode: 'Q', Modifiers: vaxis.ModCtrl | vaxis.ModShift}
	if a.key(q) || a.confirm == nil {
		t.Fatalf("running shell: quit without asking, confirm %v", a.confirm)
	}
	if a.key(vaxis.Key{Keycode: 'n', Text: "n"}) || a.confirm != nil {
		t.Fatal("n should cancel")
	}
	a.diff = &diffView{}
	a.key(q)
	if !a.key(vaxis.Key{Keycode: 'y', Text: "y"}) {
		t.Fatal("y in the diff should quit")
	}
	a = &app{terms: map[string]pane{"s1": term.New()}, active: "s1", inTerm: true}
	if !a.key(q) {
		t.Fatal("only a Claude session: should quit at once")
	}
	no := false
	a = &app{terms: map[string]pane{shellID: term.New()}, active: shellID, inTerm: true}
	a.cfg.Quit.Confirm = &no
	if !a.key(q) {
		t.Fatal("quit.confirm false: should quit at once")
	}
}

// F4 splits the app's editor under the session like the shell, in the shell's
// place; it keeps Ctrl+F, has its own height and F11, and F4 again hides it.
func TestEditorSplit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]pane{"s1": term.New(), shellID: term.New(), editorID: term.New()}, active: "s1", inTerm: true}
	a.cfg.Editor.Height = 75
	f4 := vaxis.Key{Keycode: vaxis.KeyF04}
	a.key(f4)
	if ps := a.paneTerms(testW, 42); a.active != editorID || len(ps) != 2 || ps[0] != stacked(a, "s1", 1, 10) || ps[1] != stacked(a, editorID, 12, 30) {
		t.Fatalf("F4: active %q panes %v", a.active, ps)
	}
	a.attn = []string{"s1"}
	a.key(vaxis.Key{Keycode: 'f', Modifiers: vaxis.ModCtrl})
	if a.active != editorID {
		t.Fatalf("Ctrl+F in the editor should go to it, active %q", a.active)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyUp, Modifiers: vaxis.ModAlt})
	if a.active != "s1" || a.low != editorID {
		t.Fatalf("Alt+Up: active %q low %q", a.active, a.low)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyDown, Modifiers: vaxis.ModAlt})
	a.key(vaxis.Key{Keycode: vaxis.KeyF11})
	if ps := a.paneTerms(testW, 42); !a.st.EditorFull || a.st.ShellFull || len(ps) != 1 || ps[0].key != editorID {
		t.Fatalf("F11: panes %v", ps)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyF11})
	a.key(vaxis.Key{Keycode: vaxis.KeyF02}) // the shell takes the editor's place, under the same session
	if ps := a.paneTerms(testW, 42); a.active != shellID || len(ps) != 2 || ps[0].key != "s1" || ps[1] != stacked(a, shellID, 22, 20) {
		t.Fatalf("F2 over the editor: active %q panes %v", a.active, ps)
	}
	a.key(f4)
	a.key(f4)
	if a.active != "s1" || a.low != "" {
		t.Fatalf("F4 again: active %q low %q", a.active, a.low)
	}
	a.key(f4)
	a.closeTerm(editorID) // the editor quit
	if a.active != "s1" || a.low != "" {
		t.Fatalf("editor closed: active %q low %q", a.active, a.low)
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

func TestShowTitle(t *testing.T) {
	a := &app{rename: &renaming{id: "a", title: "typed", busy: true}}
	a.showTitle(titleEvent{id: "b", title: "other session"})
	if a.rename.title != "typed" || !a.rename.busy {
		t.Fatalf("result for another session applied: %+v", a.rename)
	}
	a.showTitle(titleEvent{id: "a", err: errors.New("boom")})
	if a.rename.title != "typed" || a.rename.busy || len(a.toasts) != 1 {
		t.Fatalf("failure: %+v, toasts %v", a.rename, a.toasts)
	}
	a.showTitle(titleEvent{id: "a", title: "Fix login flow"})
	if a.rename.title != "Fix login flow" {
		t.Fatalf("title = %q", a.rename.title)
	}
	a.rename = nil
	a.showTitle(titleEvent{id: "a", title: "late"}) // dialog closed: no panic
}

// r on a project row renames the project; an empty name falls back to the folder's.
func TestRenameProject(t *testing.T) {
	tmp := t.TempDir()
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/x/proj"}}}, cfgPath: filepath.Join(tmp, "projects.json"), home: tmp}
	a.openRename()
	if a.rename == nil || a.rename.id != "" || a.rename.title != "a" {
		t.Fatalf("dialog: %+v", a.rename)
	}
	a.rename.title = "  New name "
	a.renameKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	if a.rename != nil || a.projects[0].Name != "New name" {
		t.Fatalf("name = %q, dialog %+v", a.projects[0].Name, a.rename)
	}
	a.openRename()
	a.rename.title = ""
	a.renameKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	if a.projects[0].Name != "proj" {
		t.Fatalf("empty name = %q", a.projects[0].Name)
	}
}

// Opening sessions builds the history in state.json; Ctrl+Tab moves the cursor
// through it, focusing the sidebar, skipping sessions no longer listed.
func TestRecentSessions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := &app{tree: tree{projects: []*project{
		{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}, {ID: "s2"}}},
		{Name: "b", Dir: "/b", Collapsed: true, sessions: []session{{ID: "s3"}}},
	}}, terms: map[string]pane{"s2": term.New()}}
	for _, id := range []string{"s1", "gone", "s3", "s2"} {
		a.visit(id)
	}
	a.show("s2")
	ctrlTab := vaxis.Key{Keycode: vaxis.KeyTab, Modifiers: vaxis.ModCtrl}
	for _, want := range []string{"s2", "s3", "s1", "s2"} { // header row starts at the latest; skips "gone", wraps
		if a.key(ctrlTab); a.keyOf(a.current()).id != want {
			t.Fatalf("Ctrl+Tab: cursor on %q, want %q", a.keyOf(a.current()).id, want)
		}
	}
	if a.inTerm || a.active != "s2" || a.projects[1].Collapsed {
		t.Fatalf("inTerm %v active %q", a.inTerm, a.active)
	}
	if got := strings.Join(loadState(statePath()).Recent, " "); got != "s2 s3 gone s1" {
		t.Fatalf("saved history %q", got)
	}
}

// Opening another session keeps the diff open under it, focus on the session.
func TestOpenKeepsDiff(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}, {ID: "s2"}}}}},
		terms: map[string]pane{"s1": term.New(), "s2": term.New()}, active: "s1", pending: map[string]bool{}}
	a.diff = &diffView{dir: "/a", busy: true, focus: true}
	a.cursor = 3 // s2
	a.open()
	if a.diff == nil || a.diff.focus || a.active != "s2" || !a.inTerm {
		t.Fatalf("diff %v active %q inTerm %v", a.diff, a.active, a.inTerm)
	}
}

// --mobile shows the sidebar or the pane, one at a time; the pane keeps the full width either way.
func TestMobile(t *testing.T) {
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}},
		terms: map[string]pane{"s1": term.New()}, active: "s1", mobile: true, st: state{SidebarW: 50}}
	if x, end := a.paneSpan(45); !a.sheet() || x != 0 || end != 45 {
		t.Fatalf("sidebar focus: sheet %v, pane %d..%d", a.sheet(), x, end)
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyTab})
	if a.sheet() || !a.inTerm {
		t.Fatalf("Tab: sheet %v inTerm %v", a.sheet(), a.inTerm)
	}
	a.stepSide(1)
	if a.st.SidebarW != 50 || a.barH(45) != 1 {
		t.Fatalf("sidebar width %d, bar rows %d", a.st.SidebarW, a.barH(45))
	}
	// the status bar holds tap targets, right-aligned: "[new]  [diff]  [shell]  [del]  [quit]" from col 7
	if got := strings.Join(a.barHints(), "  "); got != "[new]  [diff]  [shell]  [del]  [quit]" {
		t.Fatalf("bar %q", got)
	}
	if a.tapAt(30, 45) != nil || a.tapAt(44, 45) != nil || a.tapAt(43, 45).label != "[quit]" {
		t.Fatal("tap in a gap or margin hit a target, or [quit] missed")
	}
	a.tapAt(31, 45).run(a)
	if a.confirm == nil || strings.Join(a.barHints(), "  ") != "[y] yes  [n] no" {
		t.Fatalf("[del]: confirm %v, bar %q", a.confirm, a.barHints())
	}
	a.tapAt(43, 45).run(a) // [n] no
	if a.confirm != nil || len(a.projects[0].sessions) != 1 {
		t.Fatalf("[n] no: confirm %v, sessions %d", a.confirm, len(a.projects[0].sessions))
	}
	a.taps()[len(a.taps())-1].run(a)
	if !a.quit {
		t.Fatal("[quit] should quit at once")
	}
}

// A selection dragged past a pane's top edge scrolls into the scrollback and
// a release there still finishes it.
func TestDragPastEdgeScrolls(t *testing.T) {
	vt := term.New()
	vt.Resize(20, 3)
	for i := range 10 {
		vt.WriteString(fmt.Sprintf("line %d\r\n", i))
	}
	left := func(typ vaxis.EventType, row int) vaxis.Mouse {
		return vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: typ, Row: row}
	}
	vt.Update(left(vaxis.EventPress, 1))
	for range 20 {
		vt.Update(left(vaxis.EventMotion, -1))
	}
	if off, m := vt.ScrollInfo(); off != m || m == 0 {
		t.Fatalf("scroll %d of %d", off, m)
	}
	vt.Update(left(vaxis.EventRelease, -1))
	if s := vt.Selection(); !strings.HasPrefix(s, "line 0") || !strings.HasSuffix(s, "line 8\nl") {
		t.Fatalf("selection %q", s)
	}
}

func TestConfirmEnterEsc(t *testing.T) {
	a := &app{}
	yes := false
	a.confirm = &confirmation{yes: func() { yes = true }}
	a.confirmKey(vaxis.Key{Keycode: vaxis.KeyEsc})
	if a.confirm != nil || yes {
		t.Fatalf("Esc: confirm %v yes %v", a.confirm, yes)
	}
	a.confirm = &confirmation{yes: func() { yes = true }}
	a.confirmKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	if a.confirm != nil || !yes {
		t.Fatalf("Enter: confirm %v yes %v", a.confirm, yes)
	}
}

func TestNewRowClickOnLabelOnly(t *testing.T) {
	a := &app{pending: map[string]bool{}, terms: map[string]pane{}}
	a.projects = []*project{{Name: "a", Dir: "/a"}}
	a.visible = 10
	click := func(col int) {
		a.sidebarMouse(vaxis.Mouse{Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress, Col: col, Row: titleH + 1}, true)
	}
	click(starX + runeLen(newRowLbl)) // blank rest of the [n] new session row
	if len(a.projects[0].sessions) != 0 || a.cursor != 1 {
		t.Fatalf("blank click: sessions %d cursor %d", len(a.projects[0].sessions), a.cursor)
	}
}
