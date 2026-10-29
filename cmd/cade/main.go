package main

import (
	"cmp"
	"fmt"
	"io"
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
	version  = "0.1.5"
	appLabel = appName + " v" + version
	sideDef  = 42            // sidebar width until resized; state.json keeps the resized one
	sideMin  = 30            // narrowest sidebar, and narrowest pane beside it
	sideStep = 2             // -/+ and settings step
	barW     = 2             // scrollbar columns
	focusKey = "Ctrl+Escape" // shortcut: hardcoded, make configurable once the key choice settles
	focusLbl = "Ctrl+Esc"
	// Ctrl+Esc needs the kitty keyboard protocol; legacy terminals send plain Esc, so F12 stays as fallback.
	focusAlt  = "F12"
	diffKey   = "F3"
	shellKey  = "F2"
	editorKey = "F5"
	zoomKey   = "F4"
	zoomAlt   = "Ctrl+z" // Claude's own Ctrl+Z (suspend) only freezes the pane; the shell keeps it for job control
	waitKey   = "Ctrl+f"
	waitLbl   = "Ctrl+F"
	quitKey   = "Ctrl+Shift+q" // needs the kitty keyboard protocol, like Ctrl+Esc; sidebar q always works
	quitLbl   = "Ctrl+⇧Q"
	shellID   = "shell"  // the one app-wide shell in terms
	editorID  = "editor" // the one app-wide editor in terms
	diffID    = "diff"   // the F3 diff in paneTerms; it is not a term
	fullKey   = "F11"
	splitMin  = 10 // shell.height and diff.height bounds and settings step, in % of the pane
	splitMax  = 90
	splitStep = 10
	newRowLbl = "[n] new session"
	selLinger = 600 * time.Millisecond // selection stays highlighted this long after a copy
	ddWindow  = 400 * time.Millisecond // a second d within this means dd
	// Claude's "auto" channel may pick OSC 99 (kitty), which the pane drops; BEL always arrives.
	notifSettings = `{"preferredNotifChannel":"terminal_bell"}`
)

type refreshEvent struct{}

// termEvent is an event from the terminal of session id, so bells can be told apart.
type termEvent struct {
	id string
	ev vaxis.Event
}

// dTimeout fires when no second d followed the first, so the single-d action runs.
type dTimeout struct{ seq int }

// clearSel fires a moment after a copy; seq drops it if the user clicked again since.
// A nil t is the diff.
type clearSel struct {
	t   *term.Model
	seq int
}

// prompt is the add-project dialog; step 0 asks the name, step 1 the directory,
// with the subdirectories of the typed path fuzzy-matched below it.
type prompt struct {
	step      int
	name, dir string
	err       string
	hidden    bool     // list dot-directories too
	sel       int      // highlighted match
	listed    string   // typed parent that subs was read for
	subs      []string // nil = not read yet
}

// searching is the / search: a flat list of sessions whose titles fuzzy-match query.
type searching struct {
	query string
	sel   int // highlighted hit
}

// renaming is the rename-session dialog.
type renaming struct {
	p     *project
	id    string
	title string
	busy  bool // Ctrl+T: claude is generating a title
}

// settings is the , dialog. issues are what checkConfig found after the last
// $EDITOR run; the config in use then is still the one from before the edit.
type settings struct {
	sel    int // highlighted row, see settingRows
	issues []string
	typing bool   // editing editor.cmd in its row
	cmd    string // editor.cmd as typed so far
}

// confirmation is a pending yes/no question, shown as a dialog; yes runs on y.
type confirmation struct {
	title  string
	rows   [][2]string // label, value: what the action touches
	note   string      // consequence, under the rows
	danger bool        // note in the error colour: the action can't be undone
	yes    func()
}

type app struct {
	tree
	vx       *vaxis.Vaxis
	home     string
	cwd      string
	cfgPath  string
	cfg      config
	th       theme
	st       state           // remembered UI state
	pending  map[string]bool // started from cade, jsonl may not exist yet
	terms    map[string]*term.Model
	active   string   // key into terms
	back     string   // pane shown before the shell, which F2 returns to
	shellDir string   // project dir the shell was started in
	shellOn  bool     // shell on screen, under terms[back] when split, see paneTerms
	editBack string   // pane shown before the editor, which F5 returns to
	editDir  string   // project dir the editor was started in
	grab     string   // term a left drag started in; it gets the drag until release
	resizing bool     // a left drag started on the sidebar separator; it gets the drag until release
	inTerm   bool     // keyboard focus on the Claude pane
	zoom     bool     // sidebar hidden while the pane or diff has focus; not remembered
	attn     []string // sessions that rang for attention, oldest first

	// transient UI state
	input    *prompt       // non-nil while adding a project
	rename   *renaming     // non-nil while renaming a session
	search   *searching    // non-nil while searching sessions
	confirm  *confirmation // non-nil while asking yes/no
	set      *settings     // non-nil while the settings dialog is open
	diff     *diffView     // non-nil while the git diff covers the pane
	help     bool          // shortcut dialog open
	toasts   []toast       // oldest first, see notify
	toastSeq int
	dWait    bool // a first d was pressed, see ddWindow
	dSeq     int
	selSeq   int  // bumped on every pane click, see clearSel
	pasting  bool // inside a bracketed paste; drawing waits for its end
	hover    bar  // scrollbar under the mouse pointer
	quit     bool
}

// bar names a scrollbar.
type bar int

const (
	noBar bar = iota
	sideBar
	paneBar
)

func main() {
	if len(os.Args) > 1 {
		os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr))
	}
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	cfg, _, err := checkConfig(configPath(), home, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cade: %s:\n%s\n", configPath(), indent(err.Error()))
		os.Exit(1)
	}
	cfgPath := projectsPath()
	projects, err := loadProjects(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cade: %s: %v\n", cfgPath, err)
		os.Exit(1)
	}
	if len(projects) == 0 {
		projects = []*project{{Name: filepath.Base(cwd), Dir: cwd}}
	}

	vx, err := vaxis.New(vaxis.Options{
		CSIuBitMask: vaxis.CSIuDisambiguate | vaxis.CSIuReportEvents |
			vaxis.CSIuAlternateKeys | vaxis.CSIuAllKeys | vaxis.CSIuAssociatedText,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "cade:", err)
		os.Exit(1)
	}
	a := &app{tree: tree{projects: projects}, vx: vx, home: home, cwd: cwd, cfgPath: cfgPath, cfg: cfg,
		st: loadState(statePath()), pending: map[string]bool{}, terms: map[string]*term.Model{}}
	var themeErr error
	a.th, themeErr = loadTheme(cfg)
	a.fail("", themeErr)
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath()), "config.json")); err == nil {
		a.notify(toastWarn, "config.json is no longer read: rename it to config.yaml")
	}
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

// cli handles a command-line option and returns the exit code; cade itself takes no arguments.
func cli(args []string, out, errOut io.Writer) int {
	switch args[0] {
	case "-h", "--help":
		fmt.Fprint(out, usage())
	case "-c", "--check":
		home, _ := os.UserHomeDir()
		cwd, _ := os.Getwd()
		_, issues, _ := checkConfig(configPath(), home, cwd)
		if len(issues) > 0 {
			fmt.Fprintf(errOut, "%s:\n%s\n", configPath(), indent(strings.Join(issues, "\n")))
			return 1
		}
		fmt.Fprintf(out, "%s: ok\n", configPath())
	case "-k", "--keys":
		fmt.Fprint(out, keysMarkdown())
	case "-v", "--version":
		fmt.Fprintln(out, appName, version)
	default:
		fmt.Fprintf(errOut, "cade: unknown option %q\n\n%s", args[0], usage())
		return 2
	}
	return 0
}

func indent(s string) string { return "  " + strings.ReplaceAll(s, "\n", "\n  ") }

// logo is the app name in block letters, shown in the idle pane and --help.
const logo = ` ██████╗ █████╗ ██████╗ ███████╗
██╔════╝██╔══██╗██╔══██╗██╔════╝
██║     ███████║██║  ██║█████╗
██║     ██╔══██║██║  ██║██╔══╝
╚██████╗██║  ██║██████╔╝███████╗
 ╚═════╝╚═╝  ╚═╝╚═════╝ ╚══════╝`

func usage() string {
	return logo + "\n\n" + appLabel + ` - a terminal ADE for Claude Code sessions.

Usage: cade [option]

With no option, cade opens a sidebar of your projects and their Claude
sessions; the selected session runs in the right pane. Press ? inside for
all shortcuts.

Options:
  -h, --help     show this help
  -c, --check    list problems in config.yaml
  -k, --keys     print all keyboard shortcuts as Markdown
  -v, --version  print the version

Files in ` + filepath.Dir(projectsPath()) + `:
  projects.json  projects, their order and collapsed state, session titles and stars (written by cade)
  state.json     remembered UI state (written by cade)
  config.yaml    theme and claude accounts (edited by you; the theme also from , settings)
`
}

func (a *app) run() {
	for ev := range a.vx.Events() {
		switch ev := ev.(type) {
		case vaxis.PasteStartEvent:
			a.pasting = true
		case vaxis.PasteEndEvent:
			a.pasting = false
		case vaxis.Key:
			a.pasting = a.pasting && ev.EventType == vaxis.EventPaste // a lost end marker can't freeze the screen
		}
		if quit := a.handle(ev); quit {
			return
		}
		if !a.pasting { // a redraw per pasted key made pastes take ~1ms per character
			a.draw()
		}
	}
}

// handle routes one event; it reports whether cade should quit.
func (a *app) handle(ev vaxis.Event) bool {
	switch ev := ev.(type) {
	case vaxis.Key:
		return a.key(ev)
	case vaxis.Resize:
		a.vx.Resize(ev) // also forces a full repaint: rows the terminal dropped while smaller come back blank
	case vaxis.PasteStartEvent, vaxis.PasteEndEvent, vaxis.FocusIn, vaxis.FocusOut, vaxis.ColorThemeUpdate:
		if t := a.terms[a.active]; a.inTerm && t != nil {
			t.Update(ev)
		}
	case vaxis.Mouse:
		a.mouse(ev)
	case refreshEvent:
		a.refresh()
		if a.diff != nil {
			a.loadDiff()
		}
	case diffEvent:
		a.showDiff(ev)
	case titleEvent:
		a.showTitle(ev)
	case toastExpire:
		a.expireToast(ev.id)
	case clearSel:
		switch {
		case ev.seq != a.selSeq:
		case ev.t != nil:
			ev.t.ClearSelection()
		case a.diff != nil:
			a.diff.to = a.diff.from
		}
	case dTimeout:
		if a.dWait && ev.seq == a.dSeq {
			a.dWait = false
			a.confirmSession()
		}
	case termEvent:
		switch e := ev.ev.(type) {
		case term.EventBell, term.EventNotify:
			if a.inTerm && ev.id == a.active {
				if n, ok := e.(term.EventNotify); ok {
					a.vx.Notify(n.Title, n.Body)
				}
			} else if a.markAttn(ev.id) {
				a.vx.Bell()
				a.vx.Notify(appName, a.titleOf(ev.id))
			}
		default:
			return a.handle(e)
		}
	case term.EventClosed:
		for id, t := range a.terms {
			if t == ev.Term {
				a.closeTerm(id)
				delete(a.pending, id)
			}
		}
	case term.EventTitle:
		a.vx.SetTitle(string(ev))
	case term.EventMouseShape:
		a.vx.SetMouseShape(ev.Shape)
	}
	return false
}

// fail shows err (with an optional prefix) as an error toast; nil shows nothing.
func (a *app) fail(prefix string, err error) {
	if err != nil {
		a.notify(toastErr, prefix+err.Error())
	}
}

func (a *app) save() { a.fail("save failed: ", saveProjects(a.cfgPath, a.projects)) }

// open switches to the selected session's terminal, starting claude if needed.
// On a header or new-session row it starts a new session in that project.
func (a *app) open() {
	a.diff = nil
	cur := a.current()
	p := a.projects[cur.p]
	var key string
	var args []string
	if cur.s < 0 {
		key = newUUID()
		args = []string{"--session-id", key}
		a.pending[key] = true
		p.sessions = append([]session{{ID: key, Title: "new session"}}, p.sessions...)
		p.arrange()
		a.restore(rowKey{dir: p.Dir, id: key})
	} else {
		key = p.sessions[cur.s].ID
		args = []string{"--resume", key}
	}
	args = append(args, "--settings", notifSettings)
	if _, ok := a.terms[key]; !ok {
		t := term.New(term.WithVaxis(a.vx))
		t.Attach(func(ev vaxis.Event) { a.vx.PostEvent(termEvent{key, ev}) })
		cmd := a.account(p).cmd(a.home, p.Dir, args...)
		w, h := a.paneSize()
		if err := t.StartWithSize(cmd, w, h); err != nil {
			a.fail("claude: ", err)
			return
		}
		a.terms[key] = t
	}
	a.show(key)
}

// show puts terms[key] in the pane and focuses it; any session but the one split
// above the shell hides the shell.
func (a *app) show(key string) {
	if key != shellID && (key != a.back || a.st.ShellFull) {
		a.shellOn = false
	}
	if prev := a.terms[a.active]; prev != nil && a.active != key {
		prev.Blur()
	}
	a.active = key
	a.setFocus(true)
}

// toggleShell shows the app's one $SHELL under the pane shown before (see paneTerms),
// starting it in the cursor's project dir if needed; when shown it hides it, still
// running, back to that pane, or the empty pane if there was none.
func (a *app) toggleShell() {
	a.diff = nil
	p := a.projects[a.current().p]
	key := shellID
	if a.shellOn {
		a.shellOn = false
		if a.active != key {
			return
		}
		if a.terms[a.back] != nil {
			a.show(a.back)
		} else {
			a.setFocus(false)
			a.active = ""
		}
		return
	}
	if _, ok := a.terms[key]; !ok {
		t := term.New(term.WithVaxis(a.vx))
		t.Attach(func(ev vaxis.Event) { a.vx.PostEvent(termEvent{key, ev}) })
		cmd := exec.Command(cmp.Or(os.Getenv("SHELL"), "sh"))
		cmd.Dir = p.Dir
		cmd.Env = append(os.Environ(), "PWD="+p.Dir)
		w, h := a.paneSize()
		if err := t.StartWithSize(cmd, w, h); err != nil {
			a.fail("shell: ", err)
			return
		}
		a.terms[key] = t
		a.shellDir = p.Dir
	}
	a.back = a.active
	a.shellOn = true
	a.show(key)
}

// toggleEditor shows the app's one editor (editor.cmd) in the pane, starting it
// in the cursor's project dir if needed; when shown it hides it, still running,
// back to the pane shown before, or the empty pane if there was none.
func (a *app) toggleEditor() {
	a.diff = nil
	key := editorID
	if a.active == key || a.split() && a.back == key {
		if a.back == key {
			a.shellOn = false // the shell split under the editor hides with it
		}
		if a.terms[a.editBack] != nil {
			a.show(a.editBack)
		} else {
			a.setFocus(false)
			a.active = ""
		}
		return
	}
	if _, ok := a.terms[key]; !ok {
		p := a.projects[a.current().p]
		t := term.New(term.WithVaxis(a.vx))
		t.Attach(func(ev vaxis.Event) { a.vx.PostEvent(termEvent{key, ev}) })
		// through sh, like editConfig, so a command with flags ("nvim .") works
		cmd := exec.Command("sh", "-c", a.editorCmd())
		cmd.Dir = p.Dir
		cmd.Env = append(os.Environ(), "PWD="+p.Dir)
		w, h := a.paneSize()
		if err := t.StartWithSize(cmd, w, h); err != nil {
			a.fail("editor: ", err)
			return
		}
		a.terms[key] = t
		a.editDir = p.Dir
	}
	a.editBack = a.active
	if a.active == shellID { // F5 returns to the session the shell was over
		a.editBack = a.back
	}
	if a.editBack == key { // the full-height shell was over the editor
		a.editBack = ""
	}
	a.show(key)
}

// toggleFull switches the diff, else the shell, between full height and split
// under the session, remembered, and focuses it.
func (a *app) toggleFull() {
	if a.diff != nil {
		a.st.DiffFull = !a.st.DiffFull
		a.saveState()
		a.focusLower()
		return
	}
	a.st.ShellFull = !a.st.ShellFull
	a.saveState()
	a.show(shellID)
}

// focusUpper focuses the session above the split diff or shell.
func (a *app) focusUpper() {
	if a.diff != nil {
		a.diff.focus = false
		a.setFocus(true)
		return
	}
	a.show(a.back)
}

// focusLower focuses the diff or shell.
func (a *app) focusLower() {
	if a.diff != nil {
		a.setFocus(false)
		a.diff.focus = true
		return
	}
	a.show(shellID)
}

// refresh reloads every project's sessions from disk and keeps the cursor on the same row.
func (a *app) refresh() {
	var k rowKey
	if rows := a.rows(); a.cursor < len(rows) {
		k = a.keyOf(rows[a.cursor])
	}
	for _, p := range a.projects {
		p.sessions = merge(p.sessions, loadSessions(a.root(p), p.Dir), a.pending)
		p.arrange()
	}
	a.restore(k)
}

func (a *app) deleteSession(p *project, id string) {
	a.closeTerm(id)
	delete(a.pending, id)
	if err := deleteSessionFiles(a.root(p), p.Dir, id); err != nil {
		a.fail("delete failed: ", err)
		return
	}
	p.sessions = slices.DeleteFunc(p.sessions, func(s session) bool { return s.ID == id })
	p.forget(id)
	a.save()
	a.restore(rowKey{})
	a.notify(toastOK, "Session deleted")
}

func (a *app) removeProject(p *project) {
	for _, s := range p.sessions {
		a.closeTerm(s.ID)
	}
	a.projects = slices.DeleteFunc(a.projects, func(q *project) bool { return q == p })
	a.save()
	a.restore(rowKey{})
	a.notify(toastOK, "Project "+p.Name+" removed")
}

func (a *app) account(p *project) account { return a.cfg.account(p.Dir, a.home) }

// root is where p's account keeps its sessions.
func (a *app) root(p *project) string { return a.account(p).sessionsRoot(a.home) }

func (a *app) closeTerm(id string) {
	if t := a.terms[id]; t != nil {
		t.Close()
		delete(a.terms, id)
		a.clearAttn(id)
		if id == shellID {
			a.shellOn = false
		}
		if a.active == id {
			a.active, a.inTerm = "", false
			switch {
			case a.shellOn: // the session above the shell ended: the shell fills the pane
				a.active = shellID
			case id == shellID && a.terms[a.back] != nil:
				a.active = a.back
			case id == editorID && a.terms[a.editBack] != nil:
				a.active = a.editBack
			}
		}
	}
}

func (a *app) setFocus(inTerm bool) {
	a.inTerm = inTerm
	if inTerm {
		a.clearAttn(a.active)
	}
	if t := a.terms[a.active]; t != nil {
		if inTerm {
			t.Focus()
		} else {
			t.Blur()
		}
	}
}

// paneSize is the size of the focused term, or of the diff when it has focus.
func (a *app) paneSize() (int, int) {
	w, h := a.vx.Window().Size()
	x, end := a.paneSpan(w)
	key, ph := a.active, h-titleH
	if a.diffFocused() {
		key = diffID
	}
	for _, p := range a.paneTerms(h) {
		if p.key == key {
			ph = p.h
		}
	}
	return max(end-x-barW, 1), max(ph, 1) // scrollbar
}

// paneTerm is a term or the diff on screen: its key in terms or diffID, and its
// first row and height under its title bar.
type paneTerm struct {
	key  string
	y, h int
}

// paneTerms lays out the pane on a screen h tall, top to bottom: the session and
// the diff or shell split under it, which takes diff.height or shell.height % of
// the rows; else the diff or the active term alone.
func (a *app) paneTerms(h int) []paneTerm {
	rows := h - 2*titleH
	lower, upper, pct, _ := a.lower()
	if !a.split() || rows < 2 {
		if lower == diffID {
			return []paneTerm{{diffID, titleH, h - titleH}}
		}
		return []paneTerm{{a.active, titleH, h - titleH}}
	}
	sh := min(max(rows*pct/100, 1), rows-1)
	return []paneTerm{{upper, titleH, rows - sh}, {lower, h - sh, sh}}
}

// lower is what can split the pane under a session: the diff, else the shell
// when shown; upper is the session it goes under, pct its height and full
// whether F11 made it fill the pane.
func (a *app) lower() (key, upper string, pct int, full bool) {
	switch {
	case a.diff != nil:
		return diffID, a.active, splitPct(a.cfg.Diff.Height), a.st.DiffFull
	case a.shellOn:
		return shellID, a.back, splitPct(a.cfg.Shell.Height), a.st.ShellFull
	}
	return "", "", 0, false
}

// split reports whether the diff or the shell shares the pane with a session.
func (a *app) split() bool {
	key, upper, _, full := a.lower()
	return key != "" && !full && a.terms[upper] != nil
}

func (a *app) lowerShown() bool  { return a.diff != nil || a.shellOn }
func (a *app) diffFocused() bool { return a.diff != nil && a.diff.focus }

// sideShown reports whether the sidebar is on screen: zooming hides it while the
// pane or diff has focus. Focusing the sidebar shows it again until the pane has focus back.
func (a *app) sideShown() bool { return !a.zoom || (!a.inTerm && !a.diffFocused()) }

// sideX is the sidebar's first column on a screen w wide.
func (a *app) sideX(w int) int {
	if a.st.SidebarRight {
		return w - a.sideW()
	}
	return 0
}

// paneSpan is the pane's first column and the column after its last on a screen
// w wide: beside the sidebar and its separator, or the whole width when zoomed.
func (a *app) paneSpan(w int) (x, end int) {
	switch {
	case !a.sideShown():
		return 0, w
	case a.st.SidebarRight:
		return 0, w - a.sideW() - 1
	}
	return a.sideW() + 1, w
}

// sideW is the sidebar's width, 0 in state.json being the default.
func (a *app) sideW() int { return cmp.Or(a.st.SidebarW, sideDef) }

// sepX is the column of the line between the sidebar and the pane on a screen w wide.
func (a *app) sepX(w int) int {
	if a.st.SidebarRight {
		return w - a.sideW() - 1
	}
	return a.sideW()
}

// clampSide keeps a sidebar width v between sideMin and what leaves the pane
// sideMin columns on a screen w wide.
// shortcut: not re-clamped when the terminal shrinks later, clamp at draw time if that bites.
func clampSide(v, w int) int { return max(min(v, w-sideMin-1), sideMin) }

// stepSide widens (dir 1) or narrows (dir -1) the sidebar, saving it to state.json.
func (a *app) stepSide(dir int) {
	w, _ := a.vx.Window().Size()
	a.st.SidebarW = clampSide(a.sideW()+dir*sideStep, w)
	a.saveState()
}

// toggleZoom hides or shows the sidebar; zooming with the sidebar focused moves to the pane.
func (a *app) toggleZoom() {
	if a.diff == nil && !hasPane(a) {
		return
	}
	a.zoom = !a.zoom
	if a.zoom && !a.diffFocused() {
		a.setFocus(true)
	}
}

// markAttn queues session id as waiting; it reports whether it was new.
func (a *app) markAttn(id string) bool {
	if slices.Contains(a.attn, id) {
		return false
	}
	a.attn = append(a.attn, id)
	return true
}

func (a *app) clearAttn(id string) {
	a.attn = slices.DeleteFunc(a.attn, func(s string) bool { return s == id })
}

// focusWaiting opens the session that has waited longest.
func (a *app) focusWaiting() {
	for len(a.attn) > 0 {
		id := a.attn[0]
		for pi, p := range a.projects {
			if si := slices.IndexFunc(p.sessions, func(s session) bool { return s.ID == id }); si >= 0 {
				p.Collapsed = false
				a.cursor = slices.Index(a.rows(), row{pi, si})
				a.open()
				return
			}
		}
		a.clearAttn(id) // session gone from disk and list
	}
	a.notify(toastNotice, "No session waiting")
}

// editConfig runs $EDITOR on config.yaml with cade suspended, then checks the
// file. A clean file takes effect at once, accounts for sessions started from
// then on; any issue keeps the config in use and lists the issues in the dialog.
func (a *app) editConfig() {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		a.set.issues = []string{err.Error()}
		return
	}
	// through sh, like git, so an editor with flags ("code -w") works
	cmd := exec.Command("sh", "-c", envEditor()+` "$1"`, "sh", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := a.vx.Suspend(); err != nil {
		a.set.issues = []string{"suspend: " + err.Error()}
		return
	}
	runErr := cmd.Run()
	if err := a.vx.Resume(); err != nil {
		a.fail("resume: ", err)
	}
	if runErr != nil {
		a.set.issues = []string{"editor: " + runErr.Error()}
		return
	}
	cfg, issues, _ := checkConfig(path, a.home, a.cwd)
	if a.set.issues = issues; len(issues) > 0 {
		return
	}
	a.cfg = cfg
	a.th, _ = loadTheme(cfg) // no issues, so no theme error
	a.refresh()              // an account may now keep its sessions elsewhere
	a.notify(toastOK, "config.yaml reloaded")
}

// envEditor is the editor from the environment, picked like git does.
func envEditor() string { return cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi") }

// editorCmd is the F5 editor's command: editor.cmd, else envEditor.
func (a *app) editorCmd() string { return cmp.Or(a.cfg.Editor.Cmd, envEditor()) }

// setEditorCmd saves editor.cmd to config.yaml; "" means envEditor. A running
// editor keeps its command; the next one started uses this.
func (a *app) setEditorCmd(cmd string) {
	if err := saveConfig(configPath(), cmd, "editor", "cmd"); err != nil {
		a.fail("save failed: ", err)
		return
	}
	a.cfg.Editor.Cmd = cmd
}

// themeNames lists the built-in and user themes, sorted.
func (a *app) themeNames() []string {
	var names []string
	for name := range themes {
		names = append(names, name)
	}
	for name := range a.cfg.Themes {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// cycleTheme switches to the next (dir>0) or previous theme, saving it to config.yaml.
func (a *app) cycleTheme(dir int) {
	names := a.themeNames()
	i := slices.Index(names, cmp.Or(a.cfg.Theme, defaultTheme))
	name := names[((i+dir)%len(names)+len(names))%len(names)]
	if err := saveConfig(configPath(), name, "theme"); err != nil {
		a.fail("save failed: ", err)
		return
	}
	a.cfg.Theme = name
	var err error
	a.th, err = loadTheme(a.cfg)
	a.fail("", err)
}

// toggleConfirmQuit flips quit.confirm, saving it to config.yaml.
func (a *app) toggleConfirmQuit() {
	confirm := !a.confirmQuit()
	if err := saveConfig(configPath(), confirm, "quit", "confirm"); err != nil {
		a.fail("save failed: ", err)
		return
	}
	a.cfg.Quit.Confirm = &confirm
}

// stepSplit changes section.height (shell or diff), pointed to by h, by dir
// steps, saving it to config.yaml.
func (a *app) stepSplit(dir int, h *int, section string) {
	v := min(max(splitPct(*h)+dir*splitStep, splitMin), splitMax)
	if err := saveConfig(configPath(), v, section, "height"); err != nil {
		a.fail("save failed: ", err)
		return
	}
	*h = v
}

// splitPct is a shell.height or diff.height, 0 being the default.
func splitPct(h int) int { return cmp.Or(h, 50) }

func (a *app) confirmQuit() bool { return a.cfg.Quit.Confirm == nil || *a.cfg.Quit.Confirm }
