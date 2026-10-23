package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/term"
)

const (
	appName  = "cade"
	version  = "0.1.0"
	sideW    = 40
	barW     = 2             // scrollbar columns
	focusKey = "Ctrl+Escape" // shortcut: hardcoded, make configurable once the key choice settles
	focusLbl = "Ctrl+Esc"
	// Ctrl+Esc needs the kitty keyboard protocol; legacy terminals send plain Esc, so F12 stays as fallback.
	focusAlt  = "F12"
	newRowLbl = "[n] new session"
)

// row is one line of the sidebar tree: a project header, its new-session row, or a session.
type row struct {
	p int // index into app.projects
	s int // rowHeader, rowNew, or index into the project's sessions
}

const (
	rowHeader = -2
	rowNew    = -1
)

// rowKey identifies a row across reloads and reorders.
type rowKey struct {
	dir, id string
	s       int
}

// prompt is the add-project form; step 0 asks the name, step 1 the directory.
type prompt struct {
	step      int
	name, dir string
	err       string
}

type refreshEvent struct{}

// dTimeout fires when no second d followed the first, so the single-d action runs.
type dTimeout struct{ seq int }

// clearSel fires a moment after a copy; seq drops it if the user clicked again since.
type clearSel struct {
	t   *term.Model
	seq int
}

const selLinger = 600 * time.Millisecond

// confirmation is a pending yes/no question; yes runs on y.
type confirmation struct {
	lines []string
	yes   func()
}

type app struct {
	vx       *vaxis.Vaxis
	home     string
	cwd      string
	cfgPath  string
	projects []*project
	input    *prompt       // non-nil while adding a project
	confirm  *confirmation // non-nil while asking yes/no
	help     bool          // shortcut dialog open
	visible  int           // tree rows on screen, set by draw
	st       state         // remembered UI state
	dWait    bool          // a first d was pressed; a second one within 400ms means dd
	dSeq     int
	selSeq   int    // bumped on every pane click, see clearSel
	msg      string // last error shown above the hints
	th       theme
	pending  map[string]bool // started from cade, jsonl may not exist yet
	cursor   int             // index into a.rows()
	top      int             // first visible tree row, keeps cursor on screen
	terms    map[string]*term.Model
	active   string // key into terms
	inTerm   bool   // keyboard focus on the Claude pane
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println(appName, version)
		return
	}
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	if _, err := exec.LookPath("claude"); err != nil {
		fmt.Fprintln(os.Stderr, "cade: claude not found in PATH")
		os.Exit(1)
	}

	vx, err := vaxis.New(vaxis.Options{
		CSIuBitMask: vaxis.CSIuDisambiguate | vaxis.CSIuReportEvents |
			vaxis.CSIuAlternateKeys | vaxis.CSIuAllKeys | vaxis.CSIuAssociatedText,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "cade:", err)
		os.Exit(1)
	}
	cfgPath := projectsPath()
	projects, err := loadProjects(cfgPath)
	if err != nil {
		vx.Close()
		fmt.Fprintf(os.Stderr, "cade: %s: %v\n", cfgPath, err)
		os.Exit(1)
	}
	if len(projects) == 0 {
		projects = []*project{{Name: filepath.Base(cwd), Dir: cwd}}
	}
	a := &app{vx: vx, home: home, cwd: cwd, cfgPath: cfgPath, projects: projects,
		pending: map[string]bool{}, terms: map[string]*term.Model{}}
	var themeErr error
	if a.th, themeErr = loadTheme(configPath()); themeErr != nil {
		a.msg = themeErr.Error()
	}
	a.st = loadState(statePath())
	a.refresh()
	if i := slices.IndexFunc(a.rows(), func(r row) bool { return r.s == rowHeader && a.projects[r.p].Dir == cwd }); i >= 0 {
		a.cursor = i
	}
	go func() {
		for range time.Tick(2 * time.Second) {
			vx.PostEvent(refreshEvent{})
		}
	}()
	defer func() {
		for _, t := range a.terms {
			t.Close()
		}
		vx.Close()
	}()
	a.run()
}

func (a *app) run() {
	for ev := range a.vx.Events() {
		if quit := a.handle(ev); quit {
			return
		}
		a.draw()
	}
}

func (a *app) handle(ev vaxis.Event) bool {
	t := a.terms[a.active]
	switch ev := ev.(type) {
	case vaxis.Key:
		if ev.MatchString(focusKey) || ev.MatchString(focusAlt) {
			if ev.EventType != vaxis.EventRelease && t != nil {
				a.setFocus(!a.inTerm)
			}
			return false
		}
		if a.inTerm && t != nil {
			if !a.pageScroll(t, ev) {
				t.Update(ev)
			}
			return false
		}
		if ev.EventType == vaxis.EventRelease {
			return false
		}
		if a.help {
			a.help = false // any key closes it
			return false
		}
		if a.confirm != nil {
			a.confirmKey(ev)
			return false
		}
		if a.input != nil {
			a.promptKey(ev)
			return false
		}
		return a.sidebarKey(ev)
	case vaxis.PasteStartEvent, vaxis.PasteEndEvent, vaxis.FocusIn, vaxis.FocusOut, vaxis.ColorThemeUpdate:
		if a.inTerm && t != nil {
			t.Update(ev)
		}
	case vaxis.Mouse:
		if w, h := a.vx.Window().Size(); t != nil && ev.Col >= w-barW {
			if ev.Button == vaxis.MouseLeftButton && ev.EventType != vaxis.EventRelease {
				_, m := t.ScrollInfo()
				t.ScrollTo(offsetAt(ev.Row-1, h-1, m)) // -1: title bar
			} else {
				t.Update(ev) // wheel over the bar still scrolls
			}
			return false
		}
		if ev.Col < sideW+1 {
			rows := a.rows()
			overflow := len(rows) > a.visible
			switch {
			case ev.Button == vaxis.MouseWheelUp:
				a.scrollTree(a.top - 3)
				return false
			case ev.Button == vaxis.MouseWheelDown:
				a.scrollTree(a.top + 3)
				return false
			case overflow && ev.Col == sideW-1 && ev.Row >= 1 && ev.Row-1 < a.visible &&
				ev.Button == vaxis.MouseLeftButton && ev.EventType != vaxis.EventRelease:
				m := len(rows) - a.visible
				a.scrollTree(m - offsetAt(ev.Row-1, a.visible, m))
				return false
			}
			if ev.Button == vaxis.MouseLeftButton && ev.EventType == vaxis.EventPress {
				if rows := a.rows(); ev.Row >= 1 && ev.Row-1+a.top < len(rows) {
					a.cursor = ev.Row - 1 + a.top // -1: title bar
					if rows[a.cursor].s != rowHeader {
						a.open()
					}
				}
			}
			return false
		}
		if t != nil {
			if ev.EventType == vaxis.EventPress && !a.inTerm {
				a.setFocus(true)
			}
			ev.Col -= sideW + 1
			ev.Row-- // title bar
			if ev.EventType == vaxis.EventPress {
				a.selSeq++
			}
			t.Update(ev)
			// cade captures the mouse, so the host terminal's copy-on-select can't run; do it here.
			if ev.Button == vaxis.MouseLeftButton && ev.EventType == vaxis.EventRelease && t.HasSelection() {
				a.vx.ClipboardPush(t.Selection())
				ev := clearSel{t, a.selSeq}
				time.AfterFunc(selLinger, func() { a.vx.PostEvent(ev) })
			}
		}
	case refreshEvent:
		a.refresh()
	case clearSel:
		if ev.seq == a.selSeq {
			ev.t.ClearSelection()
		}
	case dTimeout:
		if a.dWait && ev.seq == a.dSeq {
			a.dWait = false
			a.confirmSession()
		}
	case term.EventClosed:
		for k, v := range a.terms {
			if v == ev.Term {
				v.Close()
				delete(a.terms, k)
				delete(a.pending, k)
				if k == a.active {
					a.active, a.inTerm = "", false
				}
			}
		}
	case term.EventTitle:
		a.vx.SetTitle(string(ev))
	case term.EventNotify:
		a.vx.Notify(ev.Title, ev.Body)
	case term.EventMouseShape:
		a.vx.SetMouseShape(ev.Shape)
	}
	return false
}

// pageScroll scrolls the pane's scrollback on PgUp/PgDn (with or without Shift),
// acting on press and repeat only. Without scrollback (e.g. Claude fullscreen)
// the key is left for Claude.
func (a *app) pageScroll(t *term.Model, k vaxis.Key) bool {
	up, down := k.Matches(vaxis.KeyPgUp), k.Matches(vaxis.KeyPgDown)
	up = up || k.Matches(vaxis.KeyPgUp, vaxis.ModShift)
	down = down || k.Matches(vaxis.KeyPgDown, vaxis.ModShift)
	off, m := t.ScrollInfo()
	if !(up || down) || m == 0 {
		return false
	}
	if k.EventType != vaxis.EventRelease {
		_, h := a.paneSize()
		page := max(1, h-1)
		if down {
			page = -page
		}
		t.ScrollTo(off + page)
	}
	return true
}

func (a *app) sidebarKey(k vaxis.Key) bool {
	rows := a.rows()
	a.msg = "" // errors last until the next key
	if k.MatchString("d") {
		if a.dWait {
			a.dWait = false
			a.confirmProject()
		} else {
			a.dWait = true
			a.dSeq++
			seq := a.dSeq
			time.AfterFunc(400*time.Millisecond, func() { a.vx.PostEvent(dTimeout{seq}) })
		}
		return false
	}
	a.dWait = false
	switch {
	case k.MatchString("Ctrl+c"), k.MatchString("q"):
		return true
	case k.Matches(vaxis.KeyUp, vaxis.ModShift):
		a.cursor = a.header(-1)
	case k.Matches(vaxis.KeyDown, vaxis.ModShift):
		a.cursor = a.header(1)
	case k.Matches(vaxis.KeyPgUp, vaxis.ModShift):
		a.moveProject(-1)
	case k.Matches(vaxis.KeyPgDown, vaxis.ModShift):
		a.moveProject(1)
	case k.MatchString("Left"):
		a.fold(true)
	case k.MatchString("Right"):
		a.fold(false)
	case k.MatchString("j"), k.MatchString("Down"):
		a.cursor = min(a.cursor+1, len(rows)-1)
	case k.MatchString("k"), k.MatchString("Up"):
		a.cursor = max(a.cursor-1, 0)
	case k.MatchString("g"), k.MatchString("Home"):
		a.cursor = 0
	case k.MatchString("G"), k.MatchString("End"):
		a.cursor = len(rows) - 1
	case k.MatchString("Enter"), k.MatchString("l"):
		a.open()
	case k.MatchString("n"):
		p := rows[a.cursor].p
		a.projects[p].Collapsed = false
		a.cursor = slices.Index(a.rows(), row{p, rowNew})
		a.open()
	case k.MatchString("a"):
		a.input = &prompt{}
	case k.MatchString("?"):
		a.help = true
	case k.MatchString("H"):
		a.st.HideHints = !a.st.HideHints
		if err := writeJSON(statePath(), a.st); err != nil {
			a.msg = "save failed: " + err.Error()
		}
	case k.MatchString("Escape"), k.MatchString("Tab"):
		if a.terms[a.active] != nil {
			a.setFocus(true)
		}
	}
	return false
}

func (a *app) confirmKey(k vaxis.Key) {
	switch {
	case k.MatchString("y"):
		yes := a.confirm.yes
		a.confirm = nil
		yes()
	case k.MatchString("n"), k.MatchString("Escape"):
		a.confirm = nil
	}
}

// confirmSession asks to delete the session under the cursor from disk.
func (a *app) confirmSession() {
	cur := a.rows()[a.cursor]
	if cur.s < 0 {
		return
	}
	p := a.projects[cur.p]
	s := p.sessions[cur.s]
	a.confirm = &confirmation{
		lines: []string{"Delete session from disk? Can't undo.", fmt.Sprintf("%s (%s)", s.Title, s.ID[:min(8, len(s.ID))])},
		yes:   func() { a.deleteSession(p, s.ID) },
	}
}

func (a *app) deleteSession(p *project, id string) {
	a.closeTerm(id)
	delete(a.pending, id)
	base := filepath.Join(projectDir(a.home, p.Dir), id)
	if err := os.Remove(base + ".jsonl"); err != nil && !os.IsNotExist(err) {
		a.msg = "delete failed: " + err.Error()
		return
	}
	os.RemoveAll(base) // per-session tool-results dir, if any
	p.sessions = slices.DeleteFunc(p.sessions, func(s session) bool { return s.ID == id })
	a.restore(rowKey{})
}

// confirmProject asks to remove the cursor's project from cade's list.
func (a *app) confirmProject() {
	p := a.projects[a.rows()[a.cursor].p]
	if len(a.projects) == 1 {
		a.msg = "can't remove the last project"
		return
	}
	a.confirm = &confirmation{
		lines: []string{"Remove project from cade's list?", p.Name + "  " + a.short(p.Dir), "Folder and Claude sessions are kept."},
		yes: func() {
			for _, s := range p.sessions {
				a.closeTerm(s.ID)
			}
			a.projects = slices.DeleteFunc(a.projects, func(q *project) bool { return q == p })
			a.save()
			a.restore(rowKey{})
		},
	}
}

func (a *app) closeTerm(id string) {
	if t := a.terms[id]; t != nil {
		t.Close()
		delete(a.terms, id)
		if a.active == id {
			a.active, a.inTerm = "", false
		}
	}
}

// promptKey edits the add-project form.
func (a *app) promptKey(k vaxis.Key) {
	in := a.input
	field := &in.name
	if in.step == 1 {
		field = &in.dir
	}
	switch {
	case k.MatchString("Escape"):
		a.input = nil
	case k.MatchString("BackSpace"):
		if r := []rune(*field); len(r) > 0 {
			*field = string(r[:len(r)-1])
		}
	case k.MatchString("Enter"):
		if in.step == 0 {
			in.step, in.dir = 1, a.cwd
			return
		}
		dir, err := expandDir(in.dir, a.home)
		if err == nil && slices.ContainsFunc(a.projects, func(p *project) bool { return p.Dir == dir }) {
			err = fmt.Errorf("already added")
		}
		if err != nil {
			in.err = err.Error()
			return
		}
		name := strings.TrimSpace(in.name)
		if name == "" {
			name = filepath.Base(dir)
		}
		a.projects = append(a.projects, &project{Name: name, Dir: dir})
		a.input = nil
		a.save()
		a.refresh()
		a.cursor = slices.Index(a.rows(), row{len(a.projects) - 1, rowHeader})
	case k.Text != "" && k.Modifiers&(vaxis.ModCtrl|vaxis.ModAlt|vaxis.ModSuper) == 0:
		*field += k.Text
		in.err = ""
	}
}

// rows flattens projects and their sessions into the sidebar tree.
func (a *app) rows() []row {
	var rows []row
	for pi, p := range a.projects {
		rows = append(rows, row{pi, rowHeader})
		if p.Collapsed {
			continue
		}
		rows = append(rows, row{pi, rowNew})
		for si := range p.sessions {
			rows = append(rows, row{pi, si})
		}
	}
	return rows
}

func (a *app) keyOf(r row) rowKey {
	p := a.projects[r.p]
	k := rowKey{dir: p.Dir, s: r.s}
	if r.s >= 0 {
		k.id, k.s = p.sessions[r.s].ID, 0
	}
	return k
}

// restore puts the cursor back on the row identified by k, if it still exists.
func (a *app) restore(k rowKey) {
	rows := a.rows()
	if i := slices.IndexFunc(rows, func(r row) bool { return a.keyOf(r) == k }); i >= 0 {
		a.cursor = i
	}
	a.cursor = max(min(a.cursor, len(rows)-1), 0)
}

// header returns the row of the previous (dir<0) or next (dir>0) project header.
func (a *app) header(dir int) int {
	rows := a.rows()
	for i := a.cursor + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].s == rowHeader {
			return i
		}
	}
	return a.cursor
}

// fold collapses (Left) or expands (Right) the cursor's project. Left on a
// session row first jumps to its project header.
func (a *app) fold(collapse bool) {
	cur := a.rows()[a.cursor]
	p := a.projects[cur.p]
	if collapse && cur.s != rowHeader {
		a.restore(rowKey{dir: p.Dir, s: rowHeader})
		return
	}
	if p.Collapsed == collapse {
		return
	}
	p.Collapsed = collapse
	a.save()
	a.restore(rowKey{dir: p.Dir, s: rowHeader})
}

// scrollTree scrolls the sidebar to top and pulls the cursor into view, so
// draw's keep-cursor-visible rule doesn't snap the scroll back.
func (a *app) scrollTree(top int) {
	m := max(len(a.rows())-a.visible, 0)
	a.top = max(min(top, m), 0)
	a.cursor = max(min(a.cursor, a.top+a.visible-1), a.top)
}

// moveProject swaps the cursor's project with its neighbour and saves the order.
func (a *app) moveProject(dir int) {
	cur := a.rows()[a.cursor]
	j := cur.p + dir
	if j < 0 || j >= len(a.projects) {
		return
	}
	k := a.keyOf(cur)
	a.projects[cur.p], a.projects[j] = a.projects[j], a.projects[cur.p]
	a.save()
	a.restore(k)
}

func (a *app) save() {
	a.msg = ""
	if err := saveProjects(a.cfgPath, a.projects); err != nil {
		a.msg = "save failed: " + err.Error()
	}
}

// open switches to the selected session's terminal, starting claude if needed.
// On a header or new-session row it starts a new session in that project.
func (a *app) open() {
	cur := a.rows()[a.cursor]
	p := a.projects[cur.p]
	var key string
	var args []string
	if cur.s < 0 {
		key = newUUID()
		args = []string{"--session-id", key}
		a.pending[key] = true
		p.sessions = append([]session{{ID: key, Title: "new session"}}, p.sessions...)
		a.restore(rowKey{dir: p.Dir, id: key})
	} else {
		key = p.sessions[cur.s].ID
		args = []string{"--resume", key}
	}
	if _, ok := a.terms[key]; !ok {
		t := term.New(term.WithVaxis(a.vx))
		t.Attach(a.vx.PostEvent)
		cmd := exec.Command("claude", args...)
		cmd.Dir = p.Dir
		w, h := a.paneSize()
		if err := t.StartWithSize(cmd, w, h); err != nil {
			a.msg = "claude: " + err.Error()
			return
		}
		a.terms[key] = t
	}
	if cur := a.terms[a.active]; cur != nil && a.active != key {
		cur.Blur()
	}
	a.active = key
	a.setFocus(true)
}

// refresh reloads every project's sessions from disk and keeps the cursor on the same row.
func (a *app) refresh() {
	var k rowKey
	if rows := a.rows(); a.cursor < len(rows) {
		k = a.keyOf(rows[a.cursor])
	}
	for _, p := range a.projects {
		p.sessions = merge(p.sessions, loadSessions(a.home, p.Dir), a.pending)
	}
	a.restore(k)
}

// merge keeps the shown order stable (resuming a session bumps its mtime, which
// would otherwise move it to the top): sessions new on disk go first, known ones
// stay in place with fresh titles, cade-started ones not yet on disk are kept.
func merge(shown, loaded []session, pending map[string]bool) []session {
	byID := map[string]session{}
	for _, l := range loaded {
		byID[l.ID] = l
	}
	known := map[string]bool{}
	var kept []session
	for _, s := range shown {
		known[s.ID] = true
		if l, ok := byID[s.ID]; ok {
			kept = append(kept, l)
		} else if pending[s.ID] {
			kept = append(kept, s)
		}
	}
	var fresh []session
	for _, l := range loaded {
		if !known[l.ID] {
			fresh = append(fresh, l)
		}
	}
	return append(fresh, kept...)
}

func (a *app) setFocus(inTerm bool) {
	a.inTerm = inTerm
	if t := a.terms[a.active]; t != nil {
		if inTerm {
			t.Focus()
		} else {
			t.Blur()
		}
	}
}

func (a *app) paneSize() (int, int) {
	w, h := a.vx.Window().Size()
	return max(w-sideW-1-barW, 1), max(h-1, 1) // separator, scrollbar, title bar
}

// thumb returns the scrollbar thumb's top row and size for a viewport of h rows
// scrolled offset rows up from the bottom of m scrollback rows.
func thumb(h, offset, m int) (top, size int) {
	if m == 0 {
		return 0, h
	}
	size = max(1, h*h/(h+m))
	return (m - offset) * (h - size) / m, size
}

// offsetAt maps a clicked scrollbar row back to a scroll offset.
func offsetAt(row, h, m int) int {
	if h <= 1 {
		return 0
	}
	return m - row*m/(h-1)
}

// short abbreviates the home directory to ~.
func (a *app) short(dir string) string {
	if rest, ok := strings.CutPrefix(dir, a.home); ok && a.home != "" {
		return "~" + rest
	}
	return dir
}

// promptLines renders the add-project form; long input shows its tail.
func (a *app) promptLines() []string {
	in := a.input
	tail := func(s string) string {
		if r := []rune(s); len(r) > sideW-10 {
			return "…" + string(r[len(r)-(sideW-11):])
		}
		return s
	}
	lines := []string{"Add project  [⏎] next  [Esc] cancel", "Name: " + tail(in.name)}
	if in.step == 0 {
		lines[1] += "▏"
	} else {
		lines[0] = "Add project  [⏎] save  [Esc] cancel"
		lines = append(lines, "Dir:  "+tail(in.dir)+"▏")
	}
	if in.err != "" {
		lines = append(lines, "✗ "+in.err)
	}
	return lines
}

// grid lays out key/description pairs in two aligned columns of [key] desc,
// each column's keys padded to that column's widest key.
func grid(pairs [][2]string, width int) []string {
	colW := (width - 1) / 2
	var keyW [2]int
	for i, p := range pairs {
		keyW[i%2] = max(keyW[i%2], len([]rune(p[0]))+2)
	}
	var lines []string
	for i := 0; i < len(pairs); i += 2 {
		line := ""
		for c := 0; c < 2 && i+c < len(pairs); c++ {
			p := pairs[i+c]
			cell := pad("["+p[0]+"]", keyW[c]) + " " + p[1]
			if r := []rune(cell); len(r) > colW {
				cell = string(r[:colW-1]) + "…"
			}
			if c == 0 {
				cell = pad(cell, colW) + " "
			}
			line += cell
		}
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines
}

// hintSegs colors the [key] parts of a hint line.
func (a *app) hintSegs(line string) []vaxis.Segment {
	segs := []vaxis.Segment{{Text: " ", Style: a.th.muted}}
	for line != "" {
		i := strings.IndexByte(line, '[')
		j := strings.IndexByte(line, ']')
		if i < 0 || j < i {
			segs = append(segs, vaxis.Segment{Text: line, Style: a.th.muted})
			break
		}
		segs = append(segs,
			vaxis.Segment{Text: line[:i], Style: a.th.muted},
			vaxis.Segment{Text: line[i : j+1], Style: a.th.key})
		line = line[j+1:]
	}
	return segs
}

// pad fills s with spaces to width w so a row's background spans the panel.
func pad(s string, w int) string {
	if n := w - len([]rune(s)); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// titleBar draws a panel's title row: text on the left, right (if any) right-aligned and not bold.
func (a *app) titleBar(win vaxis.Window, text, right string, focused bool) {
	style := a.th.titleBlur
	if focused {
		style = a.th.title
	}
	plain := style
	plain.Attribute &^= vaxis.AttrBold
	w, _ := win.Size()
	win.PrintTruncate(0,
		vaxis.Segment{Text: pad(text, w-len([]rune(right))), Style: style},
		vaxis.Segment{Text: right, Style: plain})
}

func (a *app) draw() {
	win := a.vx.Window()
	win.Clear()
	a.vx.HideCursor()
	w, h := win.Size()
	th := a.th

	side := win.New(0, 0, sideW, h)
	side.Fill(vaxis.Cell{Character: vaxis.Character{Grapheme: " ", Width: 1}, Style: th.base})
	a.titleBar(side, " SESSIONS", appName+" v"+version+" ", !a.inTerm)
	tree := side.New(0, 1, sideW, h-1)
	_, th1 := tree.Size()

	var hints []string
	switch {
	case a.confirm != nil:
		hints = append(slices.Clone(a.confirm.lines), "[y] yes  [n] no")
	case a.input != nil:
		hints = a.promptLines()
	case a.inTerm:
		hints = grid([][2]string{{focusLbl, "sidebar"}, {"⇧PgUp/Dn", "scroll"}}, sideW-1)
	case a.st.HideHints:
		hints = grid([][2]string{{"?", "help"}, {"H", "show hints"}}, sideW-1)
	default:
		pane := [2]string{"g/G", "top/end"}
		if a.terms[a.active] != nil {
			pane = [2]string{"⇥", "pane"}
		}
		hints = grid([][2]string{
			{"j/k", "move"}, {"⇧↑/⇧↓", "project"},
			{"l/⏎", "open"}, {"n", "new session"},
			{"←/→", "fold"}, {"a", "add project"},
			{"⇧PgUp/Dn", "reorder"}, {"d/dd", "delete"},
			pane, {"?", "help"},
			{"q", "quit"}, {"H", "hide hints"},
		}, sideW-1)
	}
	visible := max(th1-len(hints)-1, 1)
	if a.msg != "" {
		visible = max(visible-1, 1)
	}
	a.top = min(a.top, a.cursor)
	a.top = max(a.top, a.cursor-visible+1)
	a.visible = visible
	all := a.rows()
	overflow := len(all) > visible
	a.top = min(a.top, max(len(all)-visible, 0)) // no empty space below the last row
	cw := sideW                                  // row content width
	if overflow {
		cw = sideW - 1 // last column is the scrollbar
	}

	for i, r := range all {
		if i < a.top || i >= a.top+visible {
			continue
		}
		p := a.projects[r.p]
		selected := i == a.cursor
		hl := func(s vaxis.Style) vaxis.Style {
			if !selected {
				return s
			}
			sel := th.sel
			if a.inTerm {
				sel = th.selBlur
			}
			if sel.Foreground == 0 { // terminal theme: keep the slot's color under reverse video
				sel.Foreground = s.Foreground
			}
			return sel
		}
		if r.s == rowHeader {
			name := vaxis.Style{Foreground: th.project.Foreground, Background: th.project.Background, Attribute: vaxis.AttrBold}
			dir := "  " + a.short(p.Dir)
			tree.PrintTruncate(i-a.top,
				vaxis.Segment{Text: map[bool]string{false: " ▾ ", true: " ▸ "}[p.Collapsed] + p.Name, Style: hl(name)},
				vaxis.Segment{Text: pad(dir, cw-3-len([]rune(p.Name))), Style: hl(th.muted)})
			continue
		}
		branch := "   ├─ "
		if r.s == len(p.sessions)-1 || (r.s == rowNew && len(p.sessions) == 0) {
			branch = "   └─ "
		}
		label, id, mark, labelStyle := newRowLbl, "", "  ", th.key
		if r.s >= 0 {
			s := p.sessions[r.s]
			label, id, labelStyle = s.Title, " ("+s.ID[:min(8, len(s.ID))]+")", th.base
			if a.terms[s.ID] != nil {
				mark = "● "
			}
		}
		text := label
		// shortcut: rune count, not display width; wide (CJK/emoji) titles may still push the id off
		room := cw - len([]rune(branch+mark)) - len([]rune(id))
		if len([]rune(text)) > room {
			text = string([]rune(text)[:max(room-1, 0)]) + "…"
		}
		tree.PrintTruncate(i-a.top,
			vaxis.Segment{Text: branch, Style: hl(th.muted)},
			vaxis.Segment{Text: mark, Style: hl(th.running)},
			vaxis.Segment{Text: text, Style: hl(labelStyle)},
			vaxis.Segment{Text: pad(id, room-len([]rune(text))+len([]rune(id))), Style: hl(th.muted)})
	}
	if overflow {
		m := len(all) - visible
		top, size := thumb(visible, m-a.top, m)
		for r := range visible {
			g, style := "░", th.muted
			if r >= top && r < top+size {
				g, style = "█", th.thumb
			}
			tree.SetCell(sideW-1, r, vaxis.Cell{Character: vaxis.Character{Grapheme: g, Width: 1}, Style: style})
		}
	}
	row := th1 - len(hints)
	divider := row - 1
	if a.msg != "" {
		tree.PrintTruncate(row-1, vaxis.Segment{Text: " " + a.msg, Style: th.err})
		divider--
	}
	tree.PrintTruncate(divider, vaxis.Segment{Text: " " + strings.Repeat("─", sideW-2), Style: th.muted})
	for i, hint := range hints {
		tree.PrintTruncate(row+i, a.hintSegs(hint)...)
	}

	for r := range h {
		win.SetCell(sideW, r, vaxis.Cell{Character: vaxis.Character{Grapheme: "│", Width: 1}, Style: th.muted})
	}

	right := win.New(sideW+1, 0, w-sideW-1, 1)
	t := a.terms[a.active]
	if t == nil {
		a.titleBar(right, " CLAUDE", "", false)
		lines := []string{"[⏎] open selected session", "[n] new session", "[?] all shortcuts"}
		bw := len([]rune(lines[0]))
		pw, ph := w-sideW-1, h-1
		block := win.New(sideW+1+max((pw-bw)/2, 0), 1+max((ph-len(lines))/2, 0), min(bw+1, pw), len(lines))
		for i, l := range lines {
			block.PrintTruncate(i, a.hintSegs(l)[1:]...) // [1:] drops hintSegs' leading pad
		}
		a.drawHelp(win)
		a.vx.Render()
		return
	}
	a.titleBar(right, " "+a.activeTitle(), "", a.inTerm)
	pw, ph := a.paneSize()
	t.Resize(pw, ph)
	t.Draw(win.New(sideW+1, 1, pw, ph))
	if off, m := t.ScrollInfo(); m > 0 {
		top, size := thumb(ph, off, m)
		for r := range ph {
			g, style := "░", th.muted
			if r >= top && r < top+size {
				g, style = "█", th.thumb
			}
			for c := w - barW; c < w; c++ {
				win.SetCell(c, r+1, vaxis.Cell{Character: vaxis.Character{Grapheme: g, Width: 1}, Style: style})
			}
		}
	}
	if !a.inTerm {
		a.vx.HideCursor()
	}
	a.drawHelp(win)
	a.vx.Render()
}

// helpKeys lists every shortcut for the ? dialog; a "" key starts a section.
var helpKeys = [][2]string{
	{"", "Sidebar"},
	{"j/k  ↑/↓", "move"},
	{"g/G  Home/End", "top / end"},
	{"⇧↑/⇧↓", "previous / next project"},
	{"←/→", "collapse / expand project"},
	{"l/⏎  click", "open session (new one on project row)"},
	{"n", "new session in project"},
	{"a", "add project"},
	{"⇧PgUp/⇧PgDn", "move project up / down"},
	{"d", "delete session from disk (asks)"},
	{"dd", "remove project from list (asks)"},
	{"⇥/Esc", "go to Claude pane"},
	{"?", "this help"},
	{"H", "show / hide the hints below the list"},
	{"q  Ctrl+c", "quit"},
	{"", "Claude pane"},
	{focusLbl + "  F12", "switch sidebar ↔ pane"},
	{"PgUp/PgDn  ⇧PgUp/⇧PgDn", "scroll back / forward"},
	{"wheel  scrollbar drag", "scroll"},
	{"drag  2×click  3×click", "select text, word, line → copy"},
	{"anything else", "sent to Claude as-is"},
}

// drawHelp overlays the shortcut dialog in the middle of the screen.
func (a *app) drawHelp(win vaxis.Window) {
	if !a.help {
		return
	}
	a.vx.HideCursor()
	th := a.th
	const keyW = 24
	bw := 2 + keyW + 40
	bh := len(helpKeys) + 4
	w, h := win.Size()
	bw, bh = min(bw, w), min(bh, h)
	box := win.New((w-bw)/2, (h-bh)/2, bw, bh)
	panel := th.base
	if panel.Background == 0 {
		panel = th.sel // terminal theme: reverse video so the dialog stands out
	}
	box.Fill(vaxis.Cell{Character: vaxis.Character{Grapheme: " ", Width: 1}, Style: panel})
	a.titleBar(box, " SHORTCUTS", appName+" v"+version+" ", true)
	for i, k := range helpKeys {
		if i+2 >= bh-1 {
			break
		}
		if k[0] == "" {
			box.PrintTruncate(i+2, vaxis.Segment{Text: " " + k[1], Style: vaxis.Style{Foreground: th.project.Foreground, Background: panel.Background, Attribute: vaxis.AttrBold}})
			continue
		}
		box.PrintTruncate(i+2,
			vaxis.Segment{Text: "   " + pad(k[0], keyW-1), Style: vaxis.Style{Foreground: th.key.Foreground, Background: panel.Background}},
			vaxis.Segment{Text: k[1], Style: vaxis.Style{Foreground: th.base.Foreground, Background: panel.Background}})
	}
	box.PrintTruncate(bh-1, vaxis.Segment{Text: pad(" any key to close", bw), Style: th.muted})
}

// activeTitle names the open session for the pane's title bar.
func (a *app) activeTitle() string {
	for _, p := range a.projects {
		for _, s := range p.sessions {
			if s.ID == a.active {
				return p.Name + " › " + s.Title + " (" + s.ID[:min(8, len(s.ID))] + ")"
			}
		}
	}
	return a.active
}
