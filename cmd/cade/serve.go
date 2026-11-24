package main

import (
	"cmp"
	"encoding/gob"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/term"
)

// Wire protocol between cade and cade --serve: gob over a unix socket. Each
// connection starts with an opHello reply, then one request: opList, or opOpen
// to attach to one Claude session for the rest of the connection.
const (
	opHello = "hello" // reply: Text is the server version
	opList  = "list"  // request: running session ids; reply: IDs
	opOpen  = "open"  // request: attach to Open.ID, starting it if Open.Path is set
	opOK    = "ok"    // reply to opOpen: attached
	opErr   = "err"   // reply: Text is the error

	// requests on an attached connection
	opKey        = "key"
	opMouse      = "mouse"
	opPasteStart = "paste-start"
	opPasteEnd   = "paste-end"
	opFocus      = "focus"
	opBlur       = "blur"
	opTheme      = "theme"  // N is the vaxis.ColorThemeMode
	opSize       = "size"   // N × M columns × rows; the last client to send it wins
	opScroll     = "scroll" // N is the offset from the bottom
	opClearSel   = "clear-sel"
	opReady      = "ready" // the client drew the last frame and takes the next
	opKill       = "kill"  // end the session

	// replies on an attached connection
	opFrame  = "frame"
	opBell   = "bell"
	opNotify = "notify" // Text title, Body body
	opTitle  = "title"
	opShape  = "shape"  // mouse shape
	opClip   = "clip"   // the application copied Text
	opCopy   = "copy"   // this client's mouse selection Text, to copy
	opClosed = "closed" // the session ended
)

// request is a message to the server; Op says which fields count. gob drops
// zero values, so decode each into a fresh request.
type request struct {
	Op    string
	Open  *openReq
	Key   vaxis.Key
	Mouse vaxis.Mouse
	N, M  int
}

// openReq names a session and, with Path set, the command to start it with.
type openReq struct {
	ID   string
	Path string
	Args []string
	Dir  string
	Env  []string
	W, H int
}

// reply is a message from the server; Op says which fields count.
type reply struct {
	Op    string
	Text  string
	Body  string
	IDs   []string
	Frame *frame
}

// frame is the visible screen of a session, scrolled as the server has it.
type frame struct {
	W, H     int
	Off, Max int // ScrollInfo
	Snap     term.Snapshot
}

// sockPath is the server's socket, private to the user.
func sockPath() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "cade.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("cade-%d.sock", os.Getuid()))
}

// newModel makes a pane terminal. WithVaxis picks xterm-kitty on a
// kitty-protocol host, but that terminfo is often not installed: zsh then
// drops its key bindings and redraws by reprinting.
func newModel(opts ...term.Option) *term.Model {
	t := term.New(opts...)
	t.TERM = "xterm-256color"
	return t
}

// server holds the running Claude sessions, by id.
type server struct {
	mu       sync.Mutex
	sessions map[string]*hosted
}

// hosted is one session's headless terminal and the clients attached to it.
type hosted struct {
	t      *term.Model
	mu     sync.Mutex
	w, h   int
	subs   map[*sub]bool
	closed bool
}

// sub is one attached client. Its writer sends queued events, and a frame when
// the screen changed and the client asked for one (opReady), so a client that
// isn't drawing the session gets no frames.
type sub struct {
	enc         *gob.Encoder
	mu          sync.Mutex
	want, dirty bool
	queue       []reply
	kick        chan struct{}
	done        chan struct{}
}

// serve runs the server until it fails; it reports the exit code.
func serve() int {
	path := sockPath()
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		fmt.Fprintln(os.Stderr, "cade: server already running on", path)
		return 1
	}
	os.Remove(path) // stale: nobody listens
	syscall.Umask(0o077)
	ln, err := net.Listen("unix", path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cade:", err)
		return 1
	}
	s := &server{sessions: map[string]*hosted{}}
	for {
		c, err := ln.Accept()
		if err != nil {
			fmt.Fprintln(os.Stderr, "cade:", err)
			return 1
		}
		go s.conn(c)
	}
}

func (s *server) conn(c net.Conn) {
	defer c.Close()
	enc, dec := gob.NewEncoder(c), gob.NewDecoder(c)
	var r request
	if enc.Encode(reply{Op: opHello, Text: version}) != nil || dec.Decode(&r) != nil {
		return
	}
	switch {
	case r.Op == opList:
		s.mu.Lock()
		ids := make([]string, 0, len(s.sessions))
		for id := range s.sessions {
			ids = append(ids, id)
		}
		s.mu.Unlock()
		enc.Encode(reply{Op: opList, IDs: ids})
		return
	case r.Op != opOpen || r.Open == nil:
		return
	}
	h, me, err := s.open(r.Open, enc)
	if err != nil {
		enc.Encode(reply{Op: opErr, Text: err.Error()})
		return
	}
	defer h.leave(me)
	if enc.Encode(reply{Op: opOK}) != nil {
		return
	}
	go me.write(h)
	for {
		var r request
		if dec.Decode(&r) != nil {
			return
		}
		if r.Op == opKill {
			h.t.Close()
			return
		}
		h.handle(me, r)
	}
}

// open attaches a new sub to session o.ID, starting it first if it isn't running.
func (s *server) open(o *openReq, enc *gob.Encoder) (*hosted, *sub, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.sessions[o.ID]
	if h == nil {
		if o.Path == "" {
			return nil, nil, errors.New("session not running")
		}
		h = &hosted{t: newModel(term.WithKittyKeyboard(true)), w: o.W, h: o.H, subs: map[*sub]bool{}}
		h.t.Attach(func(ev vaxis.Event) { s.event(o.ID, h, ev) })
		cmd := &exec.Cmd{Path: o.Path, Args: o.Args, Dir: o.Dir, Env: o.Env}
		if err := h.t.StartWithSize(cmd, max(o.W, 1), max(o.H, 1)); err != nil {
			return nil, nil, err
		}
		s.sessions[o.ID] = h
	}
	me := &sub{enc: enc, dirty: true, kick: make(chan struct{}, 1), done: make(chan struct{})}
	h.mu.Lock()
	h.subs[me] = true
	if h.closed {
		me.push(reply{Op: opClosed})
	}
	h.mu.Unlock()
	h.resize(o.W, o.H) // the newest client is the active one
	return h, me, nil
}

// event passes a terminal event of session id on to its clients.
func (s *server) event(id string, h *hosted, ev vaxis.Event) {
	var r reply
	switch ev := ev.(type) {
	case vaxis.Redraw:
		h.each(func(c *sub) { c.mark() })
		return
	case term.EventBell:
		r.Op = opBell
	case term.EventNotify:
		r = reply{Op: opNotify, Text: ev.Title, Body: ev.Body}
	case term.EventTitle:
		r = reply{Op: opTitle, Text: string(ev)}
	case term.EventMouseShape:
		r = reply{Op: opShape, Text: string(ev.Shape)}
	case term.EventClipboard:
		r = reply{Op: opClip, Text: string(ev)}
	case term.EventClosed:
		s.mu.Lock()
		if s.sessions[id] == h {
			delete(s.sessions, id)
		}
		s.mu.Unlock()
		h.mu.Lock()
		h.closed = true
		h.mu.Unlock()
		r.Op = opClosed
	default:
		return
	}
	h.each(func(c *sub) { c.push(r) })
}

// each runs f on every attached client.
func (h *hosted) each(f func(*sub)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subs {
		f(c)
	}
}

func (h *hosted) leave(c *sub) {
	h.mu.Lock()
	delete(h.subs, c)
	h.mu.Unlock()
	close(c.done)
}

func (h *hosted) resize(w, hh int) {
	if w < 1 || hh < 1 {
		return
	}
	h.mu.Lock()
	h.w, h.h = w, hh
	h.mu.Unlock()
	h.t.Resize(w, hh)
	h.each(func(c *sub) { c.mark() })
}

// handle applies request r from client c.
func (h *hosted) handle(c *sub, r request) {
	switch r.Op {
	case opKey:
		h.t.Update(r.Key)
	case opMouse:
		h.t.Update(r.Mouse)
		// the host terminal's copy-on-select can't run under a client; the client copies
		if r.Mouse.Button == vaxis.MouseLeftButton && r.Mouse.EventType == vaxis.EventRelease && h.t.HasSelection() {
			c.push(reply{Op: opCopy, Text: h.t.Selection()})
		}
	case opPasteStart:
		h.t.Update(vaxis.PasteStartEvent{})
	case opPasteEnd:
		h.t.Update(vaxis.PasteEndEvent{})
	case opFocus:
		h.t.Focus()
	case opBlur:
		h.t.Blur()
	case opTheme:
		h.t.Update(vaxis.ColorThemeUpdate{Mode: vaxis.ColorThemeMode(r.N)})
	case opSize:
		h.resize(r.N, r.M)
		return
	case opScroll:
		h.t.ScrollTo(r.N)
	case opClearSel:
		h.t.ClearSelection()
	case opReady:
		c.mu.Lock()
		c.want = true
		c.mu.Unlock()
		c.poke()
		return
	}
	h.each(func(c *sub) { c.mark() })
}

// frame is h's screen now.
func (h *hosted) frame() *frame {
	off, m := h.t.ScrollInfo()
	h.mu.Lock()
	w, hh := h.w, h.h
	h.mu.Unlock()
	return &frame{W: w, H: hh, Off: off, Max: m, Snap: h.t.Snapshot()}
}

func (c *sub) poke() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

func (c *sub) mark() {
	c.mu.Lock()
	c.dirty = true
	c.mu.Unlock()
	c.poke()
}

func (c *sub) push(r reply) {
	c.mu.Lock()
	c.queue = append(c.queue, r)
	c.mu.Unlock()
	c.poke()
}

// write sends c's queue and frames until c leaves or the connection fails.
func (c *sub) write(h *hosted) {
	for {
		select {
		case <-c.done:
			return
		case <-c.kick:
		}
		c.mu.Lock()
		q := c.queue
		c.queue = nil
		send := c.want && c.dirty
		if send {
			c.want, c.dirty = false, false
		}
		c.mu.Unlock()
		if send {
			q = append([]reply{{Op: opFrame, Frame: h.frame()}}, q...) // a copy or close lands after the screen it belongs to
		}
		for _, r := range q {
			if c.enc.Encode(r) != nil {
				return
			}
		}
	}
}

// dial connects to the server and reads its hello; with start set it starts
// the server first when none runs.
func dial(start bool) (net.Conn, *gob.Decoder, string, error) {
	path := sockPath()
	c, err := net.Dial("unix", path)
	if err != nil && start {
		var exe string
		if exe, err = os.Executable(); err != nil {
			return nil, nil, "", err
		}
		cmd := exec.Command(exe, "--serve")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // outlives this cade and its terminal
		if err = cmd.Start(); err != nil {
			return nil, nil, "", err
		}
		cmd.Process.Release()
		for range 50 {
			time.Sleep(40 * time.Millisecond)
			if c, err = net.Dial("unix", path); err == nil {
				break
			}
		}
	}
	if err != nil {
		return nil, nil, "", err
	}
	dec := gob.NewDecoder(c)
	var hello reply
	if err := dec.Decode(&hello); err != nil || hello.Op != opHello {
		c.Close()
		return nil, nil, "", cmp.Or(err, errors.New("not a cade server: "+path))
	}
	return c, dec, hello.Text, nil
}

// running lists the server's session ids and version; none when no server runs.
func running() ([]string, string) {
	c, dec, ver, err := dial(false)
	if err != nil {
		return nil, ""
	}
	defer c.Close()
	var r reply
	if gob.NewEncoder(c).Encode(request{Op: opList}) != nil || dec.Decode(&r) != nil {
		return nil, ver
	}
	return r.IDs, ver
}
