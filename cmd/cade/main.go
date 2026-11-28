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
	appName  = "Cade"
	version  = "0.1.24"
	appLabel = appName + " v" + version
	sideDef  = 42            // sidebar width until resized; state.json keeps the resized one
	sideMin  = 30            // narrowest sidebar, and narrowest pane beside it
	sideStep = 2             // -/+ and settings step
	barW     = 2             // scrollbar columns
	focusKey = "Ctrl+Escape" // shortcut: hardcoded, make configurable once the key choice settles
	focusLbl = "Ctrl+Esc"
	// Ctrl+Esc needs the kitty keyboard protocol; legacy terminals send plain Esc, so F12 stays as fallback.
	focusAlt   = "F12"
	diffKey    = "F3"
	shellKey   = "F2"
	editorKey  = "F4"
	zoomKey    = "F5"
	zoomAlt    = "Ctrl+z" // Claude's own Ctrl+Z (suspend) only freezes the pane; the shell keeps it for job control
	waitKey    = "Ctrl+f"
	waitLbl    = "Ctrl+F"
	newKey     = "Ctrl+n"
	newLbl     = "Ctrl+N"
	delKey     = "Ctrl+d"
	delLbl     = "Ctrl+D"
	renKey     = "Ctrl+r" // Claude's own Ctrl+R (history search) is given up; the shell and editor keep theirs
	renLbl     = "Ctrl+R"
	recentMax  = 50             // sessions kept in state.json's history
	quitKey    = "Ctrl+Shift+q" // needs the kitty keyboard protocol, like Ctrl+Esc; sidebar q always works
	quitLbl    = "Ctrl+⇧Q"
	shellID    = "shell"  // the one app-wide shell in terms
	editorID   = "editor" // the one app-wide editor in terms
	diffID     = "diff"   // the F3 diff in paneTerms; it is not a term
	colsKey    = "F6"
	foldKey    = "F7"
	fullKey    = "F11"
	splitMinW  = 60 // columns each side needs for side by side; a narrower pane stacks them
	splitMin   = 10 // shell.height and diff.height bounds and settings step, in % of the pane
	splitMax   = 90
	splitStep  = 10
	newRowLbl  = "[n] new session"
	selLinger  = 600 * time.Millisecond // selection stays highlighted this long after a copy
	ddWindow   = 400 * time.Millisecond // a second d within this means dd
	edgeRepeat = 50 * time.Millisecond  // a drag held past a pane edge scrolls this often
	edgeAccel  = 2                      // ... one row more per this many repeats held there,
	edgeMax    = 30                     // ... up to this many rows each time
	// Claude's "auto" channel may pick OSC 99 (kitty), which the pane drops; BEL always arrives.
	notifSettings = `{"preferredNotifChannel":"terminal_bell"}`
)

type refreshEvent struct{}

// termEvent is an event from terminal t, terms[id] when it was made, so bells
// can be told apart and a stale close is dropped.
type termEvent struct {
	id string
	t  pane
	ev vaxis.Event
}

// dTimeout fires when no second d followed the first, so the single-d action runs.
type dTimeout struct{ seq int }

// edgeTick repeats a drag held past a pane edge; seq drops it once any other mouse event came.
type edgeTick struct {
	ev  vaxis.Mouse
	seq int
}

// clearSel fires a moment after a copy; seq drops it if the user clicked again since.
// A nil t is the diff.
type clearSel struct {
	t   pane
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
	id    string // session ID; "" renames the project
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
	terms    map[string]pane // Claude sessions are remoteTerms; the shell and editor run here
	active   string          // key into terms
	back     string          // pane shown before the shell, which F2 returns to
	shellDir string          // project dir the shell was started in
	low      string          // shellID or editorID on screen, under terms[back] when split, see paneTerms; "" none
	editDir  string          // project dir the editor was started in
	grab     string          // term a left drag started in; it gets the drag until release
	resizing bool            // a left drag started on the sidebar separator; it gets the drag until release
	dragging bool            // a left drag started on the split line; it gets the drag until release
	upFull   bool            // F11 on the session: it fills the pane, the diff, shell or editor open behind it; not remembered
	inTerm   bool            // keyboard focus on the Claude pane
	zoom     bool            // sidebar hidden while the pane or diff has focus; not remembered
	mobile   bool            // --mobile: zoom for good, and the sidebar fills the screen when shown
	attn     []string        // sessions that rang for attention, oldest first

	// transient UI state
	input     *prompt       // non-nil while adding a project
	rename    *renaming     // non-nil while renaming a session
	search    *searching    // non-nil while searching sessions
	confirm   *confirmation // non-nil while asking yes/no
	set       *settings     // non-nil while the settings dialog is open
	diff      *diffView     // non-nil while the git diff covers the pane
	help      bool          // shortcut dialog open
	toasts    []toast       // oldest first, see notify
	toastSeq  int
	dWait     bool // a first d was pressed, see ddWindow
	dSeq      int
	selSeq    int  // bumped on every pane click, see clearSel
	mouseSeq  int  // bumped on every mouse event, see edgeTick
	edgeRun   int  // mouse events in a row a drag was held past a pane edge, see edgeAccel
	pasting   bool // inside a bracketed paste; drawing waits for its end
	hover     bar  // scrollbar under the mouse pointer
	quit      bool
	verWarned bool // told the session server runs another version
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
	start(false, "")
}

// start runs Cade; mobile (--mobile) shows the sidebar or the pane, one at a time, full width;
// density (--density), when set, stands in for sidebar.density of config.yaml for this run.
func start(mobile bool, density string) {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	cfg, _, err := checkConfig(configPath(), home, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cade: %s:\n%s\n", configPath(), indent(err.Error()))
		os.Exit(1)
	}
	if density != "" {
		cfg.Sidebar.Density = density
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
		st: loadState(statePath()), pending: map[string]bool{}, terms: map[string]pane{}, mobile: mobile}
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
	for _, id := range a.st.Recent { // the last session still listed wins over cwd's project
		if a.cursorTo(id) {
			break
		}
	}
	go func() {
		for range time.Tick(2 * time.Second) {
			vx.PostEvent(refreshEvent{})
		}
	}()
	a.attachRunning()
	defer func() {
		for _, id := range []string{shellID, editorID} { // Claude sessions keep running in the server
			if t := a.terms[id]; t != nil {
				t.Close()
			}
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
	case "-s", "--serve":
		return serve()
	case "-k", "--keys":
		fmt.Fprint(out, keysMarkdown())
	case "-v", "--version":
		fmt.Fprintln(out, appName, version)
	default:
		mobile, density, err := launchOpts(args)
		if err != nil {
			fmt.Fprintf(errOut, "cade: %v\n\n%s", err, usage())
			return 2
		}
		start(mobile, density)
	}
	return 0
}

// launchOpts reads the options that start Cade, in any order: -m and -d <density>
// (also --density <density> and --density=<density>).
func launchOpts(args []string) (mobile bool, density string, err error) {
	for i := 0; i < len(args); i++ {
		opt, val, hasVal := strings.Cut(args[i], "=")
		switch {
		case !hasVal && (opt == "-m" || opt == "--mobile"):
			mobile = true
		case opt == "-d" || opt == "--density":
			if !hasVal && i+1 < len(args) {
				i++
				val = args[i]
			}
			if _, ok := densities[val]; !ok {
				return false, "", fmt.Errorf("--density: %q is not one of %s", val, densityNames())
			}
			density = val
		default:
			return false, "", fmt.Errorf("unknown option %q", args[i])
		}
	}
	return mobile, density, nil
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
       cade [-m] [-d compact|comfortable]

With no option, Cade opens a sidebar of your projects and their Claude
sessions; the selected session runs in the right pane. Press ? inside for
all shortcuts.

Options:
  -h, --help     show this help
  -c, --check    list problems in config.yaml
  -m, --mobile   phone layout: the sidebar or the pane, one at a time, full
                 width; tap the pane's title bar for the sidebar, ✕ or Tab back
  -d, --density  sidebar density for this run: compact or comfortable, a
                 blank line under each row; default sidebar.density of config.yaml
  -s, --serve    run the session server in the foreground; Cade starts it
                 when needed, and Claude sessions keep running in it after Cade quits
  -k, --keys     print all keyboard shortcuts as Markdown
  -v, --version  print the version

Files in ` + filepath.Dir(projectsPath()) + `:
  projects.json  projects, their order and collapsed state, session titles and stars (written by Cade)
  state.json     remembered UI state (written by Cade)
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
		return a.quit // a tap on [quit] under --mobile
	case refreshEvent:
		a.attachRunning()
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
	case edgeTick:
		if ev.seq == a.mouseSeq {
			a.mouse(ev.ev)
		}
	case dTimeout:
		if a.dWait && ev.seq == a.dSeq {
			a.dWait = false
			a.confirmSession()
		}
	case termEvent:
		switch e := ev.ev.(type) {
		case term.EventClosed:
			if a.terms[ev.id] == ev.t { // not a new term under the same key
				a.closeTerm(ev.id)
				delete(a.pending, ev.id)
			}
		case term.EventClipboard:
			a.vx.ClipboardPush(string(e))
		case copied:
			a.copySel(ev.t, string(e))
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
		w, h := a.paneSize()
		t, err := openRemote(key, a.account(p).cmd(a.home, p.Dir, args...), w, h, a.postTerm(key))
		if err != nil {
			a.fail("claude: ", err)
			return
		}
		a.terms[key] = t
	}
	a.visit(key)
	a.show(key)
	if a.diff != nil { // stays open under the session, on its project
		a.diff.focus = false
		if a.diff.dir != p.Dir {
			a.diff = &diffView{name: p.Name, dir: p.Dir, back: a.diff.back}
			a.loadDiff()
		}
	}
}

// newTerm makes the local terminal for terms[key], its events posted to the loop.
func (a *app) newTerm(key string) *term.Model {
	t := newModel(term.WithVaxis(a.vx))
	post := a.postTerm(key)
	t.Attach(func(ev vaxis.Event) { post(t, ev) })
	return t
}

// postTerm posts the events of terms[key] to the loop.
func (a *app) postTerm(key string) func(pane, vaxis.Event) {
	return func(t pane, ev vaxis.Event) { a.vx.PostEvent(termEvent{key, t, ev}) }
}

// attachRunning shows the sessions the server runs that cade has no pane for:
// at start, and ones another cade started since.
func (a *app) attachRunning() {
	ids, ver := running()
	if ver != "" && ver != version && !a.verWarned {
		a.verWarned = true
		a.notify(toastWarn, "Session server is v"+ver+": restart it to run v"+version+" (ends its sessions)")
	}
	for _, id := range ids {
		if a.terms[id] != nil {
			continue
		}
		w, h := a.paneSize()
		if t, err := openRemote(id, nil, w, h, a.postTerm(id)); err == nil {
			a.terms[id] = t
		}
	}
}

// show puts terms[key] in the pane and focuses it; a split shell or editor moves
// under session key, a full-height one is hidden.
func (a *app) show(key string) {
	if key != a.low && a.low != "" {
		if *a.lowFull(a.low) {
			a.low = ""
		} else {
			a.back = key
		}
	}
	if prev := a.terms[a.active]; prev != nil && a.active != key {
		prev.Blur()
	}
	a.active = key
	a.setFocus(true)
}

// toggleShell shows the app's one $SHELL, see toggleLow.
func (a *app) toggleShell() {
	a.toggleLow(shellID, exec.Command(cmp.Or(os.Getenv("SHELL"), "sh")), &a.shellDir)
}

// toggleEditor shows the app's one editor (editor.cmd), see toggleLow.
func (a *app) toggleEditor() {
	// through sh, like editConfig, so a command with flags ("nvim .") works
	a.toggleLow(editorID, exec.Command("sh", "-c", a.editorCmd()), &a.editDir)
}

// toggleLow shows the shell or editor key under the pane shown before (see
// paneTerms), in place of the other, starting cmd in the cursor's project dir,
// noted in dir, if needed; when shown it hides it, still running.
func (a *app) toggleLow(key string, cmd *exec.Cmd, dir *string) {
	a.diff, a.upFull = nil, false
	if a.low == key {
		a.hideLow()
		return
	}
	if _, ok := a.terms[key]; !ok {
		p := a.projects[a.current().p]
		t := a.newTerm(key)
		cmd.Dir = p.Dir
		cmd.Env = append(os.Environ(), "PWD="+p.Dir)
		w, h := a.paneSize()
		if err := t.StartWithSize(cmd, w, h); err != nil {
			a.fail(key+": ", err)
			return
		}
		a.terms[key] = t
		*dir = p.Dir
	}
	if a.low == "" { // in place of the other, it keeps the session it was under
		a.back = a.active
	}
	a.low = key
	a.show(key)
}

// hideLow hides the shell or editor, still running, back to the session it was
// under, or the empty pane if there was none.
func (a *app) hideLow() {
	key := a.low
	a.low = ""
	if a.active != key {
		return
	}
	if a.terms[a.back] != nil {
		a.show(a.back)
	} else {
		a.setFocus(false)
		a.active = ""
	}
}

// lowFull is the remembered F11 state of the shell or editor key.
func (a *app) lowFull(key string) *bool {
	if key == editorID {
		return &a.st.EditorFull
	}
	return &a.st.ShellFull
}

// splitSize is the size of the diff, shell or editor key in % of the pane
// (diff.height, shell.height or editor.height) and its config.yaml section.
func (a *app) splitSize(key string) (*int, string) {
	switch key {
	case diffID:
		return &a.cfg.Diff.Height, "diff"
	case editorID:
		return &a.cfg.Editor.Height, "editor"
	}
	return &a.cfg.Shell.Height, "shell"
}

// resizeSplit grows (dir 1) or shrinks (dir -1) the diff, shell or editor split
// with the session, saved to config.yaml.
func (a *app) resizeSplit(dir int) {
	key, _, _, _ := a.lower()
	h, section := a.splitSize(key)
	a.stepSplit(dir, h, section)
}

// toggleCols puts the diff, shell or editor beside the session or under it, remembered.
func (a *app) toggleCols() {
	a.st.SplitCols = !a.st.SplitCols
	a.saveState()
}

// toggleFull makes the focused window fill the pane and back: the session, the
// diff, shell or editor staying open behind it, or the diff, shell or editor,
// remembered, which it focuses.
func (a *app) toggleFull() {
	if a.upFull || a.split() && !a.diffFocused() && a.active != a.low {
		a.upFull = !a.upFull
		return
	}
	if a.diff != nil {
		a.st.DiffFull = !a.st.DiffFull
		a.saveState()
		a.focusLower()
		return
	}
	f := a.lowFull(a.low)
	*f = !*f
	a.saveState()
	a.show(a.low)
}

// focusUpper focuses the session above the split diff, shell or editor.
func (a *app) focusUpper() {
	if a.diff != nil {
		a.diff.focus = false
		a.setFocus(true)
		return
	}
	a.show(a.back)
}

// focusLower focuses the diff, shell or editor.
func (a *app) focusLower() {
	if a.diff != nil {
		a.setFocus(false)
		a.diff.focus = true
		return
	}
	a.show(a.low)
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
		if id == a.low {
			a.low = ""
		}
		if a.active == id {
			a.active, a.inTerm, a.upFull = "", false, false
			switch {
			case a.low != "": // the session above the shell or editor ended: it fills the pane
				a.active = a.low
			case (id == shellID || id == editorID) && a.terms[a.back] != nil:
				a.active = a.back
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
	w, h := a.screen()
	key := a.active
	if a.diffFocused() {
		key = diffID
	}
	ps := a.paneTerms(w, h)
	p := ps[0]
	for _, q := range ps {
		if q.key == key {
			p = q
		}
	}
	return max(p.w-barW, 1), max(p.h, 1) // scrollbar
}

// paneTerm is a term or the diff on screen: its key in terms or diffID, and its
// first column, first row under its title bar, width (scrollbar included) and height.
type paneTerm struct {
	key        string
	x, y, w, h int
}

// paneTerms lays out the pane on a screen w × h: the session, and the diff,
// shell or editor split with it, which takes diff.height, shell.height or
// editor.height % of the pane: beside it after a line (F6, see sideBySide), else
// under it; else the diff or the active term alone.
func (a *app) paneTerms(w, h int) []paneTerm {
	x, end := a.paneSpan(w)
	pw, rows := end-x, h-2*titleH
	lower, upper, pct, _ := a.lower()
	if !a.split() || rows < 2 {
		if lower == diffID && !(a.upFull && a.terms[upper] != nil) {
			return []paneTerm{{diffID, x, titleH, pw, h - titleH}}
		}
		return []paneTerm{{a.active, x, titleH, pw, h - titleH}}
	}
	if a.st.SplitCols && sideBySide(pw) {
		lw := min(max((pw-1)*pct/100, splitMinW), pw-1-splitMinW)
		return []paneTerm{{upper, x, titleH, pw - 1 - lw, h - titleH}, {lower, end - lw, titleH, lw, h - titleH}}
	}
	sh := min(max(rows*pct/100, 1), rows-1)
	return []paneTerm{{upper, x, titleH, pw, rows - sh}, {lower, x, h - sh, pw, sh}}
}

// sideBySide reports whether a pane pw wide fits two terms splitMinW wide and the line between.
func sideBySide(pw int) bool { return pw >= 2*splitMinW+1 }

// lower is what can split the pane with a session: the diff, else the shell or
// editor when shown; upper is that session, pct its size and full whether F11
// made it fill the pane.
func (a *app) lower() (key, upper string, pct int, full bool) {
	switch {
	case a.diff != nil:
		key, upper, full = diffID, a.active, a.st.DiffFull
	case a.low != "":
		key, upper, full = a.low, a.back, *a.lowFull(a.low)
	default:
		return "", "", 0, false
	}
	h, _ := a.splitSize(key)
	return key, upper, splitPct(*h), full
}

// split reports whether the diff, shell or editor shares the pane with a session.
func (a *app) split() bool {
	key, upper, _, full := a.lower()
	return key != "" && !full && !a.upFull && a.terms[upper] != nil
}

// onSplitLine reports whether col, row of a screen w × h is on the line between
// the session and the diff, shell or editor: the column between them side by
// side, else the lower one's title bar.
func (a *app) onSplitLine(col, row, w, h int) bool {
	ps := a.paneTerms(w, h)
	if !a.split() || len(ps) < 2 {
		return false
	}
	lo := ps[1]
	if lo.y == ps[0].y {
		return col == lo.x-1 && row < h
	}
	return row == lo.y-titleH && col >= lo.x && col < lo.x+lo.w
}

// dragSplit moves the split line to col, row of a screen w × h, saving the size
// to config.yaml once the drag ends.
func (a *app) dragSplit(col, row, w, h int) {
	key, _, _, _ := a.lower()
	if key == "" {
		a.dragging = false
		return
	}
	size, section := a.splitSize(key)
	pct := pctOf(h-1-row, h-2*titleH) // the lower one's title bar is on row
	if x, end := a.paneSpan(w); a.st.SplitCols && sideBySide(end-x) {
		pct = pctOf(end-1-col, end-x-1) // the line is on col
	}
	*size = min(max(pct, splitMin), splitMax)
	if !a.dragging {
		a.fail("save failed: ", saveConfig(configPath(), *size, section, "height"))
	}
}

// pctOf is n in % of of, rounded.
func pctOf(n, of int) int { return (n*100 + of/2) / max(of, 1) }

func (a *app) lowerShown() bool  { return a.diff != nil || a.low != "" }
func (a *app) diffFocused() bool { return a.diff != nil && a.diff.focus }

// sideShown reports whether the sidebar is on screen: zooming hides it while the
// pane or diff has focus. Focusing the sidebar shows it again until the pane has focus back.
func (a *app) sideShown() bool { return !a.zoom && !a.mobile || (!a.inTerm && !a.diffFocused()) }

// sheet reports whether the sidebar covers the whole screen (--mobile).
func (a *app) sheet() bool { return a.mobile && a.sideShown() }

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
	case !a.sideShown() || a.mobile: // mobile: the pane keeps the full width under the sheet
		return 0, w
	case a.st.SidebarRight:
		return 0, w - a.sideW() - 1
	}
	return a.sideW() + 1, w
}

// sideW is the sidebar's width, 0 in state.json being the default.
func (a *app) sideW() int {
	if a.mobile {
		w, _ := a.vx.Window().Size()
		return w
	}
	return cmp.Or(a.st.SidebarW, sideDef)
}

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
	if a.mobile { // sideW is the screen's width there; keep the remembered one
		return
	}
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
		if a.cursorTo(a.attn[0]) {
			a.open()
			return
		}
		a.clearAttn(a.attn[0]) // session gone from disk and list
	}
	a.notify(toastNotice, "No session waiting")
}

// cursorTo puts the cursor on session id, expanding its project; it reports
// whether the session is listed.
func (a *app) cursorTo(id string) bool {
	for pi, p := range a.projects {
		if si := slices.IndexFunc(p.sessions, func(s session) bool { return s.ID == id }); si >= 0 {
			p.Collapsed = false
			a.cursor = slices.Index(a.rows(), row{pi, si})
			return true
		}
	}
	return false
}

// visit moves session id to the front of the remembered history.
func (a *app) visit(id string) {
	r := slices.DeleteFunc(a.st.Recent, func(s string) bool { return s == id })
	a.st.Recent = slices.Insert(r, 0, id)[:min(len(r)+1, recentMax)]
	a.saveState()
}

// stepRecent moves the cursor to the session opened before the one under it,
// wrapping from the oldest to the latest and skipping sessions no longer listed;
// off the history it goes to the latest.
func (a *app) stepRecent() {
	at := -1
	if r := a.current(); r.s >= 0 {
		at = slices.Index(a.st.Recent, a.projects[r.p].sessions[r.s].ID)
	}
	for n := 1; n <= len(a.st.Recent); n++ {
		if i := (at + n) % len(a.st.Recent); i != at && a.cursorTo(a.st.Recent[i]) {
			return
		}
	}
	a.notify(toastNotice, "No other session")
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

// editorCmd is the F4 editor's command: editor.cmd, else envEditor.
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

// toggleDensity switches the sidebar between compact and comfortable, saving it to config.yaml.
func (a *app) toggleDensity() {
	v := "comfortable"
	if a.rowH() > 1 {
		v = "compact"
	}
	if err := saveConfig(configPath(), v, "sidebar", "density"); err != nil {
		a.fail("save failed: ", err)
		return
	}
	a.cfg.Sidebar.Density = v
}

// toggleWrap flips sidebar.wrap, saving it to config.yaml.
func (a *app) toggleWrap() {
	if err := saveConfig(configPath(), !a.cfg.Sidebar.Wrap, "sidebar", "wrap"); err != nil {
		a.fail("save failed: ", err)
		return
	}
	a.cfg.Sidebar.Wrap = !a.cfg.Sidebar.Wrap
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

// stepSplit changes section.height (shell, editor or diff), pointed to by h, by dir
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
