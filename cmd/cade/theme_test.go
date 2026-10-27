package main

import (
	"os"
	"path/filepath"
	"testing"

	"go.rockorager.dev/vaxis"
)

func TestBuiltinThemesBuild(t *testing.T) {
	for name, p := range themes {
		if _, err := p.build(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestLoadTheme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	dracula := mustTheme(themes["dracula"])
	load := func() (theme, error) {
		cfg, err := loadConfig(t, path)
		if err != nil {
			return dracula, err
		}
		return loadTheme(cfg)
	}

	if th, err := load(); err != nil || th != dracula {
		t.Fatalf("missing config: want dracula, err %v", err)
	}
	write := func(s string) { os.WriteFile(path, []byte(s), 0o644) }

	write(`{"theme":"terminal"}`)
	if th, err := load(); err != nil || th.sel.Attribute&vaxis.AttrReverse == 0 {
		t.Fatalf("terminal theme: %v", err)
	}

	write(`{"theme":"mine","themes":{"mine":{"key":"#00ff00"}}}`)
	th, err := load()
	if err != nil || th.key.Foreground != vaxis.RGBColor(0, 255, 0) || th.base != dracula.base {
		t.Fatalf("custom theme should override key and inherit the rest: %v", err)
	}

	for _, bad := range []string{`{"theme":"nope"}`, `{"theme":"x","themes":{"x":{"fg":"red"}}}`, `{`} {
		write(bad)
		if th, err := load(); err == nil || th != dracula {
			t.Errorf("%s: want error and dracula fallback", bad)
		}
	}
}
