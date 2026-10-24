package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// config is config.yaml, the file the user edits; cade never writes it.
type config struct {
	Theme  string             `yaml:"theme"`
	Themes map[string]palette `yaml:"themes"` // user themes; empty fields inherit from dracula
	Claude struct {
		Default  string             `yaml:"default"` // may be left out when there is one account
		Accounts map[string]account `yaml:"accounts"`
	} `yaml:"claude"`
}

// account is one Claude login: how claude is started and where it keeps sessions.
// Values may use ~, $VAR and $PWD (the project directory).
type account struct {
	Command   []string          `yaml:"command"`   // program and flags; cade appends --session-id/--resume. Default: claude
	ConfigDir string            `yaml:"configDir"` // passed as CLAUDE_CONFIG_DIR. Default: ~/.claude
	Env       map[string]string `yaml:"env"`
	Dirs      []string          `yaml:"dirs"` // projects in these directories use this account
}

func configPath() string { return filepath.Join(filepath.Dir(projectsPath()), "config.yaml") }

// loadConfig reads config.yaml; a missing file means defaults. Unlike a bad
// theme, a bad file is an error: running claude without its sandbox or with
// the wrong account must not happen quietly.
func loadConfig(path string) (config, error) {
	var cfg config
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true) // a typo like "comand" would silently start plain claude
	if err := dec.Decode(&cfg); err != nil && err != io.EOF {
		return cfg, err
	}
	c := &cfg.Claude
	if c.Default == "" && len(c.Accounts) == 1 {
		for name := range c.Accounts {
			c.Default = name
		}
	}
	if _, ok := c.Accounts[c.Default]; len(c.Accounts) > 0 && !ok {
		return cfg, fmt.Errorf("claude.default: no account %q", c.Default)
	}
	return cfg, nil
}

// account picks the account whose dirs hold dir most specifically, else the default.
func (cfg config) account(dir, home string) account {
	best, n := cfg.Claude.Accounts[cfg.Claude.Default], -1
	for _, acc := range cfg.Claude.Accounts {
		for _, d := range acc.Dirs {
			d = filepath.Clean(expand(d, home, dir))
			if (dir == d || strings.HasPrefix(dir, d+"/")) && len(d) > n {
				best, n = acc, len(d)
			}
		}
	}
	return best
}

// argv is the command without session flags, expanded for project dir.
func (acc account) argv(home, dir string) []string {
	if len(acc.Command) == 0 {
		return []string{"claude"}
	}
	out := make([]string, len(acc.Command))
	for i, s := range acc.Command {
		out[i] = expand(s, home, dir)
	}
	return out
}

// sessionsRoot is the directory holding projects/<dir>/<id>.jsonl for this account.
func (acc account) sessionsRoot(home string) string {
	if acc.ConfigDir == "" {
		return filepath.Join(home, ".claude")
	}
	return expand(acc.ConfigDir, home, "")
}

func (acc account) cmd(home, dir string, args ...string) *exec.Cmd {
	argv := acc.argv(home, dir)
	cmd := exec.Command(argv[0], append(argv[1:], args...)...)
	cmd.Dir = dir
	// Go only sets PWD itself when Env is nil; wrapper scripts rely on it.
	cmd.Env = append(os.Environ(), "PWD="+dir)
	if acc.ConfigDir != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_CONFIG_DIR="+acc.sessionsRoot(home))
	}
	for k, v := range acc.Env {
		cmd.Env = append(cmd.Env, k+"="+expand(v, home, dir))
	}
	return cmd
}

// expand resolves a leading ~ and $VAR/${VAR}, with $PWD as the project dir.
func expand(s, home, dir string) string {
	if s == "~" || strings.HasPrefix(s, "~/") {
		s = home + s[1:]
	}
	return os.Expand(s, func(k string) string {
		if k == "PWD" && dir != "" {
			return dir
		}
		return os.Getenv(k)
	})
}
