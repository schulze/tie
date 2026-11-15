package main

import "time"

// The mouse works as in acme.
//
//	button 1   click: put the cursor there.  Double click: select the
//	           word, the line (at either end of a line) or the text
//	           between brackets or quotes.  Sweep: select the text.
//	button 2   execute the word clicked on, or dot if the click is in
//	           dot, or the text swept.  Dot stays as it was, so
//	           clicking on |sort pipes dot through sort.
//	button 3   look at the name clicked on, or dot, or the text swept.
//	1-2 chord  with button 1 down, click 2: cut dot into the register.
//	1-3 chord  with button 1 down, click 3: paste over dot.
//	wheel      scroll.
//
// Most terminals still give their own selection with shift held down.

const doubleClick = 400 * time.Millisecond

// sweep is the text swept from a to b, both ends included.
func (e *Editor) sweep(a, b int) Range {
	if a == b {
		return Range{a, a}
	}
	return Range{min(a, b), min(max(a, b)+1, e.f.t.Len())}
}

func mouseCmd(e *Editor, c *ctx) {
	e.keepDot = true
	m := e.in.mouse
	if m.release || m.drag {
		return
	}
	if m.y == 1 && m.b < 3 { // the buffer line
		for _, t := range e.tabs {
			if m.x >= t.x0 && m.x < t.x1 {
				e.switchTo(t.f)
			}
		}
		return
	}
	switch m.b {
	case 0:
		e.mouseSelect(m, c)
	case 1, 2:
		e.mouseSweep(m)
	case 64, 65:
		k := ctrl('y')
		if m.b == 65 {
			k = ctrl('e')
		}
		scrollCmd(e, &ctx{key: k, count: 3})
	}
}

// nextMouse waits for the next mouse event. Any other key ends the
// gesture and is kept for the main loop.
func (e *Editor) nextMouse() (Mouse, bool) {
	k := e.in.Get()
	if k != KeyMouse {
		e.in.push([]Key{k})
		return Mouse{}, false
	}
	return e.in.mouse, true
}

func (e *Editor) mouseSelect(m Mouse, c *ctx) {
	f, q := e.posAt(m.x, m.y)
	e.f = f // a click in the tag goes there, a click in the body back
	now := time.Now()
	double := now.Sub(e.click.at) < doubleClick && q == e.click.q
	e.click.at, e.click.q = now, q
	if double {
		e.click.at = time.Time{}
		f.dot = e.unitAt(q, true)
		f.cur = f.dot.Q0
		return
	}
	anchor := q
	f.cur, f.dot = q, Range{q, q}
	for {
		e.draw()
		ev, ok := e.nextMouse()
		if !ok || ev.b == 0 && ev.release {
			return
		}
		switch {
		case ev.b == 0 && ev.drag:
			if g, p := e.posAt(ev.x, ev.y); g == f {
				f.cur, f.dot = p, e.sweep(anchor, p)
			}
		case ev.b == 1 && !ev.release && !f.dot.Empty(): // cut
			e.setReg(0, f.delete(f.dot.Q0, f.dot.Q1), false)
			f.cur, f.dot = f.dot.Q0, Range{f.dot.Q0, f.dot.Q0}
			c.changed = true
		case ev.b == 2 && !ev.release: // paste
			if e.pasteOver(0) == nil {
				c.changed = true
			}
		}
	}
}

// mouseSweep handles buttons 2 and 3. The text may be in the tag or
// in the body; either way it works on the buffer, and dot stays.
func (e *Editor) mouseSweep(m Mouse) {
	f, start := e.posAt(m.x, m.y)
	end := start
	old := f.dot
	for {
		ev, ok := e.nextMouse()
		if !ok || ev.b == m.b && ev.release {
			break
		}
		if g, p := e.posAt(ev.x, ev.y); ev.drag && g == f {
			end = p
			f.dot = e.sweep(start, end)
			e.draw() // show the sweep as dot while it lasts
		}
	}
	f.dot = old
	var r Range
	switch {
	case end != start:
		r = e.sweep(start, end)
	case !old.Empty() && old.Q0 <= start && start < old.Q1:
		r = old
	case m.b == 1:
		r = f.wordAt(start)
	default:
		e.msgErr(e.lookAt(f, start))
		return
	}
	text, from := f.t.String(r.Q0, r.Q1), r.Q0
	if f != e.buffer() {
		from = e.buffer().cur // text in the tag: search the buffer
	}
	if m.b == 1 {
		e.msgErr(e.execute(text))
	} else {
		e.msgErr(e.look(text, from))
	}
}
