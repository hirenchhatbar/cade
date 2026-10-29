package main

import (
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"
)

const sampleDiff = `diff --git a/f.go b/f.go
index 1..2 100644
--- a/f.go
+++ b/f.go
@@ -9,4 +9,4 @@ func f() {
 	a := 1
-	b := 2
-	c := 3
+	b := 20
 	d := 4
+	e := 5
`

// text renders segments as plain rows, trailing spaces cut.
func text(segs []vaxis.Segment) []string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	rows := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	return rows
}

func TestDiffSideBySide(t *testing.T) {
	got := text(diffSegments(parseDiff(sampleDiff, true), 41, true, mustTheme(themes["dracula"])))
	want := []string{
		"diff --git a/f.go b/f.go",
		"@@ -9,4 +9,4 @@ func f() {",
		" 9     a := 1      │ 9     a := 1",
		"10     b := 2      │10     b := 20",
		"11     c := 3      │",
		"12     d := 4      │11     d := 4",
		"                   │12     e := 5",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestDiffUnified(t *testing.T) {
	lines := parseDiff(sampleDiff, false)
	if len(lines) != 11 || lines[2].kind != diffText || lines[6].kind != diffDel || lines[10].kind != diffAdd {
		t.Fatalf("lines = %+v", lines) // --- inside the header is not a deleted line
	}
	if got := text(diffSegments(parseDiff("no changes", true), 80, true, theme{})); len(got) != 1 || got[0] != "no changes" {
		t.Fatalf("plain text = %q", got)
	}
}

func TestCut(t *testing.T) {
	if got := cut("abcdefg", 3, true); strings.Join(got, "|") != "abc|def|g  " {
		t.Fatalf("wrap = %q", got)
	}
	if got := cut("abcdefg", 3, false); strings.Join(got, "|") != "ab…" {
		t.Fatalf("cut = %q", got)
	}
	if got := cut("ab", 3, false); strings.Join(got, "|") != "ab " {
		t.Fatalf("short = %q", got)
	}
	if got := cut("日本", 3, true); strings.Join(got, "|") != "日 |本 " {
		t.Fatalf("wide = %q", got)
	}
}

// Keys scroll the diff and never reach the sidebar; a refresh keeps the offset;
// s switches mode and is remembered; Esc closes.
func TestDiffKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := &app{tree: tree{projects: []*project{{Name: "a", Dir: "/a", sessions: []session{{ID: "s1"}}}}}}
	a.diff = &diffView{dir: "/a", busy: true, laid: 80, focus: true}
	a.key(vaxis.Key{Keycode: 'j', Text: "j"})
	a.key(vaxis.Key{Keycode: 'j', Text: "j"})
	a.key(vaxis.Key{Keycode: 'q', Text: "q"})
	if a.diff.pg.Offset != 2 || a.cursor != 0 || a.quit {
		t.Fatalf("offset %d cursor %d quit %v", a.diff.pg.Offset, a.cursor, a.quit)
	}
	a.handle(diffEvent{"/other", "stale"})
	if a.diff.raw != "" {
		t.Fatal("result for another project should be dropped")
	}
	a.handle(diffEvent{"/a", "x"})
	if a.diff.busy || a.diff.raw != "x" || a.diff.laid != 0 || a.diff.pg.Offset != 2 {
		t.Fatalf("refresh: %+v", a.diff)
	}
	a.key(vaxis.Key{Keycode: 's', Text: "s"})
	if !a.st.UnifiedDiff || !loadState(statePath()).UnifiedDiff {
		t.Fatalf("s: state %+v saved %+v", a.st, loadState(statePath()))
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyEsc})
	if a.diff != nil || a.inTerm {
		t.Fatalf("Esc: diff %v inTerm %v", a.diff, a.inTerm)
	}
}

// A drag side by side copies only the text of the half it started in.
func TestDiffSelection(t *testing.T) {
	lines := parseDiff(sampleDiff, true)
	d := &diffView{rows: text(diffSegments(lines, 41, true, theme{})), halves: diffHalves(lines, 41)}
	d.from, d.to = [2]int{3, 0}, [2]int{5, 40} // from the left numbers to past the right half
	if got := trimCopy(d.selection()); got != "b := 2\nc := 3\nd := 4" {
		t.Fatalf("left half: %q", got)
	}
	d.from, d.to = [2]int{3, 27}, [2]int{3, 29} // right half, backwards drag
	if d.to, d.from = d.from, d.to; d.selection() != "b :" {
		t.Fatalf("right half: %q", d.selection())
	}
	d.halves = [2][2]int{} // unified: whole rows
	d.from, d.to = [2]int{0, 5}, [2]int{1, 1}
	if got := d.selection(); got != "--git a/f.go b/f.go\n@@" {
		t.Fatalf("unified: %q", got)
	}
}
