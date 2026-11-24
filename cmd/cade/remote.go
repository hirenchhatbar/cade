package main

import (
	"cmp"
	"encoding/gob"
	"errors"
	"net"
	"os/exec"
	"sync"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/term"
)

// pane is a terminal in the pane: a local *term.Model (shell, editor) or a
// *remoteTerm (Claude session run by cade --serve).
type pane interface {
	Update(vaxis.Event)
	Draw(vaxis.Window)
	Resize(w, h int)
	ScrollInfo() (int, int)
	ScrollTo(int)
	HasSelection() bool
	Selection() string
	ClearSelection()
	Focus()
	Blur()
	Close()
}

// copied is a remote session's mouse selection, for the client to copy; a
// remoteTerm reports it as an event, not through HasSelection.
type copied string

// remoteTerm shows a session the server runs: it sends input and draws the
// last frame the server sent. Its methods run on the event loop; read updates
// fr in the background.
type remoteTerm struct {
	conn net.Conn
	enc  *gob.Encoder
	post func(vaxis.Event)
	w, h int // pane size, see Resize

	mu    sync.Mutex
	fr    frame
	asked bool // an opReady is out: the server sends a frame when the screen changes
}

// openRemote attaches to session id on the server, which starts it with cmd
// (nil: only attach) at w × h; post gets its events, never on the event loop.
func openRemote(id string, cmd *exec.Cmd, w, h int, post func(pane, vaxis.Event)) (*remoteTerm, error) {
	c, dec, _, err := dial(cmd != nil)
	if err != nil {
		return nil, err
	}
	o := &openReq{ID: id, W: w, H: h}
	if cmd != nil {
		o.Path, o.Args, o.Dir, o.Env = cmd.Path, cmd.Args, cmd.Dir, cmd.Env
	}
	r := &remoteTerm{conn: c, enc: gob.NewEncoder(c), w: w, h: h}
	var ok reply
	if err := r.enc.Encode(request{Op: opOpen, Open: o}); err != nil || dec.Decode(&ok) != nil {
		c.Close()
		return nil, cmp.Or(err, errors.New("server closed the connection"))
	}
	if ok.Op != opOK {
		c.Close()
		return nil, errors.New(ok.Text)
	}
	r.post = func(ev vaxis.Event) { post(r, ev) }
	go r.read(dec)
	return r, nil
}

// read takes the server's replies until the connection ends, which closes the pane.
func (r *remoteTerm) read(dec *gob.Decoder) {
	for {
		var m reply
		if dec.Decode(&m) != nil {
			r.post(term.EventClosed{})
			return
		}
		var ev vaxis.Event
		switch m.Op {
		case opFrame:
			r.mu.Lock()
			r.fr, r.asked = *m.Frame, false
			r.mu.Unlock()
			ev = vaxis.Redraw{}
		case opBell:
			ev = term.EventBell{}
		case opNotify:
			ev = term.EventNotify{Title: m.Text, Body: m.Body}
		case opTitle:
			ev = term.EventTitle(m.Text)
		case opShape:
			ev = term.EventMouseShape{Shape: vaxis.MouseShape(m.Text)}
		case opClip:
			ev = term.EventClipboard(m.Text)
		case opCopy:
			ev = copied(m.Text)
		case opClosed:
			r.post(term.EventClosed{})
			return
		default:
			continue
		}
		r.post(ev)
	}
}

// send writes one request; a failed write ends the connection, and read then closes the pane.
func (r *remoteTerm) send(q request) {
	if r.enc.Encode(q) != nil {
		r.conn.Close()
	}
}

// claim takes the session's size for this client before input: the last
// client to type, click or focus sets it.
func (r *remoteTerm) claim() {
	r.mu.Lock()
	same := r.fr.W == r.w && r.fr.H == r.h
	r.mu.Unlock()
	if !same {
		r.send(request{Op: opSize, N: r.w, M: r.h})
	}
}

func (r *remoteTerm) Update(ev vaxis.Event) {
	switch ev := ev.(type) {
	case vaxis.Key:
		r.claim()
		r.send(request{Op: opKey, Key: ev})
	case vaxis.Mouse:
		r.claim()
		r.send(request{Op: opMouse, Mouse: ev})
	case vaxis.PasteStartEvent:
		r.claim()
		r.send(request{Op: opPasteStart})
	case vaxis.PasteEndEvent:
		r.send(request{Op: opPasteEnd})
	case vaxis.FocusIn:
		r.Focus()
	case vaxis.FocusOut:
		r.Blur()
	case vaxis.ColorThemeUpdate:
		r.send(request{Op: opTheme, N: int(ev.Mode)})
	}
}

// Draw paints the last frame and asks for the next.
func (r *remoteTerm) Draw(win vaxis.Window) {
	r.mu.Lock()
	s, ask := r.fr.Snap, !r.asked
	r.asked = true
	r.mu.Unlock()
	for _, c := range s.Cells {
		win.SetCell(c.Col, c.Row, c.Cell)
	}
	if s.CursorVisible {
		win.ShowCursor(s.CursorCol, s.CursorRow, s.CursorStyle)
	}
	if ask {
		r.send(request{Op: opReady})
	}
}

// Resize sends a new pane size; another client's size stays until claim.
func (r *remoteTerm) Resize(w, h int) {
	if w == r.w && h == r.h {
		return
	}
	r.w, r.h = w, h
	r.send(request{Op: opSize, N: w, M: h})
}

func (r *remoteTerm) ScrollInfo() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fr.Off, r.fr.Max
}

func (r *remoteTerm) ScrollTo(n int) { r.send(request{Op: opScroll, N: n}) }

// HasSelection is always false: the server sends a selection as copied.
func (r *remoteTerm) HasSelection() bool { return false }
func (r *remoteTerm) Selection() string  { return "" }
func (r *remoteTerm) ClearSelection()    { r.send(request{Op: opClearSel}) }

func (r *remoteTerm) Focus() {
	r.claim()
	r.send(request{Op: opFocus})
}

func (r *remoteTerm) Blur() { r.send(request{Op: opBlur}) }

// Close ends the session on the server. Leaving cade only drops the connection.
func (r *remoteTerm) Close() {
	r.send(request{Op: opKill})
	r.conn.Close()
}
