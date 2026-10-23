package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cade", "projects.json")
	if ps, err := loadProjects(path); err != nil || ps != nil {
		t.Fatalf("missing file: %v %v", ps, err)
	}
	in := []*project{{Name: "a", Dir: "/x"}, {Name: "b", Dir: "/y"}}
	if err := saveProjects(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := loadProjects(path)
	if err != nil || len(out) != 2 || out[1].Name != "b" || out[1].Dir != "/y" {
		t.Fatalf("got %v %v", out, err)
	}
}

func TestExpandDir(t *testing.T) {
	home := t.TempDir()
	os.Mkdir(filepath.Join(home, "proj"), 0o755)
	os.WriteFile(filepath.Join(home, "file"), nil, 0o644)
	if d, err := expandDir(" ~/proj ", home); err != nil || d != filepath.Join(home, "proj") {
		t.Errorf("~/proj: %q %v", d, err)
	}
	for _, bad := range []string{"", "~/missing", "~/file"} {
		if _, err := expandDir(bad, home); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}
