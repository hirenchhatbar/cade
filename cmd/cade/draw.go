package main

import (
	"fmt"
	"slices"
	"strings"

	"go.rockorager.dev/vaxis"
)

const titleH = 1 // title bar rows above each panel

func cell(g string, s vaxis.Style) vaxis.Cell {
	return vaxis.Cell{Character: vaxis.Character{Grapheme: g, Width: 1}, Style: s}
}

// runeLen counts runes. shortcut: not display width; wide (CJK/emoji) text can misalign, use a width lib if it matters.
func runeLen(s string) int { return len([]rune(s)) }

// truncate cuts s to w runes, ending in … when it had to cut.
func truncate(s string, w int) string {
	if r := []rune(s); len(r) > w {
		return string(r[:max(w-1, 0)]) + "…"
	}
	return s
}

// truncateLeft keeps the last w runes, starting with … when it had to cut.
func truncateLeft(s string, w int) string {
	if r := []rune(s); len(r) > w {
		return "…" + string(r[len(r)-(w-1):])
	}
	return s
}

// pad fills s with spaces to width w so a row's background spans the panel.
func pad(s string, w int) string {
	if n := w - runeLen(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// short abbreviates the home directory to ~.
func (a *app) short(dir string) string {
	if rest, ok := strings.CutPrefix(dir, a.home); ok && a.home != "" {
		return "~" + rest
	}
	return dir
}

// thumb returns the scrollbar thumb's top row and size for a viewport of h rows
// scrolled offset rows up from the bottom of m scrollback rows.
func thumb(h, offset, m int) (top, size int) {
	if m == 0 {
		return 0, h
	}
	size = max(1, h*h/(h+m))
	return (m - offset) * (h - size) / m, size
}

// offsetAt maps a clicked scrollbar row back to a scroll offset.
func offsetAt(row, h, m int) int {
	if h <= 1 {
		return 0
	}
	return m - row*m/(h-1)
}

// scrollbar draws a cols-wide bar at column x, rows y..y+h, for a view scrolled
// offset rows up from the bottom of m hidden rows.
func (a *app) scrollbar(win vaxis.Window, x, cols, y, h, offset, m int) {
	top, size := thumb(h, offset, m)
	for r := range h {
		c := cell("░", a.th.muted)
		if r >= top && r < top+size {
			c = cell("█", a.th.thumb)
		}
		for i := range cols {
			win.SetCell(x+i, y+r, c)
		}
	}
}

// grid lays out key/description pairs in two aligned columns of [key] desc,
// each column's keys padded to that column's widest key.
func grid(pairs [][2]string, width int) []string {
	colW := (width - 1) / 2
	var keyW [2]int
	for i, p := range pairs {
		keyW[i%2] = max(keyW[i%2], runeLen(p[0])+2)
	}
	var lines []string
	for i := 0; i < len(pairs); i += 2 {
		line := ""
		for c := 0; c < 2 && i+c < len(pairs); c++ {
			p := pairs[i+c]
			text := truncate(pad("["+p[0]+"]", keyW[c])+" "+p[1], colW)
			if c == 0 {
				text = pad(text, colW) + " "
			}
			line += text
		}
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return lines
}

// hintPairs picks the bindings shown in the sidebar hint grid.
func (a *app) hintPairs(bs []binding, onlyAlways bool) [][2]string {
	var pairs [][2]string
	for _, b := range bs {
		if b.label == "" || b.hint == "" || (onlyAlways && !b.always) || (b.show != nil && !b.show(a)) {
			continue
		}
		pairs = append(pairs, [2]string{b.label, b.hint})
	}
	return pairs
}

// hintSegs colors the [key] parts of a hint line.
func (a *app) hintSegs(line string) []vaxis.Segment {
	var segs []vaxis.Segment
	for line != "" {
		i := strings.IndexByte(line, '[')
		j := strings.IndexByte(line, ']')
		if i < 0 || j < i {
			segs = append(segs, vaxis.Segment{Text: line, Style: a.th.muted})
			break
		}
		segs = append(segs,
			vaxis.Segment{Text: line[:i], Style: a.th.muted},
			vaxis.Segment{Text: line[i : j+1], Style: a.th.key})
		line = line[j+1:]
	}
	return segs
}

// titleBar draws a panel's title row: text on the left, right (if any) right-aligned and not bold.
func (a *app) titleBar(win vaxis.Window, text, right string, focused bool) {
	style := a.th.titleBlur
	if focused {
		style = a.th.title
	}
	plain := style
	plain.Attribute &^= vaxis.AttrBold
	w, _ := win.Size()
	win.PrintTruncate(0,
		vaxis.Segment{Text: pad(text, w-runeLen(right)), Style: style},
		vaxis.Segment{Text: right, Style: plain})
}

func (a *app) draw() {
	win := a.vx.Window()
	win.Clear()
	a.vx.HideCursor()
	w, h := win.Size()

	a.drawSidebar(win.New(0, 0, sideW, h))
	for r := range h {
		win.SetCell(sideW, r, cell("│", a.th.muted))
	}
	a.drawPane(win, w, h)
	a.drawHelp(win)
	a.drawAdd(win)
	a.drawRename(win)
	a.vx.Render()
}

// sidebarHints returns the lines under the tree for the current mode.
func (a *app) sidebarHints() []string {
	switch {
	case a.confirm != nil:
		return append(slices.Clone(a.confirm.lines), confirmHint)
	case a.search != nil:
		return grid([][2]string{{"↑/↓", "pick"}, {"⏎", "open"}, {"Esc", "cancel"}}, sideW-1)
	case a.inTerm:
		return grid(a.hintPairs(paneKeys, false), sideW-1)
	default:
		return grid(a.hintPairs(sidebarKeys, a.st.HideHints), sideW-1)
	}
}

func (a *app) drawSidebar(side vaxis.Window) {
	th := a.th
	side.Fill(cell(" ", th.base))
	a.titleBar(side, " SESSIONS", "Cade v"+version+" ", !a.inTerm)
	_, sh := side.Size()
	tree := side.New(0, titleH, sideW, sh-titleH)
	_, treeH := tree.Size()

	hints := a.sidebarHints()
	reserved := len(hints) + 1 // + divider
	if a.msg != "" {
		reserved++
	}
	a.visible = max(treeH-reserved, 1)
	a.keepCursorVisible()
	rows := a.rows()
	overflow := len(rows) > a.visible
	cw := sideW // row content width
	if overflow {
		cw = sideW - 1 // last column is the scrollbar
	}
	if a.search != nil {
		a.drawSearch(tree)
	} else {
		for i := a.top; i < min(a.top+a.visible, len(rows)); i++ {
			a.drawRow(tree, i-a.top, rows[i], i == a.cursor, cw)
		}
	}
	if overflow && a.search == nil {
		m := len(rows) - a.visible
		a.scrollbar(tree, sideW-1, 1, 0, a.visible, m-a.top, m)
	}

	y := treeH - reserved
	tree.PrintTruncate(y, vaxis.Segment{Text: " " + strings.Repeat("─", sideW-2), Style: th.muted})
	if a.msg != "" {
		y++
		tree.PrintTruncate(y, vaxis.Segment{Text: " " + a.msg, Style: th.err})
	}
	for i, hint := range hints {
		tree.PrintTruncate(y+1+i, append([]vaxis.Segment{{Text: " ", Style: th.muted}}, a.hintSegs(hint)...)...)
	}
}

// starX is the column of a session row's star: after "   ├─ " and the running mark.
const starX = 8

// drawRow draws one tree row at line y, cw columns wide.
func (a *app) drawRow(win vaxis.Window, y int, r row, selected bool, cw int) {
	th := a.th
	hl := func(s vaxis.Style) vaxis.Style {
		if !selected {
			return s
		}
		sel := th.sel
		if a.inTerm {
			sel = th.selBlur
		}
		if sel.Foreground == 0 { // terminal theme: keep the slot's color under reverse video
			sel.Foreground = s.Foreground
		}
		return sel
	}
	p := a.projects[r.p]
	if r.s == rowHeader {
		arrow := " ▾ "
		if p.Collapsed {
			arrow = " ▸ "
		}
		name := th.project
		name.Attribute |= vaxis.AttrBold
		win.PrintTruncate(y,
			vaxis.Segment{Text: arrow + p.Name, Style: hl(name)},
			vaxis.Segment{Text: pad("  "+a.short(p.Dir), cw-runeLen(arrow+p.Name)), Style: hl(th.muted)})
		return
	}
	branch := "   ├─ "
	if r.s == len(p.sessions)-1 || (r.s == rowNew && len(p.sessions) == 0) {
		branch = "   └─ "
	}
	label, mark, star, labelStyle := newRowLbl, "  ", "", th.key
	if r.s >= 0 {
		s := p.sessions[r.s]
		label, star, labelStyle = s.Title, "  ", th.base
		if a.terms[s.ID] != nil {
			mark = "● "
		}
		if slices.Contains(p.Starred, s.ID) {
			star = "★ "
		}
	}
	room := cw - runeLen(branch+mark+star)
	win.PrintTruncate(y,
		vaxis.Segment{Text: branch, Style: hl(th.muted)},
		vaxis.Segment{Text: mark, Style: hl(th.running)},
		vaxis.Segment{Text: star, Style: hl(th.key)},
		vaxis.Segment{Text: pad(truncate(label, room), room), Style: hl(labelStyle)})
}

func (a *app) drawPane(win vaxis.Window, w, h int) {
	title := win.New(sideW+1, 0, w-sideW-1, titleH)
	t := a.terms[a.active]
	if t == nil {
		a.titleBar(title, " CLAUDE", "", false)
		a.drawIdle(win.New(sideW+1, titleH, w-sideW-1, h-titleH))
		return
	}
	a.titleBar(title, " "+a.activeTitle(), "", a.inTerm)
	pw, ph := a.paneSize()
	t.Resize(pw, ph)
	t.Draw(win.New(sideW+1, titleH, pw, ph))
	if off, m := t.ScrollInfo(); m > 0 {
		a.scrollbar(win, w-barW, barW, titleH, ph, off, m)
	}
	if !a.inTerm {
		a.vx.HideCursor()
	}
}

// drawIdle centers the logo, version and the bindings that have an empty-pane prompt.
// The logo is left out when the pane is too small for it.
func (a *app) drawIdle(pane vaxis.Window) {
	var hints []string
	for _, b := range sidebarKeys {
		if b.idle != "" {
			hints = append(hints, "["+b.label+"] "+b.idle)
		}
	}
	logoLines := strings.Split(logo, "\n")
	pw, ph := pane.Size()
	total := len(hints)
	showLogo := pw >= blockW(logoLines) && ph >= total+len(logoLines)+2
	if showLogo {
		total += len(logoLines) + 2 // + version, blank
	}
	y := max((ph-total)/2, 0)
	// put prints lines as a left-aligned block centered in the pane, from row y down.
	put := func(lines []string, segs func(i int, l string) []vaxis.Segment) {
		bw := blockW(lines)
		block := pane.New(max((pw-bw)/2, 0), y, min(bw+1, pw), len(lines))
		for i, l := range lines {
			block.PrintTruncate(i, segs(i, l)...)
		}
		y += len(lines)
	}
	if showLogo {
		th := a.th
		colors := []vaxis.Style{th.key, th.project, th.running, th.err, th.base, th.muted}
		put(logoLines, func(i int, l string) []vaxis.Segment {
			return []vaxis.Segment{{Text: l, Style: colors[i%len(colors)]}}
		})
		put([]string{"v" + version}, func(_ int, l string) []vaxis.Segment {
			return []vaxis.Segment{{Text: l, Style: th.muted}}
		})
		y++
	}
	put(hints, func(_ int, l string) []vaxis.Segment { return a.hintSegs(l) })
}

// blockW is the width of the widest line.
func blockW(lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, runeLen(l))
	}
	return w
}

// helpSections builds the ? dialog from the binding tables.
func helpSections() [][2]string {
	var lines [][2]string
	for _, sec := range []struct {
		name string
		bs   []binding
	}{{"Sidebar", sidebarKeys}, {"Claude pane", paneKeys}} {
		lines = append(lines, [2]string{"", sec.name})
		for _, b := range sec.bs {
			if b.label != "" {
				lines = append(lines, [2]string{b.label, b.help})
			}
		}
	}
	return lines
}

// keysMarkdown renders helpSections as Markdown tables for cade --keys.
func keysMarkdown() string {
	var b strings.Builder
	for _, l := range helpSections() {
		if l[0] == "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "### %s\n\n| Key | Action |\n|---|---|\n", l[1])
			continue
		}
		esc := strings.NewReplacer("|", `\|`)
		fmt.Fprintf(&b, "| `%s` | %s |\n", esc.Replace(l[0]), esc.Replace(l[1]))
	}
	return b.String()
}

// drawHelp overlays the shortcut dialog in the middle of the screen.
func (a *app) drawHelp(win vaxis.Window) {
	if !a.help {
		return
	}
	a.vx.HideCursor()
	th := a.th
	lines := helpSections()
	keyW, textW := 0, 0
	for _, l := range lines {
		keyW, textW = max(keyW, runeLen(l[0])+2), max(textW, runeLen(l[1]))
	}
	box, on := a.dialog(win, " SHORTCUTS", 3+keyW+1+textW+2, len(lines)+2*titleH+2)
	_, bh := box.Size()
	for i, l := range lines {
		y := titleH + 1 + i
		if y >= bh-1 {
			break
		}
		if l[0] == "" {
			heading := on(th.project)
			heading.Attribute |= vaxis.AttrBold
			box.PrintTruncate(y, vaxis.Segment{Text: " " + l[1], Style: heading})
			continue
		}
		box.PrintTruncate(y,
			vaxis.Segment{Text: "   " + pad("["+l[0]+"]", keyW+1), Style: on(th.key)},
			vaxis.Segment{Text: l[1], Style: on(th.base)})
	}
	box.PrintTruncate(bh-1, vaxis.Segment{Text: " any key to close", Style: th.muted})
}

// dialog fills a centered bw×bh box (shrunk to fit win) with a title bar. on puts a
// style on the box's background.
func (a *app) dialog(win vaxis.Window, title string, bw, bh int) (box vaxis.Window, on func(vaxis.Style) vaxis.Style) {
	w, h := win.Size()
	bw, bh = min(bw, w), min(bh, h)
	box = win.New((w-bw)/2, (h-bh)/2, bw, bh)
	panel := a.th.base
	if panel.Background == 0 {
		panel = a.th.sel // terminal theme: reverse video so the dialog stands out
	}
	box.Fill(cell(" ", panel))
	a.titleBar(box, title, "", true)
	return box, func(s vaxis.Style) vaxis.Style { s.Background = panel.Background; return s }
}

const addListH = 14 // directory rows in the add-project dialog

// drawAdd overlays the add-project dialog: the name, then the directory with the
// typed parent's subdirectories fuzzy-matched below it.
func (a *app) drawAdd(win vaxis.Window) {
	in := a.input
	if in == nil {
		return
	}
	th := a.th
	box, on := a.dialog(win, " ADD PROJECT", 84, addListH+8) // wide enough to cover the idle logo, centered in the pane, not the screen
	bw, bh := box.Size()
	field := func(y int, label, val string, typing bool) {
		labelStyle := th.muted
		if typing {
			labelStyle = th.key
		}
		segs := []vaxis.Segment{{Text: " " + label, Style: on(labelStyle)}, {Text: truncateLeft(val, bw-9), Style: on(th.base)}}
		if typing {
			segs = append(segs, vaxis.Segment{Text: "▏", Style: on(th.key)}) // ▏ marks the field being typed
		}
		box.PrintTruncate(y, segs...)
	}
	field(titleH+1, "Name: ", in.name, in.step == 0)
	field(titleH+2, "Dir:  ", in.dir, in.step == 1)
	ms := a.matches()
	sel := max(min(in.sel, len(ms)-1), 0)
	listH := max(bh-titleH-6, 1) // name, dir, blank, blank, error, footer
	top := max(sel-listH+1, 0)
	for i := top; i < min(len(ms), top+listH); i++ {
		a.drawMatch(box, titleH+4+i-top, bw, ms[i], i == sel && in.step == 1, on)
	}
	if len(ms) == 0 {
		box.PrintTruncate(titleH+4, vaxis.Segment{Text: "   no matching directories", Style: on(th.muted)})
	}
	hint := "[⏎/⇥/↓] directory  [Esc] cancel"
	if in.step == 1 {
		tick := "☐"
		if in.hidden {
			tick = "☑"
		}
		hint = "[⇥] open  [↑↓] pick  [⇧⇥] name  [⏎] add  [Alt+h] " + tick + " hidden  [Esc] cancel"
	}
	if in.err != "" {
		box.PrintTruncate(bh-2, vaxis.Segment{Text: " ✗ " + in.err, Style: on(th.err)})
	}
	box.PrintTruncate(bh-1, append([]vaxis.Segment{{Text: " ", Style: th.muted}}, a.hintSegs(hint)...)...)
}

// drawRename overlays the rename-session dialog.
func (a *app) drawRename(win vaxis.Window) {
	rn := a.rename
	if rn == nil {
		return
	}
	th := a.th
	box, on := a.dialog(win, " RENAME SESSION", 84, titleH+4)
	bw, bh := box.Size()
	box.PrintTruncate(titleH+1,
		vaxis.Segment{Text: " Title: ", Style: on(th.key)},
		vaxis.Segment{Text: truncateLeft(rn.title, bw-10), Style: on(th.base)},
		vaxis.Segment{Text: "▏", Style: on(th.key)})
	box.PrintTruncate(bh-1, append([]vaxis.Segment{{Text: " ", Style: th.muted}},
		a.hintSegs("[⏎] save (empty = Claude's title)  [Esc] cancel")...)...)
}

// drawMatch draws one directory row, the fuzzy-matched letters bold and underlined.
func (a *app) drawMatch(box vaxis.Window, y, bw int, m fuzzyMatch[string], selected bool, on func(vaxis.Style) vaxis.Style) {
	base, mark := on(a.th.base), "   "
	if selected {
		base, mark = a.th.sel, " › "
	}
	segs := append([]vaxis.Segment{{Text: mark, Style: base}}, hitSegs(m.item, m.pos, base)...)
	segs = append(segs, vaxis.Segment{Text: pad("/", bw-runeLen(mark+m.item)), Style: base})
	box.PrintTruncate(y, segs...)
}

// hitSegs draws text in base, the runes at pos bold and underlined.
func hitSegs(text string, pos []int, base vaxis.Style) []vaxis.Segment {
	hit := base
	hit.Attribute |= vaxis.AttrBold
	hit.UnderlineStyle = vaxis.UnderlineSingle
	var segs []vaxis.Segment
	for i, r := range []rune(text) {
		s := base
		if slices.Contains(pos, i) {
			s = hit
		}
		segs = append(segs, vaxis.Segment{Text: string(r), Style: s})
	}
	return segs
}

// drawSearch draws the / query line and, below it, the matching sessions with
// their project names, in place of the tree.
func (a *app) drawSearch(win vaxis.Window) {
	th := a.th
	sr := a.search
	win.PrintTruncate(0,
		vaxis.Segment{Text: " / ", Style: th.key},
		vaxis.Segment{Text: truncateLeft(sr.query, sideW-5), Style: th.base},
		vaxis.Segment{Text: "▏", Style: th.key})
	hits := a.searchHits()
	if len(hits) == 0 {
		win.PrintTruncate(2, vaxis.Segment{Text: "   no matching sessions", Style: th.muted})
		return
	}
	sel := max(min(sr.sel, len(hits)-1), 0)
	listH := max(a.visible-2, 1) // query, blank
	top := max(sel-listH+1, 0)
	for i := top; i < min(len(hits), top+listH); i++ {
		h := hits[i]
		base, mark := th.base, "   "
		if i == sel {
			base, mark = th.sel, " › "
		}
		proj := truncate(a.projects[h.item.p].Name, sideW/3) + " "
		title := truncate(a.projects[h.item.p].sessions[h.item.s].Title, sideW-runeLen(mark+proj)-2)
		muted := th.muted
		if i == sel {
			muted = th.sel
		}
		segs := append([]vaxis.Segment{{Text: mark, Style: base}}, hitSegs(title, h.pos, base)...)
		segs = append(segs,
			vaxis.Segment{Text: pad("", sideW-runeLen(mark+title+proj)), Style: base},
			vaxis.Segment{Text: proj, Style: muted})
		win.PrintTruncate(2+i-top, segs...)
	}
}

// activeTitle names the open session for the pane's title bar.
func (a *app) activeTitle() string {
	for _, p := range a.projects {
		for _, s := range p.sessions {
			if s.ID == a.active {
				return p.Name + " › " + s.Title + " (" + s.short() + ")"
			}
		}
	}
	return a.active
}
