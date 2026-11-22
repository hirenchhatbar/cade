package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
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

func TestProjectColors(t *testing.T) {
	if c, _ := hexLab("#ffffff"); math.Abs(c.l-1) > 1e-3 || math.Abs(c.a) > 1e-3 {
		t.Fatalf("white in Oklab: %+v", c)
	}
	var ps []*project
	for i := range 50 {
		ps = append(ps, &project{Dir: fmt.Sprintf("/p%d", i)})
		if !fillColors(ps) || fillColors(ps) {
			t.Fatal("fillColors should fill once, then report no change")
		}
	}
	first := ps[0].Color
	minD := math.Inf(1)
	for i, p := range ps {
		ci, _ := hexLab(p.Color)
		for _, q := range ps[i+1:] {
			cj, _ := hexLab(q.Color)
			minD = min(minD, ci.dist(cj))
		}
	}
	t.Logf("50 projects: closest pair %.3f apart", minD)
	if minD < 0.02 { // about the smallest difference the eye sees
		t.Errorf("50 projects: closest pair %.3f apart", minD)
	}
	if ps[0].Color != first {
		t.Error("an earlier project's color changed")
	}
	p := ps[0]
	for range int(360 / hueStep) {
		stepHue(p, 1)
	}
	a, _ := hexLab(first)
	if b, _ := hexLab(p.Color); a.dist(b) > 0.01 {
		t.Errorf("once round the wheel: %s, want about %s", p.Color, first)
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

func TestArrange(t *testing.T) {
	p := &project{Titles: map[string]string{"b": "mine"}, Starred: []string{"d", "b"},
		sessions: []session{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}, {ID: "c", Title: "C"}, {ID: "d", Title: "D"}}}
	p.arrange()
	var got []string
	for _, s := range p.sessions {
		got = append(got, s.ID+"="+s.Title)
	}
	if want := "b=mine d=D a=A c=C"; strings.Join(got, " ") != want {
		t.Fatalf("got %v want %s", got, want)
	}
	p.forget("b")
	if len(p.Titles) != 0 || len(p.Starred) != 1 {
		t.Fatalf("forget left %v %v", p.Titles, p.Starred)
	}
}
