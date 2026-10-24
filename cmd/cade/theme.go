package main

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"go.rockorager.dev/vaxis"
)

// palette is a theme as hex colors ("#rrggbb"); "" means the terminal default.
type palette struct {
	Bg      string `yaml:"bg"`      // sidebar and title bar background
	Fg      string `yaml:"fg"`      // session titles
	Muted   string `yaml:"muted"`   // dirs, ids, hints, separator, scrollbar track
	Project string `yaml:"project"` // project names
	SelBg   string `yaml:"selBg"`   // cursor row; "" falls back to reverse video
	SelFg   string `yaml:"selFg"`
	Key     string `yaml:"key"`     // [shortcut] labels
	Running string `yaml:"running"` // ● running session mark
	Thumb   string `yaml:"thumb"`   // scrollbar thumb
	Error   string `yaml:"error"`
	TitleBg string `yaml:"titleBg"` // focused panel's title bar
	TitleFg string `yaml:"titleFg"`
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

// loadTheme resolves the theme selected in cfg. On any problem it returns
// dracula plus an error to show, so a bad theme never blocks startup.
func loadTheme(cfg config) (theme, error) {
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

// inherit fills p's empty fields from base. Reflection keeps palette's field
// list the only place colors are enumerated.
func inherit(p, base palette) palette {
	pv, bv := reflect.ValueOf(&p).Elem(), reflect.ValueOf(base)
	for i := range pv.NumField() {
		if pv.Field(i).String() == "" {
			pv.Field(i).SetString(bv.Field(i).String())
		}
	}
	return p
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
