package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type project struct {
	Name      string            `json:"name"`
	Dir       string            `json:"dir"`
	Collapsed bool              `json:"collapsed,omitempty"`
	Titles    map[string]string `json:"titles,omitempty"`  // session ID -> title set in cade, wins over Claude's
	Starred   []string          `json:"starred,omitempty"` // starred session IDs, listed first
	sessions  []session         // loaded from ~/.claude/projects, not persisted
}

// arrange applies cade's titles and moves starred sessions to the top,
// keeping the shown order within both groups.
func (p *project) arrange() {
	var starred, rest []session
	for _, s := range p.sessions {
		if t := p.Titles[s.ID]; t != "" {
			s.Title = t
		}
		if slices.Contains(p.Starred, s.ID) {
			starred = append(starred, s)
		} else {
			rest = append(rest, s)
		}
	}
	p.sessions = append(starred, rest...)
}

// forget drops cade's title and star for a deleted session.
func (p *project) forget(id string) {
	delete(p.Titles, id)
	p.Starred = slices.DeleteFunc(p.Starred, func(s string) bool { return s == id })
}

func projectsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "cade", "projects.json")
}

func loadProjects(path string) ([]*project, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ps []*project
	return ps, json.Unmarshal(b, &ps)
}

// saveProjects writes via a temp file so a crash can't leave a half-written list.
func saveProjects(path string, ps []*project) error { return writeJSON(path, ps) }

// state is UI state cade remembers between runs (kept apart from the
// hand-edited config.yaml, which cade only writes from the settings dialog).
type state struct {
	HideHints    bool     `json:"hideHints"`
	UnifiedDiff  bool     `json:"unifiedDiff"`      // F3 overlay; side by side otherwise
	SidebarRight bool     `json:"sidebarRight"`     // left otherwise
	SidebarW     int      `json:"sidebarWidth"`     // columns; 0 = sideDef
	ShellFull    bool     `json:"shellFull"`        // F11: shell fills the pane; split under the session otherwise
	DiffFull     bool     `json:"diffFull"`         // F11 in the diff, like ShellFull
	Recent       []string `json:"recent,omitempty"` // opened session IDs, latest first; the first gets the cursor at startup
}

func statePath() string { return filepath.Join(filepath.Dir(projectsPath()), "state.json") }

func (a *app) saveState() { a.fail("save failed: ", writeJSON(statePath(), a.st)) }

func loadState(path string) state {
	var st state
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &st) // a broken file just means defaults
	}
	return st
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, b)
}

// writeFile writes via a temp file so a crash can't leave a half-written file.
// An existing file keeps its mode: config.yaml may hold secrets in env.
func writeFile(path string, b []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// subdirs lists the directory names in dir (symlinks to directories too), sorted;
// dot-directories only when hidden. An unreadable dir lists what it can, never nil.
func subdirs(dir string, hidden bool) []string {
	es, _ := os.ReadDir(dir)
	out := []string{}
	for _, e := range es {
		name := e.Name()
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			out = append(out, name)
		} else if st, err := os.Stat(filepath.Join(dir, name)); err == nil && e.Type()&os.ModeSymlink != 0 && st.IsDir() {
			out = append(out, name)
		}
	}
	return out
}

// expandDir resolves user input (~, relative paths) to an existing absolute directory.
func expandDir(in, home string) (string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", errors.New("directory is required")
	}
	if in == "~" || strings.HasPrefix(in, "~/") {
		in = filepath.Join(home, in[1:])
	}
	dir, err := filepath.Abs(in)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(dir)
	if err != nil {
		return "", errors.New("no such directory")
	}
	if !st.IsDir() {
		return "", errors.New("not a directory")
	}
	return dir, nil
}
