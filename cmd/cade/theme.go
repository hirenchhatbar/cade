package main

import (
	"fmt"
	"hash/fnv"
	"math"
	"reflect"
	"strconv"
	"strings"

	"go.rockorager.dev/vaxis"
)

// palette is a theme as hex colors ("#rrggbb"); "" means the terminal default.
type palette struct {
	Bg        string `yaml:"bg"`      // sidebar and title bar background
	Fg        string `yaml:"fg"`      // session titles
	Muted     string `yaml:"muted"`   // dirs, ids, hints, separator, scrollbar track
	Project   string `yaml:"project"` // project names
	SelBg     string `yaml:"selBg"`   // cursor row; "" falls back to reverse video
	SelFg     string `yaml:"selFg"`
	Key       string `yaml:"key"`       // [shortcut] labels
	Running   string `yaml:"running"`   // ● running session mark
	Attention string `yaml:"attention"` // ◆ session waiting for you
	Thumb     string `yaml:"thumb"`     // scrollbar thumb
	Error     string `yaml:"error"`
	TitleBg   string `yaml:"titleBg"` // focused panel's title bar
	TitleFg   string `yaml:"titleFg"`
}

var themes = map[string]palette{
	"dracula": {
		Bg: "#282a36", Fg: "#f8f8f2", Muted: "#6272a4", Project: "#bd93f9",
		SelBg: "#44475a", SelFg: "#f8f8f2", Key: "#ff79c6", Running: "#50fa7b", Attention: "#ffb86c",
		Thumb: "#bd93f9", Error: "#ff5555", TitleBg: "#bd93f9", TitleFg: "#282a36",
	},
	"terminal": {}, // terminal's own colors, attributes only
	"tokyonight-night": {
		Bg: "#1a1b26", Fg: "#c0caf5", Muted: "#565f89", Project: "#7aa2f7",
		SelBg: "#283457", SelFg: "#c0caf5", Key: "#bb9af7", Running: "#9ece6a", Attention: "#e0af68",
		Thumb: "#7aa2f7", Error: "#f7768e", TitleBg: "#7aa2f7", TitleFg: "#1a1b26",
	},
	"tokyonight-day": {
		Bg: "#e1e2e7", Fg: "#3760bf", Muted: "#848cb5", Project: "#2e7de9",
		SelBg: "#b7c1e3", SelFg: "#3760bf", Key: "#9854f1", Running: "#587539", Attention: "#8c6c3e",
		Thumb: "#2e7de9", Error: "#f52a65", TitleBg: "#2e7de9", TitleFg: "#e1e2e7",
	},
	"catppuccin-mocha": {
		Bg: "#1e1e2e", Fg: "#cdd6f4", Muted: "#6c7086", Project: "#cba6f7",
		SelBg: "#313244", SelFg: "#cdd6f4", Key: "#f5c2e7", Running: "#a6e3a1", Attention: "#fab387",
		Thumb: "#cba6f7", Error: "#f38ba8", TitleBg: "#cba6f7", TitleFg: "#1e1e2e",
	},
	"catppuccin-latte": {
		Bg: "#eff1f5", Fg: "#4c4f69", Muted: "#9ca0b0", Project: "#8839ef",
		SelBg: "#ccd0da", SelFg: "#4c4f69", Key: "#ea76cb", Running: "#40a02b", Attention: "#fe640b",
		Thumb: "#8839ef", Error: "#d20f39", TitleBg: "#8839ef", TitleFg: "#eff1f5",
	},
	"gruvbox-dark": {
		Bg: "#282828", Fg: "#ebdbb2", Muted: "#928374", Project: "#83a598",
		SelBg: "#3c3836", SelFg: "#ebdbb2", Key: "#d3869b", Running: "#b8bb26", Attention: "#fe8019",
		Thumb: "#83a598", Error: "#fb4934", TitleBg: "#83a598", TitleFg: "#282828",
	},
	"gruvbox-light": {
		Bg: "#fbf1c7", Fg: "#3c3836", Muted: "#928374", Project: "#076678",
		SelBg: "#ebdbb2", SelFg: "#3c3836", Key: "#8f3f71", Running: "#79740e", Attention: "#af3a03",
		Thumb: "#076678", Error: "#9d0006", TitleBg: "#076678", TitleFg: "#fbf1c7",
	},
	"nord": { // no official light variant
		Bg: "#2e3440", Fg: "#d8dee9", Muted: "#4c566a", Project: "#88c0d0",
		SelBg: "#3b4252", SelFg: "#d8dee9", Key: "#b48ead", Running: "#a3be8c", Attention: "#ebcb8b",
		Thumb: "#88c0d0", Error: "#bf616a", TitleBg: "#88c0d0", TitleFg: "#2e3440",
	},
	"solarized-dark": {
		Bg: "#002b36", Fg: "#839496", Muted: "#586e75", Project: "#268bd2",
		SelBg: "#073642", SelFg: "#93a1a1", Key: "#d33682", Running: "#859900", Attention: "#cb4b16",
		Thumb: "#268bd2", Error: "#dc322f", TitleBg: "#268bd2", TitleFg: "#002b36",
	},
	"solarized-light": {
		Bg: "#fdf6e3", Fg: "#657b83", Muted: "#93a1a1", Project: "#268bd2",
		SelBg: "#eee8d5", SelFg: "#586e75", Key: "#d33682", Running: "#859900", Attention: "#cb4b16",
		Thumb: "#268bd2", Error: "#dc322f", TitleBg: "#268bd2", TitleFg: "#fdf6e3",
	},
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
	base, muted, project, key, running, attention, thumb, err vaxis.Style
	sel, selBlur, title, titleBlur                            vaxis.Style
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
		key: fg("key", p.Key), running: fg("running", p.Running), attention: fg("attention", p.Attention), thumb: fg("thumb", p.Thumb), err: fg("error", p.Error),
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

// projectStyle is th.project in p's color; an empty or bad color keeps th.project.
func (th theme) projectStyle(p *project) vaxis.Style {
	s := th.project
	if c, ok := parseHex(p.Color); ok {
		s.Foreground = c
	}
	return s
}

// Project colors are picked in Oklab, where equal distances look equally
// different. Candidates are 36 hues on two lightness rings, mid tones that
// read on dark and light themes: about 50 projects stay apart, not more.
var (
	projRings = []float64{0.62, 0.76} // Oklab lightness
	projHues  = 36                    // per ring, 10° apart
)

const projChroma, hueStep = 0.13, 30.0 // hueStep: degrees per c press

type oklab struct{ l, a, b float64 }

func (c oklab) dist(d oklab) float64 {
	return math.Sqrt((c.l-d.l)*(c.l-d.l) + (c.a-d.a)*(c.a-d.a) + (c.b-d.b)*(c.b-d.b))
}

// lchHex turns lightness, hue (degrees) and projChroma into "#rrggbb".
func lchHex(l, hue float64) string {
	h := hue * math.Pi / 180
	a, b := projChroma*math.Cos(h), projChroma*math.Sin(h)
	cube := func(x float64) float64 { return x * x * x }
	lm, mm, sm := cube(l+0.3963377774*a+0.2158037573*b), cube(l-0.1055613458*a-0.0638541728*b), cube(l-0.0894841775*a-1.2914855480*b)
	enc := func(x float64) uint8 { // linear to sRGB, clipped to the gamut
		x = max(0, min(1, x))
		if x > 0.0031308 {
			x = 1.055*math.Pow(x, 1/2.4) - 0.055
		} else {
			x *= 12.92
		}
		return uint8(math.Round(x * 255))
	}
	return fmt.Sprintf("#%02x%02x%02x",
		enc(4.0767416621*lm-3.3077115913*mm+0.2309699292*sm),
		enc(-1.2684380046*lm+2.6097574011*mm-0.3413193965*sm),
		enc(-0.0041960863*lm-0.7034186147*mm+1.7076147010*sm))
}

// hexLab turns "#rrggbb" into Oklab.
func hexLab(s string) (oklab, bool) {
	col, ok := parseHex(s)
	if !ok {
		return oklab{}, false
	}
	rgb := col.Params()
	dec := func(v uint8) float64 { // sRGB to linear
		x := float64(v) / 255
		if x > 0.04045 {
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return x / 12.92
	}
	r, g, b := dec(rgb[0]), dec(rgb[1]), dec(rgb[2])
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	n := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return oklab{0.2104542553*l + 0.7936177850*m - 0.0040720468*n,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*n,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*n}, true
}

// pickColor returns the candidate farthest from the other projects' colors.
// Candidates start at a point picked from p's directory, so ties differ per project.
func pickColor(p *project, ps []*project) string {
	var used []oklab
	for _, o := range ps {
		if c, ok := hexLab(o.Color); ok && o != p {
			used = append(used, c)
		}
	}
	h := fnv.New32a()
	h.Write([]byte(p.Dir))
	n := projHues * len(projRings)
	start := int(h.Sum32() % uint32(n))
	best, bestD := "", -1.0
	for i := range n {
		k := (start + i) % n
		hex := lchHex(projRings[k%len(projRings)], float64(k/len(projRings))*360/float64(projHues))
		c, _ := hexLab(hex)
		d := math.Inf(1)
		for _, u := range used {
			d = min(d, c.dist(u))
		}
		if d > bestD {
			best, bestD = hex, d
		}
	}
	return best
}

// stepHue turns p's color hueStep degrees round the wheel (dir 1 or -1), on
// the nearer lightness ring. A project without a color gets the one farthest
// from the colors in ps.
func stepHue(p *project, ps []*project, dir int) {
	c, ok := hexLab(p.Color)
	if !ok {
		p.Color = pickColor(p, ps)
		return
	}
	ring := projRings[0]
	if math.Abs(c.l-projRings[1]) < math.Abs(c.l-ring) {
		ring = projRings[1]
	}
	p.Color = lchHex(ring, math.Atan2(c.b, c.a)*180/math.Pi+float64(dir)*hueStep)
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
