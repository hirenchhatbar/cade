package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"
)

func TestTreeNavigation(t *testing.T) {
	tmp := t.TempDir()
	newDir := filepath.Join(tmp, "new")
	os.Mkdir(newDir, 0o755)
	a := &app{cfgPath: filepath.Join(tmp, "projects.json"), home: tmp, cwd: tmp, pending: map[string]bool{}}
	a.projects = []*project{
		{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}, {ID: "s2"}}},
		{Name: "b", Dir: "/b"},
	}
	// rows: 0 a-header, 1 a-new, 2 s1, 3 s2, 4 b-header, 5 b-new
	if got := len(a.rows()); got != 6 {
		t.Fatalf("rows = %d", got)
	}

	a.cursor = 3
	if a.cursor = a.header(1); a.cursor != 4 {
		t.Fatalf("next header = %d", a.cursor)
	}
	if a.cursor = a.header(-1); a.cursor != 0 {
		t.Fatalf("prev header = %d", a.cursor)
	}

	a.cursor = 2 // on s1, move project a down
	a.moveSaved(1)
	if a.projects[0].Name != "b" || a.rows()[a.cursor] != (row{1, 0}) {
		t.Fatalf("after move: %s first, cursor %v", a.projects[0].Name, a.rows()[a.cursor])
	}
	a.moveSaved(1) // already last: no-op
	if a.projects[1].Name != "a" {
		t.Fatal("moved past end")
	}
	saved, _ := loadProjects(a.cfgPath)
	if len(saved) != 2 || saved[0].Name != "b" {
		t.Fatalf("saved order %v", saved)
	}

	a.input = &prompt{}
	for _, k := range []vaxis.Key{{Text: "x"}, {Text: "y"}, {Keycode: vaxis.KeyBackspace}, {Keycode: vaxis.KeyEnter}} {
		a.promptKey(k)
	}
	if a.input.name != "x" || a.input.step != 1 {
		t.Fatalf("prompt %+v", a.input)
	}
	a.input.dir = newDir
	a.promptKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	if a.input != nil || len(a.projects) != 3 || a.projects[2].Name != "x" || a.rows()[a.cursor] != (row{2, rowHeader}) {
		t.Fatalf("add project failed: input=%+v projects=%d", a.input, len(a.projects))
	}
	a.input = &prompt{step: 1, dir: newDir}
	a.promptKey(vaxis.Key{Keycode: vaxis.KeyEnter})
	if a.input == nil || a.input.err != "already added" {
		t.Fatalf("duplicate accepted: %+v", a.input)
	}
}

func TestDelete(t *testing.T) {
	tmp := t.TempDir()
	a := &app{cfgPath: filepath.Join(tmp, "projects.json"), home: tmp, pending: map[string]bool{}}
	a.projects = []*project{
		{Name: "a", Dir: "/a", sessions: []session{{ID: "s1", Title: "one"}, {ID: "s2"}}},
		{Name: "b", Dir: "/b"},
	}
	dir := projectDir(filepath.Join(tmp, ".claude"), "/a")
	os.MkdirAll(filepath.Join(dir, "s1", "tool-results"), 0o755)
	os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "s2.jsonl"), []byte("{}"), 0o644)

	a.cursor = 2 // s1
	a.confirmSession()
	a.confirmKey(vaxis.Key{Keycode: 'n', Text: "n"})
	if a.confirm != nil || len(a.projects[0].sessions) != 2 {
		t.Fatal("n should cancel")
	}
	a.confirmSession()
	a.confirmKey(vaxis.Key{Keycode: 'y', Text: "y"})
	if _, err := os.Stat(filepath.Join(dir, "s1.jsonl")); !os.IsNotExist(err) {
		t.Fatal("s1.jsonl not deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "s1")); !os.IsNotExist(err) {
		t.Fatal("s1 dir not deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "s2.jsonl")); err != nil || len(a.projects[0].sessions) != 1 {
		t.Fatal("s2 should remain")
	}

	a.cursor = 1 // new-session row: d does nothing
	a.confirmSession()
	if a.confirm != nil {
		t.Fatal("confirm on non-session row")
	}

	a.confirmProject()
	a.confirmKey(vaxis.Key{Keycode: 'y', Text: "y"})
	if len(a.projects) != 1 || a.projects[0].Name != "b" {
		t.Fatalf("project not removed: %d", len(a.projects))
	}
	if _, err := os.Stat(filepath.Join(dir, "s2.jsonl")); err != nil {
		t.Fatal("removing a project must keep its sessions")
	}
	a.confirmProject()
	if a.confirm != nil || len(a.toasts) == 0 {
		t.Fatal("last project must not be removable")
	}
}

func TestMergeKeepsOrder(t *testing.T) {
	shown := []session{{ID: "p", Title: "new session"}, {ID: "a"}, {ID: "b"}, {ID: "gone"}}
	// disk order is by mtime: b was just resumed, c is new, p not written yet, gone deleted
	loaded := []session{{ID: "c"}, {ID: "b", Title: "b2"}, {ID: "a"}}
	got := merge(shown, loaded, map[string]bool{"p": true})
	var ids []string
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "c,p,a,b" || got[3].Title != "b2" {
		t.Fatalf("got %v", got)
	}
}

func TestFold(t *testing.T) {
	a := &app{cfgPath: filepath.Join(t.TempDir(), "projects.json")}
	a.projects = []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}, {Name: "b", Dir: "/b"}}
	a.cursor = 2      // s1
	a.foldSaved(true) // Left on a session: jump to header
	if a.cursor != 0 || a.projects[0].Collapsed {
		t.Fatalf("left on session: cursor %d collapsed %v", a.cursor, a.projects[0].Collapsed)
	}
	a.foldSaved(true)
	if !a.projects[0].Collapsed || len(a.rows()) != 3 || a.rows()[1] != (row{1, rowHeader}) {
		t.Fatalf("collapse: rows %v", a.rows())
	}
	if saved, _ := loadProjects(a.cfgPath); !saved[0].Collapsed {
		t.Fatal("collapsed state not saved")
	}
	a.foldSaved(false)
	if a.projects[0].Collapsed || len(a.rows()) != 5 {
		t.Fatal("expand failed")
	}
}

func TestScrollTree(t *testing.T) {
	a := &app{tree: tree{visible: 3}}
	a.projects = []*project{{Name: "a", Dir: "/a", sessions: make([]session, 8)}} // 10 rows
	a.cursor = 0
	a.scroll(5)
	if a.top != 5 || a.cursor != 5 {
		t.Fatalf("scroll down: top %d cursor %d", a.top, a.cursor)
	}
	a.scroll(100)
	if a.top != 7 {
		t.Fatalf("clamp bottom: top %d", a.top)
	}
	a.cursor = 9
	a.scroll(-4)
	if a.top != 0 || a.cursor != 2 {
		t.Fatalf("scroll up: top %d cursor %d", a.top, a.cursor)
	}
}

func TestFlow(t *testing.T) {
	rows := flow([]string{"[j/k] move", "[n] new session", "[⇧PgUp/Dn] reorder", "[?] help", "a very long item over the width"}, 32, 5)
	var lines []string
	for _, r := range rows {
		lines = append(lines, strings.Join(r, "  "))
	}
	want := []string{
		"[j/k] move  [n] new session", // after 5 columns taken
		"[⇧PgUp/Dn] reorder  [?] help",
		"a very long item over the width",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s", strings.Join(lines, "\n"))
	}
}

func TestAddProjectDirPicker(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"web/cade-app", "web/other", "web/.hidden", "notes"} {
		os.MkdirAll(filepath.Join(tmp, d), 0o755)
	}
	os.WriteFile(filepath.Join(tmp, "web", "file"), nil, 0o644)
	a := &app{cfgPath: filepath.Join(tmp, "projects.json"), home: tmp, cwd: tmp, pending: map[string]bool{}}
	a.projects = []*project{{Name: "a", Dir: "/a"}}
	a.openAdd()
	keys := func(ks ...vaxis.Key) {
		for _, k := range ks {
			a.promptKey(k)
		}
	}
	names := func() (out []string) {
		for _, m := range a.matches() {
			out = append(out, m.item)
		}
		return out
	}
	keys(vaxis.Key{Keycode: vaxis.KeyEnter})
	if a.input.dir != "~/" || strings.Join(names(), ",") != "notes,web" {
		t.Fatalf("start dir %q lists %v", a.input.dir, names())
	}
	keys(vaxis.Key{Text: "w"}, vaxis.Key{Keycode: vaxis.KeyTab})
	if a.input.dir != "~/web/" || strings.Join(names(), ",") != "cade-app,other" {
		t.Fatalf("tab: dir %q lists %v", a.input.dir, names())
	}
	keys(vaxis.Key{Keycode: vaxis.KeyTab, Modifiers: vaxis.ModShift}, vaxis.Key{Text: "n"})
	if a.input.step != 0 || a.input.name != "n" {
		t.Fatalf("shift+tab should go back to the name: %+v", a.input)
	}
	keys(vaxis.Key{Keycode: vaxis.KeyTab}, vaxis.Key{Keycode: vaxis.KeyUp}, vaxis.Key{Keycode: vaxis.KeyDown})
	if a.input.step != 1 || a.input.name != "n" {
		t.Fatalf("up on the top row, then down, should land back on the directory: %+v", a.input)
	}
	keys(vaxis.Key{Text: "h", Modifiers: vaxis.ModAlt})
	if !a.input.hidden || len(names()) != 3 {
		t.Fatalf("alt+h: %v", names())
	}
	keys(vaxis.Key{Text: "o"}, vaxis.Key{Text: "t"}, vaxis.Key{Keycode: vaxis.KeyDown}, vaxis.Key{Keycode: vaxis.KeyEnter})
	if a.input != nil || a.projects[1].Dir != filepath.Join(tmp, "web", "other") || a.projects[1].Name != "n" {
		t.Fatalf("partial name should add the match: input=%+v projects=%+v", a.input, a.projects)
	}
}
