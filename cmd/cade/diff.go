package main

import (
	"errors"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/pager"
)

// diffView is the F3 diff of one project, split under the Claude pane or in its place.
type diffView struct {
	name, dir string
	raw       string // git's output
	laid      int    // pane width pg was laid out for; 0 = lay out again
	pg        pager.Model
	rows      []string // pg's rows as text, for selections
	halves    [2][2]int
	back      bool   // focus was on the Claude pane when it opened
	focus     bool   // keys go to the diff, not to Claude
	busy      bool   // a git diff is running; refresh ticks skip until it is back
	from, to  [2]int // selection, row (of rows) and column; from == to is none
}

// diffEvent carries git's output for dir back to the event loop.
type diffEvent struct {
	dir string
	out string
}

// diffDir is the project of the open session, else the cursor's project.
func (a *app) diffDir() *project {
	for _, p := range a.projects {
		for _, s := range p.sessions {
			if s.ID == a.active {
				return p
			}
		}
	}
	return a.projects[a.current().p]
}

// openDiff shows the diff, focused, in place of the shell; when shown it closes it.
func (a *app) openDiff() {
	if a.diff != nil {
		a.closeDiff()
		return
	}
	if a.shellOn {
		a.toggleShell()
	}
	p := a.diffDir()
	a.diff = &diffView{name: p.Name, dir: p.Dir, back: a.inTerm, focus: true}
	a.setFocus(false)
	a.loadDiff()
}

// closeDiff gives the focus back to where it was when the diff opened, if the diff has it.
func (a *app) closeDiff() {
	back, focus := a.diff.back, a.diff.focus
	a.diff = nil
	if focus {
		a.setFocus(back && hasPane(a))
	}
}

// loadDiff runs git diff in the background; the result arrives as a diffEvent.
func (a *app) loadDiff() {
	if a.diff.busy {
		return
	}
	a.diff.busy = true
	dir := a.diff.dir
	go func() { a.vx.PostEvent(diffEvent{dir, gitDiff(dir)}) }()
}

// showDiff replaces the overlay's text; the scroll offset is kept.
func (a *app) showDiff(ev diffEvent) {
	if a.diff == nil || a.diff.dir != ev.dir {
		return // closed, or reopened on another project, while git ran
	}
	a.diff.busy = false
	if a.diff.raw != ev.out {
		a.diff.raw, a.diff.laid = ev.out, 0
	}
}

// drawDiff lays the diff out again when it, the mode or the pane width changed,
// and draws it with the selection.
func (a *app) drawDiff(win vaxis.Window) {
	d := a.diff
	if w, _ := win.Size(); d.laid != w {
		d.laid = w
		lines := parseDiff(d.raw, !a.st.UnifiedDiff)
		d.pg.Segments = diffSegments(lines, w, a.cfg.Diff.Wrap == nil || *a.cfg.Diff.Wrap, a.th)
		d.pg.Layout()
		var b strings.Builder
		for _, s := range d.pg.Segments {
			b.WriteString(s.Text)
		}
		d.rows = strings.Split(b.String(), "\n")
		d.halves = [2][2]int{}
		if !a.st.UnifiedDiff {
			d.halves = diffHalves(lines, w)
		}
	}
	d.pg.Draw(win)
	_, h := win.Size()
	d.selected(func(r, lo, hi int) {
		if r < d.pg.Offset || r >= d.pg.Offset+h {
			return
		}
		col := 0
		for _, c := range vaxis.Characters(d.rows[r]) {
			if col >= lo && col < hi {
				win.SetCell(col, r-d.pg.Offset, vaxis.Cell{Character: c, Style: a.th.sel})
			}
			col += c.Width
		}
	})
}

// selected calls f with each selected row and its columns lo to hi. Side by
// side, the selection keeps to the text of the half it started in.
func (d *diffView) selected(f func(r, lo, hi int)) {
	from, to := d.from, d.to
	if from == to {
		return
	}
	if to[0] < from[0] || to[0] == from[0] && to[1] < from[1] {
		from, to = to, from
	}
	clip := [2]int{0, math.MaxInt}
	for _, h := range d.halves {
		if h != [2]int{} && d.from[1] < h[1]+1 { // the separator counts to the left half
			clip = h
			break
		}
	}
	for r := from[0]; r <= min(to[0], len(d.rows)-1); r++ {
		lo, hi := 0, math.MaxInt
		if r == from[0] {
			lo = from[1]
		}
		if r == to[0] {
			hi = to[1] + 1
		}
		if lo, hi = max(lo, clip[0]), min(hi, clip[1]); lo < hi {
			f(r, lo, hi)
		}
	}
}

// selection is the selected text, one line per row.
func (d *diffView) selection() string {
	var lines []string
	d.selected(func(r, lo, hi int) {
		var b strings.Builder
		col := 0
		for _, c := range vaxis.Characters(d.rows[r]) {
			if col >= lo && col < hi {
				b.WriteString(c.Grapheme)
			}
			col += c.Width
		}
		lines = append(lines, b.String())
	})
	return strings.Join(lines, "\n")
}

// diffMouse scrolls the diff with the wheel and selects with a left drag,
// copying on release like the Claude pane. row and col are within the diff.
func (a *app) diffMouse(ev vaxis.Mouse) {
	d := a.diff
	pos := [2]int{min(max(d.pg.Offset+ev.Row, 0), max(len(d.rows)-1, 0)), max(ev.Col, 0)}
	switch {
	case ev.Button == vaxis.MouseWheelUp:
		d.pg.Offset = max(d.pg.Offset-3, 0)
	case ev.Button == vaxis.MouseWheelDown:
		d.pg.Offset += 3
	case ev.Button != vaxis.MouseLeftButton:
	case ev.EventType == vaxis.EventPress:
		a.selSeq++
		d.from, d.to = pos, pos
	case ev.EventType == vaxis.EventMotion:
		d.to = pos
	case ev.EventType == vaxis.EventRelease && d.from != d.to:
		d.to = pos
		a.vx.ClipboardPush(trimCopy(d.selection()))
		clear := clearSel{nil, a.selSeq}
		time.AfterFunc(selLinger, func() { a.vx.PostEvent(clear) })
	}
}

// gitDiff returns git's diff of dir, or its error as text.
func gitDiff(dir string) string {
	out, err := exec.Command("git", "-C", dir, "diff", "--no-color").Output()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		return "git diff: " + strings.TrimSpace(string(ee.Stderr))
	case err != nil:
		return "git diff: " + err.Error()
	case len(out) == 0:
		return "no changes"
	}
	return string(out)
}

// diffKind says how a diffLine is drawn.
type diffKind int

const (
	diffText diffKind = iota // anything else git prints, and errors or "no changes"
	diffFile                 // diff --git a/x b/x
	diffHunk                 // @@ -1,2 +1,3 @@
	diffCtx                  // unchanged line
	diffDel
	diffAdd
	diffPair // side by side: old line left, new line right
)

// diffLine is one line to draw. A diffPair has its old and new text; a side
// without a line has number 0. same marks an unchanged line, shown on both sides.
type diffLine struct {
	kind       diffKind
	text       string
	old, new   string
	oldN, newN int
	same       bool
}

var hunkNums = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)`)

// parseDiff splits git's unified diff into lines to draw. Side by side, a run of
// deleted lines is paired with the added lines that follow it, so a changed line
// sits next to its new version; the index, --- and +++ lines are left out.
func parseDiff(out string, side bool) []diffLine {
	var lines, dels, adds []diffLine
	var oldN, newN int
	flush := func() {
		for i := range max(len(dels), len(adds)) {
			l := diffLine{kind: diffPair}
			if i < len(dels) {
				l.old, l.oldN = dels[i].text, dels[i].oldN
			}
			if i < len(adds) {
				l.new, l.newN = adds[i].text, adds[i].newN
			}
			lines = append(lines, l)
		}
		dels, adds = nil, nil
	}
	inHunk := false
	for _, s := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		l := diffLine{kind: diffText, text: s}
		switch {
		case inHunk && strings.HasPrefix(s, "-"):
			l.kind, l.oldN = diffDel, oldN
			oldN++
		case inHunk && strings.HasPrefix(s, "+"):
			l.kind, l.newN = diffAdd, newN
			newN++
		case inHunk && strings.HasPrefix(s, " "):
			l.kind, l.oldN, l.newN = diffCtx, oldN, newN
			oldN, newN = oldN+1, newN+1
		case strings.HasPrefix(s, "diff --git "):
			l.kind, inHunk = diffFile, false
		case strings.HasPrefix(s, "@@ "):
			if m := hunkNums.FindStringSubmatch(s); m != nil {
				oldN, _ = strconv.Atoi(m[1])
				newN, _ = strconv.Atoi(m[2])
				l.kind, inHunk = diffHunk, true
			}
		case side && !inHunk && (strings.HasPrefix(s, "index ") || strings.HasPrefix(s, "--- ") || strings.HasPrefix(s, "+++ ")):
			continue
		}
		if !side {
			lines = append(lines, l)
			continue
		}
		switch l.kind {
		case diffDel:
			if len(adds) > 0 { // a - after a +: the next change starts
				flush()
			}
			l.text = s[1:]
			dels = append(dels, l)
		case diffAdd:
			l.text = s[1:]
			adds = append(adds, l)
		case diffCtx:
			flush()
			lines = append(lines, diffLine{kind: diffPair, old: s[1:], new: s[1:], oldN: l.oldN, newN: l.newN, same: true})
		default:
			flush()
			lines = append(lines, l)
		}
	}
	flush()
	return lines
}

// diffSegments lays lines out w columns wide for the pager. Rows are kept one
// column narrower than w: the pager would add a blank line after a full row.
// Long lines wrap inside their column, or are cut with … when wrap is off.
func diffSegments(lines []diffLine, w int, wrap bool, th theme) []vaxis.Segment {
	fg := func(s vaxis.Style) vaxis.Style { s.Background = 0; return s } // the pane keeps the terminal's background
	base, muted, file := fg(th.base), fg(th.muted), fg(th.project)
	file.Attribute |= vaxis.AttrBold
	style := map[diffKind]vaxis.Style{diffText: base, diffCtx: base, diffFile: file, diffHunk: muted, diffDel: fg(th.err), diffAdd: fg(th.running)}

	w = max(w-1, 3)
	numW, colW := diffCols(lines, w)
	num := func(n int) string {
		if n == 0 {
			return strings.Repeat(" ", numW)
		}
		return strings.Repeat(" ", numW-len(strconv.Itoa(n))) + strconv.Itoa(n)
	}

	var segs []vaxis.Segment
	for _, l := range lines {
		if l.kind != diffPair {
			for _, r := range cut(l.text, w, wrap) {
				segs = append(segs, vaxis.Segment{Text: r, Style: style[l.kind]}, vaxis.Segment{Text: "\n"})
			}
			continue
		}
		oldS, newS := style[diffDel], style[diffAdd]
		if l.same {
			oldS, newS = base, base
		}
		ls, rs := cut(l.old, colW, wrap), cut(l.new, colW, wrap)
		blank := strings.Repeat(" ", colW)
		for i := range max(len(ls), len(rs)) {
			on, nn, lt, rt := num(0), num(0), blank, blank
			if i == 0 {
				on, nn = num(l.oldN), num(l.newN)
			}
			if i < len(ls) && l.oldN > 0 {
				lt = ls[i]
			}
			if i < len(rs) && l.newN > 0 {
				rt = rs[i]
			}
			segs = append(segs,
				vaxis.Segment{Text: on + " ", Style: muted}, vaxis.Segment{Text: lt, Style: oldS},
				vaxis.Segment{Text: "│", Style: muted},
				vaxis.Segment{Text: nn + " ", Style: muted}, vaxis.Segment{Text: rt, Style: newS},
				vaxis.Segment{Text: "\n"})
		}
	}
	return segs
}

// diffCols are the widths of the line numbers and of each side's text in rows
// w columns wide: number, space, text on each side; │ between.
func diffCols(lines []diffLine, w int) (numW, colW int) {
	numW = 1
	for _, l := range lines {
		numW = max(numW, len(strconv.Itoa(max(l.oldN, l.newN))))
	}
	return numW, max((w-1)/2-numW-1, 1)
}

// diffHalves are the columns, first and after last, of the old and the new text
// of side by side rows laid out by diffSegments for a pager w wide.
func diffHalves(lines []diffLine, w int) [2][2]int {
	numW, colW := diffCols(lines, max(w-1, 3))
	left := numW + 1
	right := left + colW + 1 + numW + 1
	return [2][2]int{{left, left + colW}, {right, right + colW}}
}

// cut splits s into rows of exactly w columns, padded with spaces. With wrap off
// it keeps the first row, ending in … when s was longer.
func cut(s string, w int, wrap bool) []string {
	// shortcut: a tab is always 4 spaces, not to the next tab stop; fine for diffs of indented code
	chars := vaxis.Characters(strings.ReplaceAll(s, "\t", "    "))
	var rows []string
	var b strings.Builder
	used := 0
	for _, c := range chars {
		if used+c.Width > w && used > 0 {
			if !wrap {
				return []string{truncRow(chars, w)}
			}
			rows = append(rows, b.String()+strings.Repeat(" ", w-used))
			b.Reset()
			used = 0
		}
		b.WriteString(c.Grapheme)
		used += c.Width
	}
	return append(rows, b.String()+strings.Repeat(" ", max(w-used, 0)))
}

// truncRow fits chars, which overflow w, into w columns ending in ….
func truncRow(chars []vaxis.Character, w int) string {
	var b strings.Builder
	used := 0
	for _, c := range chars {
		if used+c.Width > w-1 {
			break
		}
		b.WriteString(c.Grapheme)
		used += c.Width
	}
	return b.String() + "…" + strings.Repeat(" ", max(w-1-used, 0))
}

// diffKey scrolls the focused diff and s switches unified ↔ side by side; Esc
// or F3 closes it, F11 and Alt+↑ work as in the pane. Other keys are dropped, so
// nothing reaches Claude while the diff has focus. pager.Draw clamps the offset.
func (a *app) diffKey(k vaxis.Key) {
	pg := &a.diff.pg
	page := func() int { _, h := a.paneSize(); return max(1, h-1) }
	switch {
	case k.MatchString("Escape"), k.MatchString(diffKey):
		a.closeDiff()
	case k.MatchString(fullKey):
		a.toggleFull()
	case k.MatchString("Alt+Up") && a.split():
		a.focusUpper()
	case k.MatchString(zoomKey), k.MatchString(zoomAlt):
		a.toggleZoom()
	case k.MatchString(quitKey):
		a.askQuit()
	case k.MatchString("s"):
		a.st.UnifiedDiff = !a.st.UnifiedDiff
		a.saveState()
		a.diff.laid, pg.Offset = 0, 0 // rows of one layout don't match the other's
	case k.MatchString("j"), k.MatchString("Down"):
		pg.Offset++
	case k.MatchString("k"), k.MatchString("Up"):
		pg.Offset = max(pg.Offset-1, 0)
	case k.MatchString("Page_Down"), k.MatchString("Shift+Page_Down"):
		pg.Offset += page()
	case k.MatchString("Page_Up"), k.MatchString("Shift+Page_Up"):
		pg.Offset = max(pg.Offset-page(), 0)
	case k.MatchString("Ctrl+d"):
		pg.Offset += max(1, page()/2)
	case k.MatchString("Ctrl+u"):
		pg.Offset = max(pg.Offset-max(1, page()/2), 0)
	case k.MatchString("g"), k.MatchString("Home"):
		pg.Offset = 0
	case k.MatchString("G"), k.MatchString("End"):
		pg.Offset = math.MaxInt
	}
}
