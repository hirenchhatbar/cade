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
	a.vx.Render()
}

// sidebarHints returns the lines under the tree for the current mode.
func (a *app) sidebarHints() []string {
	switch {
	case a.confirm != nil:
		return append(slices.Clone(a.confirm.lines), confirmHint)
	case a.input != nil:
		return a.promptLines()
	case a.inTerm:
		return grid(a.hintPairs(paneKeys, false), sideW-1)
	default:
		return grid(a.hintPairs(sidebarKeys, a.st.HideHints), sideW-1)
	}
}

func (a *app) drawSidebar(side vaxis.Window) {
	th := a.th
	side.Fill(cell(" ", th.base))
	a.titleBar(side, " SESSIONS", appLabel+" ", !a.inTerm)
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
	for i := a.top; i < min(a.top+a.visible, len(rows)); i++ {
		a.drawRow(tree, i-a.top, rows[i], i == a.cursor, cw)
	}
	if overflow {
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
	label, id, mark, labelStyle := newRowLbl, "", "  ", th.key
	if r.s >= 0 {
		s := p.sessions[r.s]
		label, id, labelStyle = s.Title, " ("+s.short()+")", th.base
		if a.terms[s.ID] != nil {
			mark = "● "
		}
	}
	room := cw - runeLen(branch+mark) - runeLen(id)
	text := truncate(label, room)
	win.PrintTruncate(y,
		vaxis.Segment{Text: branch, Style: hl(th.muted)},
		vaxis.Segment{Text: mark, Style: hl(th.running)},
		vaxis.Segment{Text: text, Style: hl(labelStyle)},
		vaxis.Segment{Text: pad(id, room-runeLen(text)+runeLen(id)), Style: hl(th.muted)})
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

// drawIdle centers the bindings that have an empty-pane prompt.
func (a *app) drawIdle(pane vaxis.Window) {
	var lines []string
	for _, b := range sidebarKeys {
		if b.idle != "" {
			lines = append(lines, "["+b.label+"] "+b.idle)
		}
	}
	bw := 0
	for _, l := range lines {
		bw = max(bw, runeLen(l))
	}
	pw, ph := pane.Size()
	block := pane.New(max((pw-bw)/2, 0), max((ph-len(lines))/2, 0), min(bw+1, pw), len(lines))
	for i, l := range lines {
		block.PrintTruncate(i, a.hintSegs(l)...)
	}
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
	w, h := win.Size()
	bw, bh := min(3+keyW+1+textW+2, w), min(len(lines)+2*titleH+2, h)
	box := win.New((w-bw)/2, (h-bh)/2, bw, bh)
	panel := th.base
	if panel.Background == 0 {
		panel = th.sel // terminal theme: reverse video so the dialog stands out
	}
	on := func(s vaxis.Style) vaxis.Style { s.Background = panel.Background; return s }
	box.Fill(cell(" ", panel))
	a.titleBar(box, " SHORTCUTS", appLabel+" ", true)
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
	box.PrintTruncate(bh-1, vaxis.Segment{Text: pad(" any key to close", bw), Style: th.muted})
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
