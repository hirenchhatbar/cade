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
	version  = "0.1.0"
	appLabel = appName + " v" + version
	sideW    = 42
	barW     = 2        // scrollbar columns
	focusKey = "Escape" // shortcut: hardcoded, make configurable once the key choice settles
	focusLbl = "Esc"
	focusAlt = "F12"
	// Esc leaves the pane, so Ctrl+Esc sends Claude its Esc; it needs the kitty keyboard protocol.
	escKey    = "Ctrl+Escape"
	escLbl    = "Ctrl+Esc"
	diffKey   = "F3"
	shellKey  = "F2"
	waitKey   = "Ctrl+f"
	waitLbl   = "Ctrl+F"
	shellID   = "shell:" // + project dir: the project's shell in terms
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
}

// settings is the , dialog. issues are what checkConfig found after the last
// $EDITOR run; the config in use then is still the one from before the edit.
type settings struct {
	sel    int // highlighted row, see settingRows
	issues []string
}

// confirmation is a pending yes/no question; yes runs on y.
type confirmation struct {
	lines []string
	yes   func()
}

type app struct {
	tree
	vx      *vaxis.Vaxis
	home    string
	cwd     string
	cfgPath string
	cfg     config
	th      theme
	st      state           // remembered UI state
	pending map[string]bool // started from cade, jsonl may not exist yet
	terms   map[string]*term.Model
	active  string   // key into terms
	back    string   // pane shown before the shell, which F2 returns to
	inTerm  bool     // keyboard focus on the Claude pane
	attn    []string // sessions that rang for attention, oldest first

	// transient UI state
	input   *prompt       // non-nil while adding a project
	rename  *renaming     // non-nil while renaming a session
	search  *searching    // non-nil while searching sessions
	confirm *confirmation // non-nil while asking yes/no
	set     *settings     // non-nil while the settings dialog is open
	diff    *diffView     // non-nil while the git diff covers the pane
	help    bool          // shortcut dialog open
	msg     string        // last error, shown above the hints
	dWait   bool          // a first d was pressed, see ddWindow
	dSeq    int
	selSeq  int // bumped on every pane click, see clearSel
	quit    bool
}

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
		a.msg = "config.json is no longer read: rename it to config.yaml"
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
		if quit := a.handle(ev); quit {
			return
		}
		a.draw()
	}
}

// handle routes one event; it reports whether cade should quit.
func (a *app) handle(ev vaxis.Event) bool {
	switch ev := ev.(type) {
	case vaxis.Key:
		return a.key(ev)
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
	case clearSel:
		if ev.seq == a.selSeq {
			ev.t.ClearSelection()
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

// fail shows err (with an optional prefix) above the hints; nil clears nothing.
func (a *app) fail(prefix string, err error) {
	if err != nil {
		a.msg = prefix + err.Error()
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

// show puts terms[key] in the pane and focuses it.
func (a *app) show(key string) {
	if prev := a.terms[a.active]; prev != nil && a.active != key {
		prev.Blur()
	}
	a.active = key
	a.setFocus(true)
}

// toggleShell shows a $SHELL in the cursor's project dir over the pane, starting
// it if needed; on that shell it hides it, still running, back to the pane shown
// before, or the empty pane if there was none.
func (a *app) toggleShell() {
	a.diff = nil
	p := a.projects[a.current().p]
	key := shellID + p.Dir
	if a.active == key {
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
	}
	a.back = a.active
	a.show(key)
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
}

func (a *app) removeProject(p *project) {
	for _, s := range p.sessions {
		a.closeTerm(s.ID)
	}
	a.closeTerm(shellID + p.Dir)
	a.projects = slices.DeleteFunc(a.projects, func(q *project) bool { return q == p })
	a.save()
	a.restore(rowKey{})
}

func (a *app) account(p *project) account { return a.cfg.account(p.Dir, a.home) }

// root is where p's account keeps its sessions.
func (a *app) root(p *project) string { return a.account(p).sessionsRoot(a.home) }

func (a *app) closeTerm(id string) {
	if t := a.terms[id]; t != nil {
		t.Close()
		delete(a.terms, id)
		a.clearAttn(id)
		if a.active == id {
			a.active, a.inTerm = "", false
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

func (a *app) paneSize() (int, int) {
	w, h := a.vx.Window().Size()
	return max(w-sideW-1-barW, 1), max(h-1, 1) // separator, scrollbar, title bar
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
	a.msg = "no session waiting"
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
	cmd := exec.Command("sh", "-c", cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")+` "$1"`, "sh", path)
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
	a.msg = "config.yaml reloaded"
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
	if err := saveTheme(configPath(), name); err != nil {
		a.fail("save failed: ", err)
		return
	}
	a.cfg.Theme = name
	var err error
	a.th, err = loadTheme(a.cfg)
	a.fail("", err)
}
