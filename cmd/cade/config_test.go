package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	write := func(s string) { os.WriteFile(path, []byte(s), 0o644) }
	home := "/h"

	cfg, err := loadConfig(path)
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
	if cfg, err = loadConfig(path); err != nil {
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
	if cfg, err = loadConfig(path); err != nil || cfg.account("/p", home).argv(home, "/p")[0] != "x" {
		t.Fatalf("single account should be the default: %v", err)
	}
	for _, bad := range []string{
		"claude:\n  default: nope\n  accounts:\n    a: {}\n",
		"claude:\n  accounts:\n    a: {}\n    b: {}\n", // two accounts, no default
		"claude:\n  accounts:\n    a: {comand: [x]}\n",
		"theme: [",
	} {
		write(bad)
		if _, err := loadConfig(path); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}
