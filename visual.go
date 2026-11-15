package main

// Visual mode makes dot. v selects characters, V whole lines, the
// select button (keySelect) the word; motions move one end, and the
// select button grows dot. A command then applies to dot: the
// operators as in vi, a command line on dot (:), or the execute
// and look buttons.
//
// v with a non-empty dot (after a search, say) starts from that dot, so
// "/re" "v" "c" changes the match, and period repeats that on the next
// match found with n.
var visualOps map[Key]func(e *Editor, c *ctx, line bool)

func init() {
	cut := func(e *Editor, c *ctx, line bool) { opCut(e, e.f.dot, line, c); c.changed = true }
	visualOps = map[Key]func(*Editor, *ctx, bool){
		'c': cut, KeyDelete: cut,
		'y': func(e *Editor, c *ctx, line bool) {
			opYank(e, e.f.dot, line, c)
			e.keepDot = true // to do something else with it
		},
		'p': vPaste,
		'J': vJoin,
		'~': vCase, 'u': vCase, 'U': vCase,
		'r': vReplace,
		':': vCommand,
		'<': vShift, '>': vShift,
		keyExecute: vExecute,
		keyLook:    vLook,
	}
}

func visualCmd(e *Editor, c *ctx) {
	f := e.f
	e.mode = Visual
	if c.key == 'V' {
		e.mode = VisualLine
	}
	anchor := f.cur
	if c.key == 'v' && !f.dot.Empty() {
		anchor, f.cur = f.dot.Q0, f.dot.last()
	}
	for {
		e.clamp()
		e.setDot(anchor)
		e.draw()
		vc := &ctx{}
		k := e.readPrefix(vc)
		vc.key = k
		e.keepWant = false
		switch k {
		case KeyEsc, KeyEOF, ctrl('c'):
			e.mode = Normal
			return
		case 'o':
			anchor, f.cur = f.cur, anchor
			continue
		case keySelect: // grow: lines, paragraph, file
			f.dot = e.grow(f.dot)
			anchor, f.cur = f.dot.Q0, f.dot.last()
			if f.lines(f.dot) == f.dot {
				e.mode = VisualLine
				f.cur = f.bol(f.cur)
			}
			continue
		case keyTag: // to the tag (or back), keeping dot
			e.mode = Normal
			tagCmd(e, vc)
			return
		case KeyMouse: // the mouse takes over in normal mode
			e.mode = Normal
			e.keepDot = true
			e.in.push([]Key{KeyMouse})
			return
		case 'v', 'V':
			m := Visual
			if k == 'V' {
				m = VisualLine
			}
			if e.mode == m {
				e.mode = Normal
				return
			}
			e.mode = m
			continue
		}
		if op, ok := visualOps[k]; ok {
			line := e.mode == VisualLine
			e.mode = Normal
			f.marks['<'] = f.dot.Q0
			f.marks['>'] = f.dot.last()
			op(e, vc, line)
			c.changed = vc.changed
			return
		}
		if m, ok := motions[k]; ok && e.move(vc, m) && !e.keepWant {
			f.want = e.col(f.cur)
		}
	}
}

func (e *Editor) setDot(anchor int) {
	f := e.f
	a, b := anchor, f.cur
	if b < a {
		a, b = b, a
	}
	b = min(b+1, f.t.Len())
	if e.mode == VisualLine {
		a, b = f.bol(a), f.lineEnd(f.line(max(a, b-1)))
	}
	f.dot = Range{a, b}
}

// vPaste replaces dot with a register, like acme's Paste.
func vPaste(e *Editor, c *ctx, line bool) {
	err := e.pasteOver(c.reg)
	e.msgErr(err)
	c.changed = err == nil
}

func vJoin(e *Editor, c *ctx, line bool) {
	f := e.f
	l0 := f.line(f.dot.Q0)
	e.join(l0, max(1, f.lastLine(f.dot)-l0))
	c.changed = true
}

func vCase(e *Editor, c *ctx, line bool) {
	f := e.f
	d := f.dot
	f.replace(d.Q0, d.Q1, mapCase(f.t.Runes(d.Q0, d.Q1), c.key))
	f.cur = d.Q0
	c.changed = true
}

func vReplace(e *Editor, c *ctx, line bool) {
	k := e.in.Get()
	if k < ' ' {
		return
	}
	f := e.f
	d := f.dot
	s := f.t.Runes(d.Q0, d.Q1)
	for i, r := range s {
		if r != '\n' {
			s[i] = rune(k)
		}
	}
	f.replace(d.Q0, d.Q1, s)
	f.cur = d.Q0
	c.changed = true
}

// vCommand opens the command line; with no address its commands apply
// to dot.
func vCommand(e *Editor, c *ctx, line bool) {
	e.exline()
	c.changed = e.buffer().pend != nil // period repeats an edit on dot
}

func vShift(e *Editor, c *ctx, line bool) {
	opShift(e, e.f.dot, true, c)
	c.changed = true
}

func vLook(e *Editor, c *ctx, line bool) {
	e.msgErr(e.look(e.dotText(), e.f.dot.Q0))
}

func vExecute(e *Editor, c *ctx, line bool) {
	f := e.f
	// The text is the command, not what it works on.
	text := f.t.String(f.dot.Q0, f.dot.Q1)
	f.cur, f.dot = f.dot.Q0, Range{f.dot.Q0, f.dot.Q0}
	e.msgErr(e.execute(text))
}
