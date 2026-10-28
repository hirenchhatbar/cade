package main

import (
	"cmp"
	"fmt"
	"os"
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
// offset rows up from the bottom of m hidden rows. The thumb is greyed out
// unless lit (its panel focused or the bar hovered).
func (a *app) scrollbar(win vaxis.Window, x, cols, y, h, offset, m int, lit bool) {
	top, size := thumb(h, offset, m)
	thumbStyle := a.th.muted
	if lit {
		thumbStyle = a.th.thumb
	}
	for r := range h {
		c := cell("░", a.th.muted)
		if r >= top && r < top+size {
			c = cell("█", thumbStyle)
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

	if a.sideShown() {
		x := a.sideX(w)
		a.drawSidebar(win.New(x, 0, sideW, h))
		sep := sideW
		if a.st.SidebarRight {
			sep = x - 1
		}
		for r := range h {
			win.SetCell(sep, r, cell("│", a.th.muted))
		}
	}
	a.drawPane(win, w, h)
	a.drawHelp(win)
	a.drawAdd(win)
	a.drawRename(win)
	a.drawSettings(win)
	a.drawConfirm(win)
	a.drawToasts(win)
	a.vx.Render()
}

// sidebarHints returns the lines under the tree for the current mode.
func (a *app) sidebarHints() []string {
	switch {
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
		a.scrollbar(tree, sideW-1, 1, 0, a.visible, m-a.top, m, !a.inTerm || a.hover == sideBar)
	}

	y := treeH - reserved
	tree.PrintTruncate(y, vaxis.Segment{Text: " " + strings.Repeat("─", sideW-2), Style: th.muted})
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
		waiting := ""
		if n := a.waiting(p); p.Collapsed && n > 0 {
			waiting = fmt.Sprintf(" ◆%d", n)
		}
		win.PrintTruncate(y,
			vaxis.Segment{Text: arrow + p.Name, Style: hl(name)},
			vaxis.Segment{Text: waiting, Style: hl(th.attention)},
			vaxis.Segment{Text: pad("  "+a.short(p.Dir), cw-runeLen(arrow+p.Name+waiting)), Style: hl(th.muted)})
		return
	}
	branch := "   ├─ "
	if r.s == len(p.sessions)-1 || (r.s == rowNew && len(p.sessions) == 0) {
		branch = "   └─ "
	}
	label, mark, star, labelStyle, markStyle := newRowLbl, "  ", "", th.key, th.running
	if r.s >= 0 {
		s := p.sessions[r.s]
		label, star, labelStyle = s.Title, "  ", th.base
		if slices.Contains(a.attn, s.ID) {
			mark, markStyle = "◆ ", th.attention
			labelStyle.Attribute |= vaxis.AttrBold
		} else if a.terms[s.ID] != nil {
			mark = "● "
		}
		if slices.Contains(p.Starred, s.ID) {
			star = "★ "
		}
	}
	room := cw - runeLen(branch+mark+star)
	win.PrintTruncate(y,
		vaxis.Segment{Text: branch, Style: hl(th.muted)},
		vaxis.Segment{Text: mark, Style: hl(markStyle)},
		vaxis.Segment{Text: star, Style: hl(th.key)},
		vaxis.Segment{Text: pad(truncate(label, room), room), Style: hl(labelStyle)})
}

func (a *app) drawPane(win vaxis.Window, w, h int) {
	x, end := a.paneSpan(w)
	title := win.New(x, 0, end-x, titleH)
	t := a.terms[a.active]
	waiting := ""
	if len(a.attn) > 0 {
		waiting = fmt.Sprintf("◆ %d waiting ["+waitLbl+"] ", len(a.attn))
	}
	if a.diff != nil {
		mode := "unified"
		if a.st.UnifiedDiff {
			mode = "side by side"
		}
		a.titleBar(title, " DIFF › "+a.diff.name, waiting+"[s] "+mode+"  [Esc] close ", true)
		a.drawDiff(win.New(x, titleH, end-x, h-titleH))
		return
	}
	if t == nil {
		a.titleBar(title, " CLAUDE", waiting, false)
		a.drawIdle(win.New(x, titleH, end-x, h-titleH))
		return
	}
	a.titleBar(title, " "+a.titleOf(a.active), waiting, a.inTerm)
	pw, ph := a.paneSize()
	t.Resize(pw, ph)
	t.Draw(win.New(x, titleH, pw, ph))
	if off, m := t.ScrollInfo(); m > 0 {
		a.scrollbar(win, end-barW, barW, titleH, ph, off, m, a.inTerm || a.hover == paneBar)
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

// drawConfirm overlays the pending yes/no question with what it touches.
func (a *app) drawConfirm(win vaxis.Window) {
	c := a.confirm
	if c == nil {
		return
	}
	th := a.th
	labelW, bw := 0, runeLen(c.title)+2
	for _, r := range c.rows {
		labelW = max(labelW, runeLen(r[0])+1)
	}
	for _, r := range c.rows {
		bw = max(bw, 1+labelW+1+runeLen(r[1])+1)
	}
	bw = max(bw, runeLen(c.note)+2, 84) // as large as the add-project dialog, so the logo behind doesn't show around it
	box, on := a.dialog(win, " "+c.title, bw, max(titleH+len(c.rows)+4, addListH+8))
	bw, bh := box.Size()
	for i, r := range c.rows {
		box.PrintTruncate(titleH+1+i,
			vaxis.Segment{Text: " " + pad(r[0]+":", labelW+1), Style: on(th.muted)},
			vaxis.Segment{Text: truncateLeft(r[1], bw-labelW-3), Style: on(th.base)})
	}
	note := on(th.muted)
	if c.danger {
		note = on(th.err)
	}
	box.PrintTruncate(bh-2, vaxis.Segment{Text: " " + c.note, Style: note})
	box.PrintTruncate(bh-1, append([]vaxis.Segment{{Text: " ", Style: th.muted}}, a.hintSegs(confirmHint)...)...)
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

// titleOf names session id for the pane's title bar and notifications.
func (a *app) titleOf(id string) string {
	if id == shellID {
		return "SHELL › " + a.short(a.shellDir)
	}
	for _, p := range a.projects {
		for _, s := range p.sessions {
			if s.ID == id {
				return p.Name + " › " + s.Title + " (" + s.short() + ")"
			}
		}
	}
	return id
}

// waiting counts p's sessions that rang for attention.
func (a *app) waiting(p *project) int {
	n := 0
	for _, s := range p.sessions {
		if slices.Contains(a.attn, s.ID) {
			n++
		}
	}
	return n
}

// drawSettings overlays the settings dialog, with the issues of the last
// config.yaml edit below the rows.
func (a *app) drawSettings(win vaxis.Window) {
	st := a.set
	if st == nil {
		return
	}
	th := a.th
	extra := 0
	if len(st.issues) > 0 {
		extra = len(st.issues) + 2 // blank, heading
	}
	// as tall as the add-project dialog so it covers the idle logo, taller when issues need it
	box, on := a.dialog(win, " SETTINGS", 84, max(addListH+8, titleH+settingRows+3+extra))
	bw, bh := box.Size()
	hints := "☐"
	if a.st.HideHints {
		hints = "☑"
	}
	side := "‹ left ›"
	if a.st.SidebarRight {
		side = "‹ right ›"
	}
	quit := "☐"
	if a.confirmQuit() {
		quit = "☑"
	}
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	rows := [settingRows][2]string{
		setTheme: {"Theme", "‹ " + cmp.Or(a.cfg.Theme, defaultTheme) + " ›"},
		setHints: {"Hide hints", hints},
		setSide:  {"Sidebar", side},
		setQuit:  {"Confirm quit", quit},
		setEdit:  {"Edit config.yaml", "in " + editor + ", checked on save"},
	}
	for i, r := range rows {
		base, label, mark := on(th.base), on(th.key), "   "
		if i == st.sel {
			base, label, mark = th.sel, th.sel, " › "
		}
		box.PrintTruncate(titleH+1+i,
			vaxis.Segment{Text: mark, Style: base},
			vaxis.Segment{Text: pad(r[0], 18), Style: label},
			vaxis.Segment{Text: pad(r[1], bw-21), Style: base})
	}
	hint := "[↑↓] pick  [←→/⏎] change  [e] edit config  [Esc] close"
	if len(st.issues) > 0 {
		y := titleH + settingRows + 2
		box.PrintTruncate(y, vaxis.Segment{Text: fmt.Sprintf(" ✗ config.yaml: %d issue(s); the config before the edit is still in use", len(st.issues)), Style: on(th.err)})
		for i, is := range st.issues {
			if y+1+i >= bh-1 { // screen too short: keep the footer
				break
			}
			box.PrintTruncate(y+1+i, vaxis.Segment{Text: "   " + truncate(is, bw-4), Style: on(th.base)})
		}
		hint = "[e] edit again  [Esc] keep old config"
	}
	box.PrintTruncate(bh-1, append([]vaxis.Segment{{Text: " ", Style: th.muted}}, a.hintSegs(hint)...)...)
}
