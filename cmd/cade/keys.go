package main

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/term"
)

// binding is one shortcut: the keys that trigger it, what it does, and how it
// is shown. The tables below are the single source for key handling, the
// sidebar hints, the ? dialog and the empty-pane prompt.
type binding struct {
	keys   []string        // vaxis MatchString names; none = documentation only
	label  string          // shown as [label]; "" folds into the previous binding's entry
	hint   string          // sidebar hint text; "" keeps it out of the hint grid
	help   string          // ? dialog text
	idle   string          // empty-pane prompt text, if any
	always bool            // hint stays visible when hints are hidden
	show   func(*app) bool // hint only when true (nil = always)
	when   func(*app) bool // binding active only when true (nil = always)
	run    func(*app)
}

func hasPane(a *app) bool { return a.terms[a.active] != nil }
func noPane(a *app) bool  { return !hasPane(a) }

var sidebarKeys = []binding{
	{label: "j/k", keys: []string{"j", "Down"}, hint: "move", help: "move down / up (also ↓/↑)",
		run: func(a *app) { a.cursor = min(a.cursor+1, len(a.rows())-1) }},
	{keys: []string{"k", "Up"}, run: func(a *app) { a.cursor = max(a.cursor-1, 0) }},
	{label: "⇧↑/⇧↓", keys: []string{"Shift+Up"}, hint: "project", help: "previous / next project",
		run: func(a *app) { a.cursor = a.header(-1) }},
	{keys: []string{"Shift+Down"}, run: func(a *app) { a.cursor = a.header(1) }},
	{label: "l/⏎", keys: []string{"Enter", "l"}, hint: "open", help: "open session (click works too; new one on a project row)",
		idle: "open selected session", run: (*app).open},
	{label: "n", keys: []string{"n"}, hint: "new session", help: "new session in this project", idle: "new session",
		run: (*app).newSession},
	{label: "/", keys: []string{"/"}, hint: "search", help: "fuzzy search session titles, all projects",
		run: func(a *app) { a.search = &searching{} }},
	{label: "←/→", keys: []string{"Left"}, hint: "fold", help: "collapse / expand project (→ on a session opens it)",
		run: func(a *app) { a.foldSaved(true) }},
	{keys: []string{"Right"}, run: func(a *app) {
		if a.current().s >= 0 {
			a.open()
		} else {
			a.foldSaved(false)
		}
	}},
	{label: "a", keys: []string{"a"}, hint: "add project", help: "add project (name, then directory; ⇥ opens a match)",
		run: (*app).openAdd},
	{label: "⇧PgUp/Dn", keys: []string{"Shift+Page_Up"}, hint: "reorder", help: "move project up / down",
		run: func(a *app) { a.moveSaved(-1) }},
	{keys: []string{"Shift+Page_Down"}, run: func(a *app) { a.moveSaved(1) }},
	{label: "r", keys: []string{"r"}, hint: "rename", help: "rename session (empty = Claude's title)",
		run: (*app).openRename},
	{label: "s/*", keys: []string{"s", "*"}, hint: "star", help: "star / unstar session; starred go first (click ★ too)",
		run: (*app).toggleStar},
	{label: "d/dd", keys: []string{"d"}, hint: "delete",
		help: "d: delete session from disk · dd: remove project from list (both ask)", run: (*app).pressD},
	{label: "g/G", keys: []string{"g", "Home"}, hint: "top/end", show: noPane, help: "top / end",
		run: func(a *app) { a.cursor = 0 }},
	{keys: []string{"G", "End"}, run: func(a *app) { a.cursor = len(a.rows()) - 1 }},
	{label: "⇥", keys: []string{"Tab"}, hint: "pane", show: hasPane, when: hasPane, help: "go to Claude pane",
		run: func(a *app) { a.setFocus(true) }},
	{label: ",", keys: []string{","}, hint: "settings", help: "settings: theme, hints, sidebar side, edit config.yaml in $EDITOR",
		run: func(a *app) { a.set = &settings{} }},
	{label: "?", keys: []string{"?"}, hint: "help", always: true, help: "this help", idle: "all shortcuts",
		run: func(a *app) { a.help = true }},
	{label: "q", keys: []string{"q", "Ctrl+c"}, hint: "quit", help: "quit (asks while sessions run)", run: (*app).askQuit},
	{label: "H", keys: []string{"H"}, hint: "toggle hints", always: true, help: "show / hide these hints",
		run: (*app).toggleHints},
}

// paneKeys apply while the Claude pane has focus; everything else goes to Claude.
// The focus, diff, shell and waiting keys are checked first, in any focus state.
var paneKeys = []binding{
	{label: focusLbl, keys: []string{focusKey, focusAlt}, hint: "sidebar", help: "switch sidebar ↔ pane (also " + focusAlt + ")",
		when: hasPane, run: func(a *app) { a.setFocus(!a.inTerm) }},
	{label: diffKey, keys: []string{diffKey}, hint: "git diff",
		help: "git diff of the session's project, sidebar too; s: side by side ↔ unified; ^D/^U: half page",
		run:  (*app).openDiff},
	{label: shellKey, keys: []string{shellKey}, hint: "shell",
		help: "one shell for the app, started in the project's directory, over the pane, sidebar too; again goes back",
		run:  (*app).toggleShell},
	{label: waitLbl, keys: []string{waitKey}, hint: "waiting", show: func(a *app) bool { return len(a.attn) > 0 },
		help: "focus next waiting session (◆), oldest first, sidebar too", run: (*app).focusWaiting},
	{label: "F4/^Z", keys: []string{zoomKey}, hint: "zoom",
		help: "hide / show the sidebar (Ctrl+Z too, except in the shell), sidebar too", run: (*app).toggleZoom},
	{keys: []string{zoomAlt}, when: func(a *app) bool { return a.active != shellID }, run: (*app).toggleZoom},
	{label: quitLbl, keys: []string{quitKey}, help: "quit cade, sidebar too (asks while sessions run)", run: (*app).askQuit},
	{label: "⇧PgUp/Dn", keys: []string{"Page_Up", "Shift+Page_Up"}, hint: "scroll", help: "scroll back / forward (PgUp/PgDn too)",
		when: hasScrollback, run: func(a *app) { a.pageScroll(1) }},
	{keys: []string{"Page_Down", "Shift+Page_Down"}, when: hasScrollback, run: func(a *app) { a.pageScroll(-1) }},
	{label: "wheel", help: "scroll; or drag the scrollbar"},
	{label: "drag", help: "select text, 2×click word, 3×click line → copied"},
	{label: "other keys", help: "sent to Claude as-is"},
}

func hasScrollback(a *app) bool {
	t := a.terms[a.active]
	if t == nil {
		return false
	}
	_, m := t.ScrollInfo()
	return m > 0
}

// match returns the active binding for k, or nil.
func (a *app) match(bs []binding, k vaxis.Key) *binding {
	var loose *binding
	for i := range bs {
		b := &bs[i]
		if b.run == nil || b.when != nil && !b.when(a) || !slices.ContainsFunc(b.keys, k.MatchString) {
			continue
		}
		// Shift+/ also matches "/" in vaxis; the binding for the typed text ("?") wins
		if k.Text == "" || slices.Contains(b.keys, k.Text) {
			return b
		}
		if loose == nil {
			loose = b
		}
	}
	return loose
}

// key routes a key press; it reports whether cade should quit.
func (a *app) key(k vaxis.Key) bool {
	release := k.EventType == vaxis.EventRelease
	// pasted text is never a shortcut
	paste := k.EventType == vaxis.EventPaste
	// the dialog is drawn over everything, so it takes keys before the diff and the pane
	if a.confirm != nil {
		if !release && !paste {
			a.confirmKey(k)
		}
		return a.quit
	}
	if a.diff != nil {
		if !release {
			a.diffKey(k)
		}
		return a.quit
	}
	if paste && a.inTerm && hasPane(a) {
		a.terms[a.active].Update(k)
		return false
	}
	// prompts and help keep Esc for cancel
	if b := a.match(paneKeys[:7], k); b != nil && !paste && (a.inTerm || !a.help && a.input == nil && a.rename == nil && a.search == nil && a.set == nil) {
		if !release {
			b.run(a)
		}
		return a.quit
	}
	if a.inTerm && hasPane(a) {
		if b := a.match(paneKeys, k); b != nil {
			if !release { // releases are swallowed too, so Claude never sees half a key
				b.run(a)
			}
			return false
		}
		a.terms[a.active].Update(k)
		return false
	}
	switch {
	case release:
	case a.help:
		a.help = false // any key closes it
	case a.input != nil:
		a.promptKey(k)
	case a.rename != nil:
		a.renameKey(k)
	case a.search != nil:
		a.searchKey(k)
	case a.set != nil:
		a.settingsKey(k)
	default:
		if !k.MatchString("d") {
			a.dWait = false
		}
		if b := a.match(sidebarKeys, k); b != nil && !paste {
			b.run(a)
		}
	}
	return a.quit
}

// pageScroll scrolls the pane one page back (dir>0) or forward (dir<0).
func (a *app) pageScroll(dir int) {
	t := a.terms[a.active]
	off, _ := t.ScrollInfo()
	_, h := a.paneSize()
	t.ScrollTo(off + dir*max(1, h-1))
}

func (a *app) newSession() {
	p := a.current().p
	a.projects[p].Collapsed = false
	a.cursor = slices.Index(a.rows(), row{p, rowNew})
	a.open()
}

func (a *app) foldSaved(collapse bool) {
	if a.fold(collapse) {
		a.save()
	}
}

func (a *app) moveSaved(dir int) {
	if a.move(dir) {
		a.save()
	}
}

func (a *app) toggleHints() {
	a.st.HideHints = !a.st.HideHints
	a.saveState()
}

func (a *app) toggleSide() {
	a.st.SidebarRight = !a.st.SidebarRight
	a.saveState()
}

// pressD waits ddWindow for a second d: d alone deletes a session, dd removes a project.
func (a *app) pressD() {
	if a.dWait {
		a.dWait = false
		a.confirmProject()
		return
	}
	a.dWait = true
	a.dSeq++
	seq := a.dSeq
	time.AfterFunc(ddWindow, func() { a.vx.PostEvent(dTimeout{seq}) })
}

// mouse routes a mouse event to the pane scrollbar, the sidebar or the Claude pane.
func (a *app) mouse(ev vaxis.Mouse) {
	t := a.terms[a.active]
	w, h := a.vx.Window().Size()
	drag := ev.Button == vaxis.MouseLeftButton && ev.EventType != vaxis.EventRelease
	a.hover = noBar
	x, end := a.paneSpan(w)
	switch {
	case a.diff != nil && ev.Col >= x && ev.Col < end: // the diff covers the pane
		if ev.Button == vaxis.MouseWheelUp {
			a.diff.pg.Offset = max(a.diff.pg.Offset-3, 0)
		} else if ev.Button == vaxis.MouseWheelDown {
			a.diff.pg.Offset += 3
		}
	case t != nil && ev.Col >= end-barW && ev.Col < end: // pane scrollbar
		a.hover = paneBar
		if drag {
			if !a.inTerm {
				a.setFocus(true)
			}
			_, m := t.ScrollInfo()
			t.ScrollTo(offsetAt(ev.Row-titleH, h-titleH, m))
		} else {
			t.Update(ev) // wheel over the bar still scrolls
		}
	case ev.Col < x || ev.Col >= end:
		ev.Col -= a.sideX(w)
		a.sidebarMouse(ev, drag)
	case t != nil:
		ev.Col -= x
		a.paneMouse(t, ev)
	}
}

func (a *app) sidebarMouse(ev vaxis.Mouse, drag bool) {
	if a.search != nil { // the tree is hidden behind the hits
		return
	}
	rows := a.rows()
	r := ev.Row - titleH
	onBar := len(rows) > a.visible && ev.Col == sideW-1 && r >= 0 && r < a.visible
	if onBar {
		a.hover = sideBar
	}
	switch {
	case ev.Button == vaxis.MouseWheelUp:
		a.scroll(a.top - 3)
	case ev.Button == vaxis.MouseWheelDown:
		a.scroll(a.top + 3)
	case onBar && drag: // tree scrollbar
		if a.inTerm {
			a.setFocus(false)
		}
		m := len(rows) - a.visible
		a.scroll(m - offsetAt(r, a.visible, m))
	case ev.Button == vaxis.MouseLeftButton && ev.EventType == vaxis.EventPress && r >= 0 && r+a.top < len(rows):
		a.cursor = r + a.top
		if rows[a.cursor].s >= 0 && ev.Col >= starX && ev.Col < starX+2 {
			a.toggleStar()
		} else if rows[a.cursor].s != rowHeader {
			a.open()
		}
	}
}

func (a *app) paneMouse(t *term.Model, ev vaxis.Mouse) {
	if ev.EventType == vaxis.EventPress {
		a.selSeq++
		if !a.inTerm {
			a.setFocus(true)
		}
	}
	ev.Row -= titleH
	t.Update(ev)
	// cade captures the mouse, so the host terminal's copy-on-select can't run; do it here.
	if ev.Button == vaxis.MouseLeftButton && ev.EventType == vaxis.EventRelease && t.HasSelection() {
		a.vx.ClipboardPush(trimCopy(t.Selection()))
		clear := clearSel{t, a.selSeq}
		time.AfterFunc(selLinger, func() { a.vx.PostEvent(clear) })
	}
}

// trimCopy drops trailing spaces from every line and the leading spaces all
// non-blank lines share, so relative indentation survives.
func trimCopy(s string) string {
	lines := strings.Split(s, "\n")
	indent := -1
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
		if lines[i] == "" {
			continue
		}
		n := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	for i, l := range lines {
		if l != "" {
			lines[i] = l[indent:]
		}
	}
	return strings.Join(lines, "\n")
}

const confirmHint = "[y] yes  [n] no"

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

// askQuit quits, first asking when sessions run, since quitting closes them.
func (a *app) askQuit() {
	var rows [][2]string
	claude := len(a.terms)
	if a.terms[shellID] != nil {
		claude--
		rows = append(rows, [2]string{"Shell", "running"})
	}
	if claude > 0 {
		rows = append([][2]string{{"Claude", fmt.Sprintf("%d running", claude)}}, rows...)
	}
	if len(rows) == 0 {
		a.quit = true
		return
	}
	a.confirm = &confirmation{
		title: "QUIT CADE",
		rows:  rows,
		note:  "Closes them; Claude sessions stay on disk and can be resumed.",
		yes:   func() { a.quit = true },
	}
}

// confirmSession asks to delete the session under the cursor from disk.
func (a *app) confirmSession() {
	cur := a.current()
	if cur.s < 0 {
		return
	}
	p := a.projects[cur.p]
	s := p.sessions[cur.s]
	file := filepath.Join(projectDir(a.root(p), p.Dir), s.ID+".jsonl")
	rows := [][2]string{{"Session", s.Title}, {"ID", s.ID}, {"Project", p.Name + "  " + a.short(p.Dir)}, {"File", a.short(file)}}
	if st, err := os.Stat(file); err == nil {
		rows = append(rows, [2]string{"Modified", st.ModTime().Format("2006-01-02 15:04")})
	}
	if a.terms[s.ID] != nil {
		rows = append(rows, [2]string{"Status", "running, Claude in it will be closed"})
	}
	a.confirm = &confirmation{
		title:  "DELETE SESSION",
		rows:   rows,
		note:   "Deletes the transcript from disk. Can't undo.",
		danger: true,
		yes:    func() { a.deleteSession(p, s.ID) },
	}
}

// confirmProject asks to remove the cursor's project from cade's list.
func (a *app) confirmProject() {
	if len(a.projects) == 1 {
		a.notify(toastWarn, "Can't remove the last project")
		return
	}
	p := a.projects[a.current().p]
	running := 0
	for _, s := range p.sessions {
		if a.terms[s.ID] != nil {
			running++
		}
	}
	sessions := fmt.Sprint(len(p.sessions))
	if running > 0 {
		sessions += fmt.Sprintf(" (%d running, will be closed)", running)
	}
	a.confirm = &confirmation{
		title: "REMOVE PROJECT",
		rows:  [][2]string{{"Project", p.Name}, {"Dir", a.short(p.Dir)}, {"Sessions", sessions}},
		note:  "Only leaves cade's list; folder and Claude sessions are kept.",
		yes:   func() { a.removeProject(p) },
	}
}

// openAdd opens the add-project dialog on the name, the directory prefilled with cade's own.
func (a *app) openAdd() {
	a.input = &prompt{dir: strings.TrimSuffix(a.short(a.cwd), "/") + "/"}
}

// split cuts the typed directory into the parent being listed and the query after the last /.
func (in *prompt) split() (parent, query string) {
	i := strings.LastIndex(in.dir, "/")
	return in.dir[:i+1], in.dir[i+1:]
}

// matches lists the typed parent's subdirectories that fuzzy-match the query, best first.
// The listing is cached until the parent changes.
func (a *app) matches() []fuzzyMatch[string] {
	in := a.input
	parent, query := in.split()
	if in.subs == nil || in.listed != parent {
		dir := parent
		if strings.HasPrefix(dir, "~/") {
			dir = filepath.Join(a.home, dir[2:])
		}
		in.subs, in.listed = subdirs(cmp.Or(dir, "."), in.hidden), parent
	}
	return fuzzyFilter(query, in.subs, func(s string) string { return s })
}

// promptKey edits the add-project dialog; drawAdd renders it.
func (a *app) promptKey(k vaxis.Key) {
	in := a.input
	field := &in.name
	ms := a.matches()
	in.sel = max(min(in.sel, len(ms)-1), 0)
	dirStep := in.step == 1
	if dirStep {
		field = &in.dir
	}
	switch {
	case k.MatchString("Escape"):
		a.input = nil
	case !dirStep && (k.MatchString("Enter") || k.MatchString("Tab") || k.MatchString("Down")):
		in.step = 1
	case dirStep && (k.MatchString("Shift+Tab") || in.sel == 0 && k.MatchString("Up")):
		in.step = 0
	case dirStep && (k.MatchString("Down") || k.MatchString("Ctrl+n")):
		in.sel = max(min(in.sel+1, len(ms)-1), 0)
	case dirStep && (k.MatchString("Up") || k.MatchString("Ctrl+p")):
		in.sel = max(in.sel-1, 0)
	case dirStep && k.MatchString("Tab"):
		if len(ms) > 0 {
			parent, _ := in.split()
			in.dir, in.sel, in.err = parent+ms[in.sel].item+"/", 0, ""
		}
	case dirStep && k.MatchString("Alt+h"):
		in.hidden, in.subs, in.sel = !in.hidden, nil, 0
	case k.MatchString("Enter"):
		path := in.dir
		if parent, query := in.split(); query != "" && len(ms) > 0 {
			path = parent + ms[in.sel].item // a partly typed name means the highlighted match
		}
		dir, err := expandDir(path, a.home)
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
	case typeKey(field, k):
		in.err, in.sel = "", 0
	}
}

// typeKey applies a backspace or typed text to field; it reports whether k was one.
func typeKey(field *string, k vaxis.Key) bool {
	switch {
	case k.MatchString("BackSpace"):
		if r := []rune(*field); len(r) > 0 {
			*field = string(r[:len(r)-1])
		}
	case k.Text != "" && k.Modifiers&(vaxis.ModCtrl|vaxis.ModAlt|vaxis.ModSuper) == 0:
		*field += k.Text
	default:
		return false
	}
	return true
}

// savedSession returns the cursor's session once Claude has written it to disk;
// renaming and starring a session that may never be saved is refused.
func (a *app) savedSession() (*project, session, bool) {
	cur := a.current()
	if cur.s < 0 {
		return nil, session{}, false
	}
	p := a.projects[cur.p]
	s := p.sessions[cur.s]
	if _, err := os.Stat(filepath.Join(projectDir(a.root(p), p.Dir), s.ID+".jsonl")); err != nil {
		a.notify(toastNotice, "Session not saved by Claude yet")
		return nil, session{}, false
	}
	return p, s, true
}

func (a *app) openRename() {
	if p, s, ok := a.savedSession(); ok {
		a.rename = &renaming{p: p, id: s.ID, title: s.Title}
	}
}

// renameKey edits the rename dialog; drawRename renders it.
func (a *app) renameKey(k vaxis.Key) {
	rn := a.rename
	switch {
	case k.MatchString("Escape"):
		a.rename = nil
	case k.MatchString("Enter"):
		a.rename = nil
		if t := strings.TrimSpace(rn.title); t == "" {
			delete(rn.p.Titles, rn.id)
		} else {
			if rn.p.Titles == nil {
				rn.p.Titles = map[string]string{}
			}
			rn.p.Titles[rn.id] = t
		}
		a.save()
		a.refresh()
	default:
		typeKey(&rn.title, k)
	}
}

func (a *app) toggleStar() {
	p, s, ok := a.savedSession()
	if !ok {
		return
	}
	if i := slices.Index(p.Starred, s.ID); i >= 0 {
		p.Starred = slices.Delete(p.Starred, i, i+1)
	} else {
		p.Starred = append(p.Starred, s.ID)
	}
	a.save()
	a.refresh()
}

// searchHits lists every session whose title fuzzy-matches the query, best first;
// collapsed projects are searched too.
func (a *app) searchHits() []fuzzyMatch[row] {
	var all []row
	for pi, p := range a.projects {
		for si := range p.sessions {
			all = append(all, row{pi, si})
		}
	}
	return fuzzyFilter(a.search.query, all, func(r row) string { return a.projects[r.p].sessions[r.s].Title })
}

// searchKey edits the search; drawSearch renders it. Enter opens the highlighted hit.
func (a *app) searchKey(k vaxis.Key) {
	sr := a.search
	hits := a.searchHits()
	sr.sel = max(min(sr.sel, len(hits)-1), 0)
	switch {
	case k.MatchString("Escape"):
		a.search = nil
	case k.MatchString("Down") || k.MatchString("Ctrl+n"):
		sr.sel = max(min(sr.sel+1, len(hits)-1), 0)
	case k.MatchString("Up") || k.MatchString("Ctrl+p"):
		sr.sel = max(sr.sel-1, 0)
	case k.MatchString("Enter"):
		if len(hits) == 0 {
			return
		}
		r := hits[sr.sel].item
		a.search = nil
		a.projects[r.p].Collapsed = false
		a.cursor = slices.Index(a.rows(), r)
		a.open()
	case typeKey(&sr.query, k):
		sr.sel = 0
	}
}

// settingRows are the settings dialog's rows, in order.
const (
	setTheme = iota
	setHints
	setSide
	setEdit
	settingRows
)

// settingsKey drives the settings dialog; drawSettings renders it.
func (a *app) settingsKey(k vaxis.Key) {
	st := a.set
	change := k.MatchString("Enter") || k.MatchString("space")
	switch {
	case k.MatchString("Escape"), k.MatchString(","), k.MatchString("q"):
		a.set = nil
	case k.MatchString("Up"), k.MatchString("k"), k.MatchString("Shift+Tab"): // before Tab, which Shift+Tab also matches
		st.sel = max(st.sel-1, 0)
	case k.MatchString("Down"), k.MatchString("j"), k.MatchString("Tab"):
		st.sel = min(st.sel+1, settingRows-1)
	case k.MatchString("e"), st.sel == setEdit && change:
		a.editConfig()
	case st.sel == setTheme && (k.MatchString("Left") || k.MatchString("h")):
		a.cycleTheme(-1)
	case st.sel == setTheme && (change || k.MatchString("Right") || k.MatchString("l")):
		a.cycleTheme(1)
	case st.sel == setHints && (change || k.MatchString("Left") || k.MatchString("Right")):
		a.toggleHints()
	case st.sel == setSide && (change || k.MatchString("Left") || k.MatchString("Right") || k.MatchString("h") || k.MatchString("l")):
		a.toggleSide()
	}
}
