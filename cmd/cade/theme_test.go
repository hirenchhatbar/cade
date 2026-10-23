package main

import (
	"os"
	"path/filepath"
	"testing"

	"go.rockorager.dev/vaxis"
)

func TestLoadTheme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	dracula := mustTheme(themes["dracula"])

	if th, err := loadTheme(path); err != nil || th != dracula {
		t.Fatalf("missing config: want dracula, err %v", err)
	}
	write := func(s string) { os.WriteFile(path, []byte(s), 0o644) }

	write(`{"theme":"terminal"}`)
	if th, err := loadTheme(path); err != nil || th.sel.Attribute&vaxis.AttrReverse == 0 {
		t.Fatalf("terminal theme: %v", err)
	}

	write(`{"theme":"mine","themes":{"mine":{"key":"#00ff00"}}}`)
	th, err := loadTheme(path)
	if err != nil || th.key.Foreground != vaxis.RGBColor(0, 255, 0) || th.base != dracula.base {
		t.Fatalf("custom theme should override key and inherit the rest: %v", err)
	}

	for _, bad := range []string{`{"theme":"nope"}`, `{"theme":"x","themes":{"x":{"fg":"red"}}}`, `{`} {
		write(bad)
		if th, err := loadTheme(path); err == nil || th != dracula {
			t.Errorf("%s: want error and dracula fallback", bad)
		}
	}
}
