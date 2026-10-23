package main

import "slices"

// row is one line of the sidebar tree: a project header, its new-session row, or a session.
type row struct {
	p int // index into tree.projects
	s int // rowHeader, rowNew, or index into the project's sessions
}

const (
	rowHeader = -2
	rowNew    = -1
)

// rowKey identifies a row across reloads and reorders.
type rowKey struct {
	dir, id string
	s       int
}

// tree is the sidebar model: projects, cursor and scroll position. It does no
// UI or IO; callers persist changes when a method reports one.
type tree struct {
	projects []*project
	cursor   int // index into rows()
	top      int // first visible row
	visible  int // rows on screen, set by the renderer
}

// rows flattens projects and their sessions into the sidebar tree.
func (t *tree) rows() []row {
	var rows []row
	for pi, p := range t.projects {
		rows = append(rows, row{pi, rowHeader})
		if p.Collapsed {
			continue
		}
		rows = append(rows, row{pi, rowNew})
		for si := range p.sessions {
			rows = append(rows, row{pi, si})
		}
	}
	return rows
}

func (t *tree) current() row { return t.rows()[t.cursor] }

func (t *tree) keyOf(r row) rowKey {
	p := t.projects[r.p]
	k := rowKey{dir: p.Dir, s: r.s}
	if r.s >= 0 {
		k.id, k.s = p.sessions[r.s].ID, 0
	}
	return k
}

// restore puts the cursor back on the row identified by k, if it still exists.
func (t *tree) restore(k rowKey) {
	rows := t.rows()
	if i := slices.IndexFunc(rows, func(r row) bool { return t.keyOf(r) == k }); i >= 0 {
		t.cursor = i
	}
	t.cursor = max(min(t.cursor, len(rows)-1), 0)
}

// header returns the row of the previous (dir<0) or next (dir>0) project header.
func (t *tree) header(dir int) int {
	rows := t.rows()
	for i := t.cursor + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].s == rowHeader {
			return i
		}
	}
	return t.cursor
}

// fold collapses or expands the cursor's project; collapsing from a session row
// first jumps to its project header. It reports whether the project changed.
func (t *tree) fold(collapse bool) bool {
	cur := t.current()
	p := t.projects[cur.p]
	defer t.restore(rowKey{dir: p.Dir, s: rowHeader})
	if (collapse && cur.s != rowHeader) || p.Collapsed == collapse {
		return false
	}
	p.Collapsed = collapse
	return true
}

// move swaps the cursor's project with its neighbour; it reports whether it moved.
func (t *tree) move(dir int) bool {
	cur := t.current()
	j := cur.p + dir
	if j < 0 || j >= len(t.projects) {
		return false
	}
	k := t.keyOf(cur)
	t.projects[cur.p], t.projects[j] = t.projects[j], t.projects[cur.p]
	t.restore(k)
	return true
}

// scroll sets the first visible row and pulls the cursor into view, so
// keepCursorVisible doesn't snap the scroll back.
func (t *tree) scroll(top int) {
	t.top = max(min(top, max(len(t.rows())-t.visible, 0)), 0)
	t.cursor = max(min(t.cursor, t.top+t.visible-1), t.top)
}

// keepCursorVisible scrolls just enough to show the cursor, without leaving
// empty space below the last row.
func (t *tree) keepCursorVisible() {
	t.top = min(t.top, t.cursor)
	t.top = max(t.top, t.cursor-t.visible+1)
	t.top = min(t.top, max(len(t.rows())-t.visible, 0))
}
