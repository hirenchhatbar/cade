package main

import (
	"fmt"
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
	{label: "←/→", keys: []string{"Left"}, hint: "fold", help: "collapse / expand project",
		run: func(a *app) { a.foldSaved(true) }},
	{keys: []string{"Right"}, run: func(a *app) { a.foldSaved(false) }},
	{label: "a", keys: []string{"a"}, hint: "add project", help: "add project (name, then directory)",
		run: func(a *app) { a.input = &prompt{} }},
	{label: "⇧PgUp/Dn", keys: []string{"Shift+Page_Up"}, hint: "reorder", help: "move project up / down",
		run: func(a *app) { a.moveSaved(-1) }},
	{keys: []string{"Shift+Page_Down"}, run: func(a *app) { a.moveSaved(1) }},
	{label: "d/dd", keys: []string{"d"}, hint: "delete",
		help: "d: delete session from disk · dd: remove project from list (both ask)", run: (*app).pressD},
	{label: "g/G", keys: []string{"g", "Home"}, hint: "top/end", show: noPane, help: "top / end",
		run: func(a *app) { a.cursor = 0 }},
	{keys: []string{"G", "End"}, run: func(a *app) { a.cursor = len(a.rows()) - 1 }},
	{label: "⇥", keys: []string{"Tab", "Escape"}, hint: "pane", show: hasPane, when: hasPane, help: "go to Claude pane",
		run: func(a *app) { a.setFocus(true) }},
	{label: "?", keys: []string{"?"}, hint: "help", always: true, help: "this help", idle: "all shortcuts",
		run: func(a *app) { a.help = true }},
	{label: "q", keys: []string{"q", "Ctrl+c"}, hint: "quit", help: "quit", run: func(a *app) { a.quit = true }},
	{label: "H", keys: []string{"H"}, hint: "toggle hints", always: true, help: "show / hide these hints",
		run: (*app).toggleHints},
}

// paneKeys apply while the Claude pane has focus; everything else goes to Claude.
// The focus key is checked first, in any focus state.
var paneKeys = []binding{
	{label: focusLbl, keys: []string{focusKey, focusAlt}, hint: "sidebar", help: "switch sidebar ↔ pane (also " + focusAlt + ")",
		when: hasPane, run: func(a *app) { a.setFocus(!a.inTerm) }},
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
	for i := range bs {
		b := &bs[i]
		if b.run != nil && (b.when == nil || b.when(a)) && slices.ContainsFunc(b.keys, k.MatchString) {
			return b
		}
	}
	return nil
}

// key routes a key press; it reports whether cade should quit.
func (a *app) key(k vaxis.Key) bool {
	release := k.EventType == vaxis.EventRelease
	if b := a.match(paneKeys[:1], k); b != nil {
		if !release {
			b.run(a)
		}
		return false
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
	case a.confirm != nil:
		a.confirmKey(k)
	case a.input != nil:
		a.promptKey(k)
	default:
		a.msg = "" // errors last until the next key
		if !k.MatchString("d") {
			a.dWait = false
		}
		if b := a.match(sidebarKeys, k); b != nil {
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
	a.fail("save failed: ", writeJSON(statePath(), a.st))
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
	switch {
	case t != nil && ev.Col >= w-barW: // pane scrollbar
		if drag {
			_, m := t.ScrollInfo()
			t.ScrollTo(offsetAt(ev.Row-titleH, h-titleH, m))
		} else {
			t.Update(ev) // wheel over the bar still scrolls
		}
	case ev.Col <= sideW:
		a.sidebarMouse(ev, drag)
	case t != nil:
		a.paneMouse(t, ev)
	}
}

func (a *app) sidebarMouse(ev vaxis.Mouse, drag bool) {
	rows := a.rows()
	r := ev.Row - titleH
	switch {
	case ev.Button == vaxis.MouseWheelUp:
		a.scroll(a.top - 3)
	case ev.Button == vaxis.MouseWheelDown:
		a.scroll(a.top + 3)
	case len(rows) > a.visible && ev.Col == sideW-1 && r >= 0 && r < a.visible && drag: // tree scrollbar
		m := len(rows) - a.visible
		a.scroll(m - offsetAt(r, a.visible, m))
	case ev.Button == vaxis.MouseLeftButton && ev.EventType == vaxis.EventPress && r >= 0 && r+a.top < len(rows):
		a.cursor = r + a.top
		if rows[a.cursor].s != rowHeader {
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
	ev.Col -= sideW + 1
	ev.Row -= titleH
	t.Update(ev)
	// cade captures the mouse, so the host terminal's copy-on-select can't run; do it here.
	if ev.Button == vaxis.MouseLeftButton && ev.EventType == vaxis.EventRelease && t.HasSelection() {
		a.vx.ClipboardPush(t.Selection())
		clear := clearSel{t, a.selSeq}
		time.AfterFunc(selLinger, func() { a.vx.PostEvent(clear) })
	}
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

// confirmSession asks to delete the session under the cursor from disk.
func (a *app) confirmSession() {
	cur := a.current()
	if cur.s < 0 {
		return
	}
	p := a.projects[cur.p]
	s := p.sessions[cur.s]
	a.confirm = &confirmation{
		lines: []string{"Delete session from disk? Can't undo.", fmt.Sprintf("%s (%s)", s.Title, s.short())},
		yes:   func() { a.deleteSession(p, s.ID) },
	}
}

// confirmProject asks to remove the cursor's project from cade's list.
func (a *app) confirmProject() {
	if len(a.projects) == 1 {
		a.msg = "can't remove the last project"
		return
	}
	p := a.projects[a.current().p]
	a.confirm = &confirmation{
		lines: []string{"Remove project from cade's list?", p.Name + "  " + a.short(p.Dir), "Folder and Claude sessions are kept."},
		yes:   func() { a.removeProject(p) },
	}
}

// promptKey edits the add-project form; promptLines below renders it.
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

func (a *app) promptLines() []string {
	in := a.input
	field := func(label, s string) string {
		return label + truncateLeft(s, sideW-10) + "▏" // ▏ marks the field being typed
	}
	if in.step == 0 {
		return withErr([]string{"Add project  [⏎] next  [Esc] cancel", field("Name: ", in.name)}, in.err)
	}
	return withErr([]string{"Add project  [⏎] save  [Esc] cancel", "Name: " + truncateLeft(in.name, sideW-10), field("Dir:  ", in.dir)}, in.err)
}

func withErr(lines []string, err string) []string {
	if err != "" {
		lines = append(lines, "✗ "+err)
	}
	return lines
}
