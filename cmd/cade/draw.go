package main

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"go.rockorager.dev/vaxis"
)

const (
	titleH = 1 // title bar rows above each panel
	barMax = 5 // most status bar rows statusBar.height may ask for
)

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

// flow packs items left to right into rows w wide, two spaces apart, the first
// row starting first columns in. An item wider than a row gets a row of its own.
func flow(items []string, w, first int) [][]string {
	rows, x := [][]string{nil}, first
	for _, it := range items {
		gap := 0
		if len(rows[len(rows)-1]) > 0 {
			gap = 2
		}
		if x > 0 && x+gap+runeLen(it) > w {
			rows, x, gap = append(rows, nil), 0, 0
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], it)
		x += gap + runeLen(it)
	}
	return rows
}

// hints are the [label] hint items of the bindings shown now, or of all of them,
// help first so a short status bar never cuts it.
func (a *app) hints(bs []binding, all bool) []string {
	var items []string
	for _, b := range bs {
		if b.label == "" || b.hint == "" || !all && b.show != nil && !b.show(a) {
			continue
		}
		items = append(items, "["+b.label+"] "+b.hint)
	}
	if i := slices.Index(items, "[?] help"); i > 0 {
		items = slices.Insert(slices.Delete(items, i, i+1), 0, "[?] help")
	}
	return items
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
	w, h := a.screen()
	a.drawBar(win.New(0, h, w, -1))
	win = win.New(0, 0, w, h)

	if a.sideShown() {
		a.drawSidebar(win.New(a.sideX(w), 0, a.sideW(), h))
	}
	if !a.sheet() {
		if a.sideShown() {
			for r := range h {
				win.SetCell(a.sepX(w), r, cell("│", a.th.muted))
			}
		}
		a.drawPane(win, w, h)
	}
	a.drawHelp(win)
	a.drawAdd(win)
	a.drawRename(win)
	a.drawSettings(win)
	a.drawConfirm(win)
	a.drawToasts(win)
	a.vx.Render()
}

// screen is the window's width and the height above the status bar.
func (a *app) screen() (w, h int) {
	w, h = a.vx.Window().Size()
	return w, max(h-a.barH(w), 1)
}

// barRows lays out items in a status bar w wide, after the app label on its first row.
func (a *app) barRows(items []string, w int) [][]string { return flow(items, w-1, a.barLead()) }

// barLead is the width the app label takes on the status bar's first row; --mobile
// leaves it out, so the tap targets fit a phone screen.
func (a *app) barLead() int {
	if a.mobile {
		return 0
	}
	return runeLen(appLabel) + 2
}

// barH is the status bar's rows on a screen w wide: statusBar.height, or with 0
// the rows the longest binding table needs, so the bar keeps its height as focus moves.
func (a *app) barH(w int) int {
	if a.st.HideBar {
		return 0
	}
	if n := a.cfg.StatusBar.Height; n > 0 {
		return min(n, barMax)
	}
	if a.mobile { // a phone screen is short; drawBar cuts the hints with …
		return 1
	}
	n := 1
	for _, bs := range barTables {
		n = max(n, len(a.barRows(a.hints(bs, true), w)))
	}
	return n
}

// barTables are the tables barH fits; set in init, as the tables' run funcs reach barH.
var barTables [][]binding

func init() { barTables = [][]binding{sidebarKeys, paneKeys} }

// barHints are the status bar's hints for whatever takes the keys, checked in key()'s order.
func (a *app) barHints() []string {
	if a.mobile {
		var labels []string
		for _, t := range a.taps() {
			labels = append(labels, t.label)
		}
		return labels
	}
	switch {
	case a.confirm != nil:
		return nil // dialogs show their own keys
	case a.diffFocused():
		mode := "unified"
		if a.st.UnifiedDiff {
			mode = "side by side"
		}
		return []string{"[s] " + mode, "[Esc] close"}
	case a.inTerm:
		return a.hints(paneKeys, false)
	case a.help, a.input != nil, a.rename != nil, a.set != nil:
		return nil
	case a.search != nil:
		return []string{"[↑/↓] pick", "[⏎] open", "[Esc] cancel"}
	}
	return a.hints(sidebarKeys, false)
}

// drawBar draws the status bar: the app name and version, then the hints right-aligned, cut
// with … when they need more rows than the bar has.
func (a *app) drawBar(win vaxis.Window) {
	w, h := win.Size()
	if h <= 0 {
		return
	}
	th := a.th
	win.Fill(cell(" ", th.base))
	rows := a.barRows(a.barHints(), w)
	cut := len(rows) > h
	rows = rows[:min(len(rows), h)]
	for y, r := range rows {
		segs := []vaxis.Segment{{Text: " ", Style: th.muted}}
		room := w - 1
		if y == 0 && a.barLead() > 0 {
			segs = []vaxis.Segment{{Text: " " + appLabel + " ", Style: th.title}, {Text: " ", Style: th.muted}}
			room -= a.barLead()
		}
		line := strings.Join(r, "  ")
		if cut && y == len(rows)-1 {
			line = truncate(line+"  …", room)
		}
		line = strings.Repeat(" ", max(room-1-runeLen(line), 0)) + line // right-aligned, one space margin
		win.PrintTruncate(y, append(segs, a.hintSegs(line)...)...)
	}
}

func (a *app) drawSidebar(side vaxis.Window) {
	th := a.th
	side.Fill(cell(" ", th.base))
	back := ""
	if a.mobile && hasPane(a) {
		back = "✕ " // a tap on it goes back to the pane
	}
	a.titleBar(side, " SESSIONS", back, !a.inTerm)
	_, sh := side.Size()
	tree := side.New(0, titleH, a.sideW(), sh-titleH)
	_, treeH := tree.Size()

	a.visible = treeH
	cw := a.sideW() // row content width
	a.rowLines = func(r row) int { return len(a.labelLines(r, cw)) - 1 + a.rowH() }
	overflow := a.maxTop() > 0
	if overflow {
		cw = a.sideW() - 1 // last column is the scrollbar; narrower rows only wrap more, so it still overflows
	}
	a.keepCursorVisible()
	rows := a.rows()
	if a.search != nil {
		a.drawSearch(tree, cw)
	} else {
		a.drawRows(tree, rows, nil, a.top, a.cursor, cw)
	}
	if overflow && a.search == nil {
		m := a.maxTop()
		a.scrollbar(tree, a.sideW()-1, 1, 0, treeH, m-a.top, m, !a.inTerm || a.hover == sideBar)
	}
}

// densities maps a sidebar.density (or --density) to the blank lines under each sidebar row.
var densities = map[string]int{"compact": 0, "comfortable": 1}

func densityNames() string { return strings.Join(slices.Sorted(maps.Keys(densities)), ", ") }

// rowH is the screen lines one sidebar row takes: the row and its density's blank lines.
func (a *app) rowH() int { return 1 + densities[a.cfg.Sidebar.Density] }

// starX is the column of a session row's star: after "   ├─ " and the running mark.
const starX = 8

// titleLines is the most lines a wrapped session title (sidebar.wrap) takes.
const titleLines = 3

// labelLines is the label of row r cw columns wide: a session title wraps when
// sidebar.wrap is on, anything else is cut to one line.
func (a *app) labelLines(r row, cw int) []string {
	p := a.projects[r.p]
	if r.s < 0 {
		return []string{""} // drawRow draws headers and the new-session row itself
	}
	room := max(cw-starX-2, 1) // after the star
	if !a.cfg.Sidebar.Wrap {
		return []string{truncate(p.sessions[r.s].Title, room)}
	}
	return wrap(p.sessions[r.s].Title, room, titleLines)
}

// drawRows draws rows from index top down win, cw columns wide, the row at cursor
// selected. pos, when set, gives row i's matched rune positions to colour.
func (a *app) drawRows(win vaxis.Window, rows []row, pos func(i int) []int, top, cursor, cw int) {
	_, h := win.Size()
	for i, y := top, 0; i < len(rows) && y < h; i++ {
		last := i+1 == len(rows) || rows[i+1].p != rows[i].p
		var hit []int
		if pos != nil {
			hit = pos(i)
		}
		a.drawRow(win, y, rows[i], last, i == cursor, cw, hit)
		if !last { // the tree line runs on through the density's blank lines
			for g := len(a.labelLines(rows[i], cw)); g < a.rowLines(rows[i]); g++ {
				win.PrintTruncate(y+g, vaxis.Segment{Text: "   │", Style: a.th.projectStyle(a.projects[rows[i].p])})
			}
		}
		y += a.rowLines(rows[i])
	}
}

// drawRow draws one tree row at line y, cw columns wide; last ends the project's
// tree line. The runes at pos (in the project name or session title) are coloured.
func (a *app) drawRow(win vaxis.Window, y int, r row, last, selected bool, cw int, pos []int) {
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
		name := th.projectStyle(p)
		name.Attribute |= vaxis.AttrBold
		waiting := ""
		if n := a.waiting(p); p.Collapsed && n > 0 {
			waiting = fmt.Sprintf(" ◆%d", n)
		}
		segs := append([]vaxis.Segment{{Text: arrow, Style: hl(name)}}, th.hitSegs(p.Name, pos, hl(name))...)
		win.PrintTruncate(y, append(segs,
			vaxis.Segment{Text: waiting, Style: hl(th.attention)},
			vaxis.Segment{Text: pad("  "+a.short(p.Dir), cw-runeLen(arrow+p.Name+waiting)), Style: hl(th.muted)})...)
		return
	}
	branch := "   ├─ "
	if last {
		branch = "   └─ "
	}
	label, mark, star, labelStyle, markStyle := []string{newRowLbl}, "  ", "", th.key, th.running
	var title []rune // pos indexes it; each label line is a piece of it
	if r.s >= 0 {
		s := p.sessions[r.s]
		title = []rune(s.Title)
		label, star, labelStyle = a.labelLines(r, cw), "  ", th.base
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
	start := 0 // rune offset in title where the next line's search begins
	for i, l := range label {
		if i == 1 { // a wrapped title's next lines: the tree line runs on, no mark or star
			branch, mark, star = strings.Replace(strings.Replace(branch, "├─", "│ ", 1), "└─", "  ", 1), "  ", "  "
		}
		text := truncate(l, room)
		piece := []rune(strings.TrimSuffix(text, "…"))
		off := start + max(runeIndex(title[start:], piece), 0) // wrap drops spaces, so find the line in the title
		start = off + len(piece)
		var linePos []int
		for _, q := range pos {
			if q >= off && q < start {
				linePos = append(linePos, q-off)
			}
		}
		win.PrintTruncate(y+i, append([]vaxis.Segment{
			{Text: branch, Style: hl(th.projectStyle(p))},
			{Text: mark, Style: hl(markStyle)},
			{Text: star, Style: hl(th.key)}},
			th.hitSegs(pad(text, room), linePos, hl(labelStyle))...)...)
	}
}

func (a *app) drawPane(win vaxis.Window, w, h int) {
	x, end := a.paneSpan(w)
	waiting := ""
	if len(a.attn) > 0 {
		waiting = fmt.Sprintf("◆ %d waiting ["+waitLbl+"] ", len(a.attn))
	}
	if a.diff == nil && a.terms[a.active] == nil {
		a.titleBar(win.New(x, 0, end-x, titleH), " CLAUDE", waiting, false)
		a.drawIdle(win.New(x, titleH, end-x, h-titleH))
		return
	}
	ps := a.paneTerms(w, h)
	if len(ps) == 2 && ps[0].y == ps[1].y { // side by side: the line between them
		for r := range h {
			win.SetCell(ps[1].x-1, r, cell("│", a.th.muted))
		}
	}
	if ps[0].key == a.active { // each term's Draw shows its cursor, and the last one shown wins
		slices.Reverse(ps)
	}
	lead := " "
	if a.mobile {
		lead = " ▾ " // a tap on the title bar shows the sidebar
	}
	for _, p := range ps {
		right := ""
		if p.y == titleH && p.x+p.w == end {
			right = waiting
		}
		title := win.New(p.x, p.y-titleH, p.w, titleH)
		if p.key == diffID {
			a.titleBar(title, lead+"DIFF › "+a.diff.name, right, a.diff.focus)
			a.drawDiff(win.New(p.x, p.y, p.w, p.h))
			continue
		}
		t := a.terms[p.key]
		on := a.inTerm && p.key == a.active
		a.titleBar(title, lead+a.titleOf(p.key), right, on)
		tw := max(p.w-barW, 1) // scrollbar
		t.Resize(tw, p.h)
		t.Draw(win.New(p.x, p.y, tw, p.h))
		if off, m := t.ScrollInfo(); m > 0 {
			a.scrollbar(win, p.x+p.w-barW, barW, p.y, p.h, off, m, on || a.hover == paneBar)
		}
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
	a.dialogKeys(box, "any key to close")
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

// dialogKeys draws a dialog's key hint on its last row.
func (a *app) dialogKeys(box vaxis.Window, hint string) {
	_, bh := box.Size()
	box.PrintTruncate(bh-1, append([]vaxis.Segment{{Text: " ", Style: a.th.muted}}, a.hintSegs(hint)...)...)
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
	if in.err != "" {
		box.PrintTruncate(bh-2, vaxis.Segment{Text: " ✗ " + in.err, Style: on(th.err)})
	}
	a.dialogKeys(box, a.addHint())
}

// addHint is the add-project dialog's key hint for its current step.
func (a *app) addHint() string {
	if a.input.step == 0 {
		return "[⏎/⇥/↓] directory  [Esc] cancel"
	}
	tick := "☐"
	if a.input.hidden {
		tick = "☑"
	}
	return "[⇥] open  [↑↓] pick  [⇧⇥] name  [⏎] add  [Alt+h] " + tick + " hidden  [Esc] cancel"
}

// drawRename overlays the rename dialog of a session or project.
func (a *app) drawRename(win vaxis.Window) {
	rn := a.rename
	if rn == nil {
		return
	}
	th := a.th
	head, label, hint := " RENAME SESSION", " Title: ", renameHint
	if rn.id == "" {
		head, label, hint = " RENAME PROJECT", " Name: ", renameProjectHint
	}
	box, on := a.dialog(win, head, 84, titleH+4)
	bw, bh := box.Size()
	box.PrintTruncate(titleH+1,
		vaxis.Segment{Text: label, Style: on(th.key)},
		vaxis.Segment{Text: truncateLeft(rn.title, bw-runeLen(label)-2), Style: on(th.base)},
		vaxis.Segment{Text: "▏", Style: on(th.key)})
	if rn.busy {
		box.PrintTruncate(bh-2, vaxis.Segment{Text: " generating title…", Style: on(th.muted)})
	}
	a.dialogKeys(box, hint)
}

const (
	renameHint        = "[⏎] save (empty = Claude's title)  [^T] generate  [Esc] cancel"
	renameProjectHint = "[⏎] save (empty = folder name)  [Esc] cancel"
)

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
	bw, _ = box.Size()
	for i, r := range c.rows {
		box.PrintTruncate(titleH+1+i,
			vaxis.Segment{Text: " " + pad(r[0]+":", labelW+1), Style: on(th.muted)},
			vaxis.Segment{Text: truncateLeft(r[1], bw-labelW-3), Style: on(th.base)})
	}
	note := on(th.muted)
	if c.danger {
		note = on(th.err)
	}
	box.PrintTruncate(titleH+len(c.rows)+2, vaxis.Segment{Text: " " + c.note, Style: note})
	a.dialogKeys(box, confirmHint)
}

// drawMatch draws one directory row, the fuzzy-matched letters bold and underlined.
func (a *app) drawMatch(box vaxis.Window, y, bw int, m fuzzyMatch[string], selected bool, on func(vaxis.Style) vaxis.Style) {
	base, mark := on(a.th.base), "   "
	if selected {
		base, mark = a.th.sel, " › "
	}
	segs := append([]vaxis.Segment{{Text: mark, Style: base}}, a.th.hitSegs(m.item, m.pos, base)...)
	segs = append(segs, vaxis.Segment{Text: pad("/", bw-runeLen(mark+m.item)), Style: base})
	box.PrintTruncate(y, segs...)
}

// hitSegs draws text in base, the runes at pos in the key colour; the terminal
// theme has no colours, so there they are bold and underlined.
func (th theme) hitSegs(text string, pos []int, base vaxis.Style) []vaxis.Segment {
	hit := base
	if th.key.Foreground != 0 {
		hit.Foreground = th.key.Foreground
	} else {
		hit.Attribute |= vaxis.AttrBold
		hit.UnderlineStyle = vaxis.UnderlineSingle
	}
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

// drawSearch draws the / query line and, below it, the hits as a tree like the
// sidebar's, cw columns wide, in place of the tree.
func (a *app) drawSearch(win vaxis.Window, cw int) {
	th := a.th
	sr := a.search
	win.PrintTruncate(0,
		vaxis.Segment{Text: " / ", Style: th.key},
		vaxis.Segment{Text: truncateLeft(sr.query, a.sideW()-5), Style: th.base},
		vaxis.Segment{Text: "▏", Style: th.key})
	hits := a.searchHits()
	if len(hits) == 0 {
		win.PrintTruncate(2, vaxis.Segment{Text: "   no matches", Style: th.muted})
		return
	}
	sel := max(min(sr.sel, len(hits)-1), 0)
	rows := make([]row, len(hits))
	for i, h := range hits {
		rows[i] = h.item
	}
	_, h := win.Size()
	listH := h - 2 // query, blank
	top, used := sel, a.rowLines(rows[sel])
	for top > 0 && used+a.rowLines(rows[top-1]) <= listH { // sel on screen, as many hits above it as fit
		top--
		used += a.rowLines(rows[top])
	}
	a.drawRows(win.New(0, 2, cw, listH), rows, func(i int) []int { return hits[i].pos }, top, sel, cw)
}

// titleOf names session id for the pane's title bar and notifications.
func (a *app) titleOf(id string) string {
	if id == shellID {
		return "SHELL › " + a.short(a.shellDir)
	}
	if id == editorID {
		return "EDITOR › " + a.short(a.editDir)
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
	bar := "☑"
	if a.st.HideBar {
		bar = "☐"
	}
	barH := "‹ auto ›"
	if n := a.cfg.StatusBar.Height; n > 0 {
		barH = fmt.Sprintf("‹ %d ›", n)
	}
	side := "‹ left ›"
	if a.st.SidebarRight {
		side = "‹ right ›"
	}
	quit := "☐"
	if a.confirmQuit() {
		quit = "☑"
	}
	wrapTitles := "☐"
	if a.cfg.Sidebar.Wrap {
		wrapTitles = "☑"
	}
	editor := a.editorCmd()
	if st.typing {
		editor = st.cmd + "▏"
	}
	rows := [settingRows][2]string{
		setTheme:   {"Theme", "‹ " + cmp.Or(a.cfg.Theme, defaultTheme) + " ›"},
		setBar:     {"Status bar (S)", bar},
		setBarH:    {"Status bar height", barH},
		setSide:    {"Sidebar", side},
		setSideW:   {"Sidebar width", fmt.Sprintf("‹ %d ›", a.sideW())},
		setDensity: {"Sidebar density", "‹ " + cmp.Or(a.cfg.Sidebar.Density, "compact") + " ›"},
		setWrap:    {"Wrap titles", wrapTitles},
		setShell:   {"Shell size", fmt.Sprintf("‹ %d%% ›", splitPct(a.cfg.Shell.Height))},
		setDiff:    {"Diff size", fmt.Sprintf("‹ %d%% ›", splitPct(a.cfg.Diff.Height))},
		setEditorH: {"Editor size", fmt.Sprintf("‹ %d%% ›", splitPct(a.cfg.Editor.Height))},
		setQuit:    {"Confirm quit", quit},
		setEditor:  {"Editor (" + editorKey + ")", editor},
		setEdit:    {"Edit config.yaml", "in " + envEditor() + ", checked on save"},
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
	if len(st.issues) > 0 {
		y := titleH + settingRows + 2
		box.PrintTruncate(y, vaxis.Segment{Text: fmt.Sprintf(" ✗ config.yaml: %d issue(s); the config before the edit is still in use", len(st.issues)), Style: on(th.err)})
		for i, is := range st.issues {
			if y+1+i >= bh-1 { // screen too short: keep the key hint
				break
			}
			box.PrintTruncate(y+1+i, vaxis.Segment{Text: "   " + truncate(is, bw-4), Style: on(th.base)})
		}
	}
	a.dialogKeys(box, a.settingsHint())
}

// settingsHint is the settings dialog's key hint.
func (a *app) settingsHint() string {
	switch {
	case a.set.typing:
		return "[⏎] save (empty = $VISUAL, else $EDITOR)  [Esc] cancel"
	case len(a.set.issues) > 0:
		return "[e] edit again  [Esc] keep old config"
	}
	return "[↑↓] pick  [←→/⏎] change  [e] edit config  [Esc] close"
}
