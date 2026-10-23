package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.rockorager.dev/vaxis"
)

// palette is a theme as hex colors ("#rrggbb"); "" means the terminal default.
type palette struct {
	Bg      string `json:"bg"`      // sidebar and title bar background
	Fg      string `json:"fg"`      // session titles
	Muted   string `json:"muted"`   // dirs, ids, hints, separator, scrollbar track
	Project string `json:"project"` // project names
	SelBg   string `json:"selBg"`   // cursor row; "" falls back to reverse video
	SelFg   string `json:"selFg"`
	Key     string `json:"key"`     // [shortcut] labels
	Running string `json:"running"` // ● running session mark
	Thumb   string `json:"thumb"`   // scrollbar thumb
	Error   string `json:"error"`
	TitleBg string `json:"titleBg"` // focused panel's title bar
	TitleFg string `json:"titleFg"`
}

var themes = map[string]palette{
	"dracula": {
		Bg: "#282a36", Fg: "#f8f8f2", Muted: "#6272a4", Project: "#bd93f9",
		SelBg: "#44475a", SelFg: "#f8f8f2", Key: "#ff79c6", Running: "#50fa7b",
		Thumb: "#bd93f9", Error: "#ff5555", TitleBg: "#bd93f9", TitleFg: "#282a36",
	},
	"terminal": {}, // terminal's own colors, attributes only
}

const defaultTheme = "dracula"

type config struct {
	Theme  string             `json:"theme"`
	Themes map[string]palette `json:"themes"` // user themes; empty fields inherit from dracula
}

func configPath() string { return filepath.Join(filepath.Dir(projectsPath()), "config.json") }

// loadTheme reads config.json and resolves the selected theme. On any problem it
// returns dracula plus an error to show, so a bad config never blocks startup.
func loadTheme(path string) (theme, error) {
	var cfg config
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return mustTheme(themes[defaultTheme]), err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return mustTheme(themes[defaultTheme]), fmt.Errorf("config.json: %w", err)
		}
	}
	name := cfg.Theme
	if name == "" {
		name = defaultTheme
	}
	p, ok := themes[name]
	if custom, isCustom := cfg.Themes[name]; isCustom {
		p, ok = inherit(custom, themes[defaultTheme]), true
	}
	if !ok {
		return mustTheme(themes[defaultTheme]), fmt.Errorf("unknown theme %q", name)
	}
	t, err := p.build()
	if err != nil {
		return mustTheme(themes[defaultTheme]), fmt.Errorf("theme %q: %w", name, err)
	}
	return t, nil
}

func inherit(p, base palette) palette {
	pick := func(a, b string) string {
		if a != "" {
			return a
		}
		return b
	}
	return palette{
		Bg: pick(p.Bg, base.Bg), Fg: pick(p.Fg, base.Fg), Muted: pick(p.Muted, base.Muted),
		Project: pick(p.Project, base.Project), SelBg: pick(p.SelBg, base.SelBg), SelFg: pick(p.SelFg, base.SelFg),
		Key: pick(p.Key, base.Key), Running: pick(p.Running, base.Running), Thumb: pick(p.Thumb, base.Thumb),
		Error: pick(p.Error, base.Error), TitleBg: pick(p.TitleBg, base.TitleBg), TitleFg: pick(p.TitleFg, base.TitleFg),
	}
}

// theme is a palette resolved into ready-to-use styles.
type theme struct {
	base, muted, project, key, running, thumb, err vaxis.Style
	sel, selBlur, title, titleBlur                 vaxis.Style
}

func mustTheme(p palette) theme {
	t, err := p.build()
	if err != nil {
		panic(err)
	}
	return t
}

func (p palette) build() (theme, error) {
	var bad []string
	c := func(field, hex string) vaxis.Color {
		if hex == "" {
			return 0
		}
		col, ok := parseHex(hex)
		if !ok {
			bad = append(bad, field+"="+hex)
		}
		return col
	}
	bg := c("bg", p.Bg)
	fg := func(field, hex string) vaxis.Style { return vaxis.Style{Foreground: c(field, hex), Background: bg} }
	t := theme{
		base: fg("fg", p.Fg), muted: fg("muted", p.Muted), project: fg("project", p.Project),
		key: fg("key", p.Key), running: fg("running", p.Running), thumb: fg("thumb", p.Thumb), err: fg("error", p.Error),
		sel:       vaxis.Style{Foreground: c("selFg", p.SelFg), Background: c("selBg", p.SelBg)},
		title:     vaxis.Style{Foreground: c("titleFg", p.TitleFg), Background: c("titleBg", p.TitleBg), Attribute: vaxis.AttrBold},
		titleBlur: vaxis.Style{Foreground: c("muted", p.Muted), Background: c("selBg", p.SelBg), Attribute: vaxis.AttrBold},
	}
	if len(bad) > 0 {
		return theme{}, fmt.Errorf("bad colors: %s", strings.Join(bad, ", "))
	}
	if p.SelBg == "" {
		t.sel.Attribute = vaxis.AttrReverse
		t.titleBlur.Attribute |= vaxis.AttrDim
	}
	if p.TitleBg == "" {
		t.title.Attribute |= vaxis.AttrReverse
	}
	if p.Muted == "" {
		t.muted.Attribute = vaxis.AttrDim
	}
	t.selBlur = t.sel
	t.selBlur.Attribute |= vaxis.AttrDim
	return t, nil
}

func parseHex(s string) (vaxis.Color, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return vaxis.RGBColor(uint8(v>>16), uint8(v>>8), uint8(v)), true
}
