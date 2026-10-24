package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	newRowLbl = "[n] new session"
	selLinger = 600 * time.Millisecond // selection stays highlighted this long after a copy
	ddWindow  = 400 * time.Millisecond // a second d within this means dd
)

type refreshEvent struct{}

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

// renaming is the rename-session dialog.
type renaming struct {
	p     *project
	id    string
	title string
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
	active  string // key into terms
	inTerm  bool   // keyboard focus on the Claude pane

	// transient UI state
	input   *prompt       // non-nil while adding a project
	rename  *renaming     // non-nil while renaming a session
	confirm *confirmation // non-nil while asking yes/no
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
	cfg, err := loadConfig(configPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "cade: %s: %v\n", configPath(), err)
		os.Exit(1)
	}
	accs := []account{{}} // plain claude when no accounts are configured
	if len(cfg.Claude.Accounts) > 0 {
		accs = slices.Collect(maps.Values(cfg.Claude.Accounts))
	}
	for _, acc := range accs {
		bin := acc.argv(home, cwd)[0]
		if _, err := exec.LookPath(bin); err != nil {
			fmt.Fprintf(os.Stderr, "cade: %s not found in PATH\n", bin)
			os.Exit(1)
		}
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
  -k, --keys     print all keyboard shortcuts as Markdown
  -v, --version  print the version

Files in ` + filepath.Dir(projectsPath()) + `:
  projects.json  projects, their order and collapsed state, session titles and stars (written by cade)
  state.json     remembered UI state (written by cade)
  config.yaml    theme and claude accounts (edited by you)
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
		for id, t := range a.terms {
			if t == ev.Term {
				a.closeTerm(id)
				delete(a.pending, id)
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
	if _, ok := a.terms[key]; !ok {
		t := term.New(term.WithVaxis(a.vx))
		t.Attach(a.vx.PostEvent)
		cmd := a.account(p).cmd(a.home, p.Dir, args...)
		w, h := a.paneSize()
		if err := t.StartWithSize(cmd, w, h); err != nil {
			a.fail("claude: ", err)
			return
		}
		a.terms[key] = t
	}
	if prev := a.terms[a.active]; prev != nil && a.active != key {
		prev.Blur()
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
		if a.active == id {
			a.active, a.inTerm = "", false
		}
	}
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
