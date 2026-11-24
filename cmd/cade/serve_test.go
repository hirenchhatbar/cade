package main

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/term"
)

// A session started through the server shows in its frames and list, outlives
// its client, and ends on Close.
func TestServeSession(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	go serve()
	for range 100 { // wait for the socket
		if c, _, _, err := dial(false); err == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	evs := make(chan vaxis.Event, 64)
	post := func(_ pane, ev vaxis.Event) { evs <- ev }
	wait := func(want func(vaxis.Event) bool) {
		t.Helper()
		for {
			select {
			case ev := <-evs:
				if want(ev) {
					return
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timed out")
			}
		}
	}
	screen := func(r *remoteTerm) string {
		r.mu.Lock()
		defer r.mu.Unlock()
		var b strings.Builder
		for _, c := range r.fr.Snap.Cells {
			b.WriteString(c.Cell.Grapheme)
		}
		return b.String()
	}

	r, err := openRemote("s1", exec.Command("sh", "-c", "printf hello; exec cat"), 40, 5, post)
	if err != nil {
		t.Fatal(err)
	}
	for !strings.Contains(screen(r), "hello") {
		r.send(request{Op: opReady})
		wait(func(ev vaxis.Event) bool { _, ok := ev.(vaxis.Redraw); return ok })
	}
	r.conn.Close() // cade quits: the session stays
	wait(func(ev vaxis.Event) bool { _, ok := ev.(term.EventClosed); return ok })
	ids, ver := running()
	if !slices.Equal(ids, []string{"s1"}) || ver != version {
		t.Fatalf("running %v %q, want [s1] %q", ids, ver, version)
	}

	r, err = openRemote("s1", nil, 40, 5, post) // attach only
	if err != nil {
		t.Fatal(err)
	}
	r.Update(vaxis.Key{Keycode: 'x', Text: "x"})
	for !strings.Contains(screen(r), "hellox") {
		r.send(request{Op: opReady})
		wait(func(ev vaxis.Event) bool { _, ok := ev.(vaxis.Redraw); return ok })
	}
	if _, err := openRemote("nope", nil, 40, 5, post); err == nil {
		t.Fatal("attach to a session that isn't running should fail")
	}
	r.Close()
	for range 100 {
		if ids, _ := running(); len(ids) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Close should end the session")
}
