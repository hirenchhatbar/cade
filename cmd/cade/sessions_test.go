package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestLoadSessions(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/u/my.proj"
	dir := projectDir(home, cwd)
	if filepath.Base(dir) != "-home-u-my-proj" {
		t.Fatalf("projectDir = %s", dir)
	}
	os.MkdirAll(dir, 0o755)
	write := func(name string, lines ...string) {
		os.WriteFile(filepath.Join(dir, name+".jsonl"), []byte(strings.Join(lines, "\n")), 0o644)
	}
	write("ai", `{"type":"user","message":{"content":"hi"}}`, `{"type":"ai-title","aiTitle":"old"}`, `{"type":"ai-title","aiTitle":"fix login"}`)
	write("custom", `{"type":"ai-title","aiTitle":"x"}`, `{"type":"custom-title","customTitle":"mine"}`, `{"type":"agent-name","agentName":"agent"}`)
	write("agent", `{"type":"ai-title","aiTitle":"x"}`, `{"type":"agent-name","agentName":"agent"}`)
	write("prompt", `{"type":"user","isMeta":true,"message":{"content":"meta"}}`,
		`{"type":"user","message":{"content":"<local-command-caveat>x"}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"Hello,\n  project."}]}}`)
	write("empty", `{"type":"mode"}`)

	got := map[string]string{}
	for _, s := range loadSessions(home, cwd) {
		got[s.ID] = s.Title
	}
	want := map[string]string{"ai": "fix login", "custom": "mine", "agent": "agent", "prompt": "Hello, project.", "empty": "untitled"}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: got %q want %q", id, got[id], w)
		}
	}
}

func TestNewUUID(t *testing.T) {
	ok := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if id := newUUID(); !ok.MatchString(id) || id == newUUID() {
		t.Fatalf("bad uuid %s", id)
	}
}

func TestFirstPrompts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(path, []byte(strings.Join([]string{
		`{"type":"user","isMeta":true,"message":{"content":"meta"}}`,
		`{"type":"user","message":{"content":"fix the login"}}`,
		`{"type":"assistant","message":{"content":"ok"}}`,
		`{"type":"user","message":{"content":"<command-name>/clear"}}`,
		`{"type":"user","message":{"content":"and add tests"}}`,
		`{"type":"user","message":{"content":"third"}}`,
	}, "\n")), 0o644)
	if got := firstPrompts(path, 2, 7); !slices.Equal(got, []string{"fix the", "and add"}) {
		t.Fatalf("got %q", got)
	}
}
