package term

// cade patch: expose the scrollback viewport so hosts can draw a scrollbar.

// ScrollInfo returns how many rows the viewport is scrolled up from the bottom
// and the maximum it can scroll. max is 0 when there is no scrollback (or the
// alternate screen is active).
func (vt *Model) ScrollInfo() (offset int, max int) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	return vt.scrollOffset, vt.maxScrollOffset()
}

// ScrollTo sets the viewport offset from the bottom, clamped to the scrollback.
func (vt *Model) ScrollTo(offset int) {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	if vt.mode.smcup {
		return
	}
	vt.scrollOffset = offset
	vt.clampScrollOffset()
	vt.invalidate()
}
