package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	write := func(s string) { os.WriteFile(path, []byte(s), 0o644) }
	home := "/h"

	cfg, err := loadConfig(t, path)
	if err != nil {
		t.Fatal(err)
	}
	plain := cfg.account("/p", home)
	if got := plain.cmd(home, "/p", "--resume", "x").Args; !slices.Equal(got, []string{"claude", "--resume", "x"}) {
		t.Fatalf("no config: %v", got)
	}
	if plain.sessionsRoot(home) != "/h/.claude" {
		t.Fatal("default sessions root")
	}

	write(`
claude:
  default: me
  accounts:
    me:
      command: [firejail, --read-write=$PWD, --profile=$HOME/x.profile, claude]
      env: {BASE: "$PWD/u"}
    work:
      configDir: ~/.claude-work
      dirs: [~/www/a, /srv/b/]
`)
	t.Setenv("HOME", home)
	if cfg, err = loadConfig(t, path); err != nil {
		t.Fatal(err)
	}
	cmd := cfg.account("/p", home).cmd(home, "/p", "--session-id", "x")
	if want := []string{"firejail", "--read-write=/p", "--profile=/h/x.profile", "claude", "--session-id", "x"}; !slices.Equal(cmd.Args, want) {
		t.Fatalf("args %v", cmd.Args)
	}
	if !slices.Contains(cmd.Env, "BASE=/p/u") || !slices.Contains(cmd.Env, "PWD=/p") || cmd.Dir != "/p" {
		t.Fatalf("env/dir %v %s", cmd.Env[len(cmd.Env)-2:], cmd.Dir)
	}
	for dir, root := range map[string]string{"/h/www/a": "/h/.claude-work", "/h/www/a/x": "/h/.claude-work", "/srv/b": "/h/.claude-work", "/h/www/ab": "/h/.claude"} {
		if got := cfg.account(dir, home).sessionsRoot(home); got != root {
			t.Errorf("%s: root %s want %s", dir, got, root)
		}
	}
	if !slices.Contains(cfg.account("/srv/b", home).cmd(home, "/srv/b").Env, "CLAUDE_CONFIG_DIR=/h/.claude-work") {
		t.Error("CLAUDE_CONFIG_DIR not passed")
	}

	write("claude:\n  accounts:\n    solo: {command: [x]}\n")
	if cfg, err = loadConfig(t, path); err != nil || cfg.account("/p", home).argv(home, "/p")[0] != "x" {
		t.Fatalf("single account should be the default: %v", err)
	}
	for _, bad := range []string{
		"claude:\n  default: nope\n  accounts:\n    a: {}\n",
		"claude:\n  accounts:\n    a: {}\n    b: {}\n", // two accounts, no default
		"claude:\n  accounts:\n    a: {comand: [x]}\n",
		"theme: [",
		"shell: {height: 95}\n",
		"diff: {height: 5}\n",
	} {
		write(bad)
		if _, err := loadConfig(t, path); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

// loadConfig is parseConfig with its issues as an error, as startup sees them.
func loadConfig(t *testing.T, path string) (config, error) {
	t.Helper()
	cfg, issues, err := parseConfig(path)
	if err == nil && len(issues) > 0 {
		err = errors.New(strings.Join(issues, "\n"))
	}
	return cfg, err
}

func TestCheckConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	write := func(s string) { os.WriteFile(path, []byte(s), 0o644) }
	bin := filepath.Join(dir, "fakeclaude")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)

	write("claude:\n  accounts:\n    a: {command: [" + bin + "], dirs: [" + dir + "]}\n")
	if _, issues, err := checkConfig(path, dir, dir); err != nil || len(issues) > 0 {
		t.Fatalf("clean config: %v %v", issues, err)
	}

	write("theme: nope\nclaude:\n  accounts:\n    a: {comand: [x], dirz: [y], command: [no-such-bin], configDir: /no/such}\n")
	_, issues, err := checkConfig(path, dir, dir)
	want := []string{
		`line 4: unknown key "comand" (did you mean "command"?)`,
		`line 4: unknown key "dirz" (did you mean "dirs"?)`,
		"claude.accounts.a.command: no-such-bin not found in PATH",
		`theme: unknown theme "nope"`,
		"claude.accounts.a.configDir: /no/such is not a directory",
	}
	if !slices.Equal(issues, want) {
		t.Fatalf("issues:\n%s", strings.Join(issues, "\n"))
	}
	if err == nil || strings.Contains(err.Error(), "theme") || strings.Contains(err.Error(), "configDir") {
		t.Fatalf("fatal error should hold the first three issues only: %v", err)
	}

	write("theme: [")
	if _, issues, err := checkConfig(path, dir, dir); err == nil || len(issues) != 1 {
		t.Fatalf("syntax error: %v %v", issues, err)
	}
}

func TestSaveConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := saveConfig(path, "terminal", "theme"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "theme: terminal\n" {
		t.Fatalf("new file: %q", b)
	}

	orig := "# my config\nclaude: # accounts\n  default: me\n  accounts:\n    me: {}\ntheme: dracula # dark\n"
	os.WriteFile(path, []byte(orig), 0o600)
	os.Chmod(path, 0o600)
	link := filepath.Join(dir, "link.yaml")
	os.Symlink(path, link)
	if err := saveConfig(link, "terminal", "theme"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if want := strings.Replace(orig, "dracula", "terminal", 1); string(b) != want {
		t.Fatalf("got\n%s\nwant\n%s", b, want)
	}
	if st, _ := os.Lstat(link); st.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink replaced by a file")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", st.Mode().Perm())
	}

	os.WriteFile(path, []byte("claude: {}\n"), 0o644)
	saveConfig(path, "x", "theme")
	if cfg, err := loadConfig(t, path); err != nil || cfg.Theme != "x" {
		t.Fatalf("added key: %v %q", err, cfg.Theme)
	}
	os.WriteFile(path, []byte("quit:\n"), 0o644)
	saveConfig(path, false, "quit", "confirm")
	if cfg, err := loadConfig(t, path); err != nil || cfg.Quit.Confirm == nil || *cfg.Quit.Confirm {
		t.Fatalf("nested key: %v %v", err, cfg.Quit.Confirm)
	}
}
