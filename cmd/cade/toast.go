package main

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"go.rockorager.dev/vaxis"
)

type toastKind int

const (
	toastOK toastKind = iota
	toastNotice
	toastWarn
	toastErr
)

// ttl is how long a toast stays; errors longest, so there is time to read them.
func (k toastKind) ttl() time.Duration {
	return [...]time.Duration{3 * time.Second, 4 * time.Second, 6 * time.Second, 8 * time.Second}[k]
}

// toast is one message in the stack; id matches its toastExpire.
type toast struct {
	kind toastKind
	text string
	id   int
}

// toastExpire fires when toast id has been shown for its ttl.
type toastExpire struct{ id int }

const (
	maxToasts     = 5  // older ones drop off when more arrive
	toastW        = 48 // box width, border included
	toastMaxLines = 4  // longer text ends in …
)

var toastPositions = []string{"top-right", "top-left", "bottom-right", "bottom-left"}

// notify stacks a toast and schedules its removal.
func (a *app) notify(kind toastKind, text string) {
	a.toastSeq++
	a.toasts = append(a.toasts, toast{kind, text, a.toastSeq})
	if len(a.toasts) > maxToasts {
		a.toasts = a.toasts[1:]
	}
	if a.vx != nil { // nil in tests
		id := a.toastSeq
		time.AfterFunc(kind.ttl(), func() { a.vx.PostEvent(toastExpire{id}) })
	}
}

func (a *app) expireToast(id int) {
	a.toasts = slices.DeleteFunc(a.toasts, func(t toast) bool { return t.id == id })
}

// drawToasts stacks the toasts in the configured corner, oldest farthest from it.
func (a *app) drawToasts(win vaxis.Window) {
	if len(a.toasts) == 0 {
		return
	}
	th := a.th
	w, h := win.Size()
	pos := cmp.Or(a.cfg.Toasts.Position, toastPositions[0])
	bw := min(toastW, w)
	x := 0
	if strings.HasSuffix(pos, "right") {
		x = w - bw
	}
	panel := th.base
	if panel.Background == 0 {
		panel = th.sel // terminal theme: reverse video so the toast stands out
	}
	on := func(s vaxis.Style) vaxis.Style { s.Background = panel.Background; return s }
	icons := [...]string{"✓", "i", "!", "✗"}
	colors := [...]vaxis.Style{th.running, th.project, th.attention, th.err}

	boxes := make([][]string, len(a.toasts))
	total := 0
	for i, t := range a.toasts {
		boxes[i] = wrap(t.text, max(bw-4, 1), toastMaxLines)
		total += len(boxes[i]) + 1 // + gap
	}
	y := titleH
	if strings.HasPrefix(pos, "bottom") {
		y = max(h-total, 0)
	}
	for i, t := range a.toasts {
		lines := boxes[i]
		if y+len(lines) > h {
			break
		}
		box := win.New(x, y, bw, len(lines))
		box.Fill(cell(" ", panel))
		accent := on(colors[t.kind])
		accent.Attribute |= vaxis.AttrBold
		for j, l := range lines {
			icon := " "
			if j == 0 {
				icon = icons[t.kind]
			}
			box.PrintTruncate(j,
				vaxis.Segment{Text: "▌" + icon + " ", Style: accent},
				vaxis.Segment{Text: l, Style: on(th.base)})
		}
		y += len(lines) + 1
	}
}

// wrap breaks s into lines of at most w runes at spaces, at most n lines.
func wrap(s string, w, n int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case runeLen(line)+1+runeLen(word) <= w:
			line += " " + word
		default:
			lines, line = append(lines, line), word
		}
		for runeLen(line) > w { // a word longer than the box, such as a path
			lines, line = append(lines, string([]rune(line)[:w])), string([]rune(line)[w:])
		}
	}
	lines = append(lines, line)
	if len(lines) > n {
		lines = append(lines[:n-1], truncate(strings.Join(lines[n-1:], " "), w))
	}
	return lines
}
