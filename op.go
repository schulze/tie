package main

import (
	"fmt"
	"strings"
)

// An operator acts on a range given by a motion (cw), by doubling the
// operator (cc: whole lines) or by dot in visual mode.
type operator struct {
	fn     func(e *Editor, r Range, line bool, c *ctx)
	change bool
}

var operators map[Key]operator

func init() {
	operators = map[Key]operator{
		'c': {opCut, true},
		'y': {opYank, false},
		'>': {opShift, true},
		'<': {opShift, true},
	}
}

// operate runs operator op. If mk is 0, the motion (with an optional
// count) is read from the keyboard.
func (e *Editor) operate(c *ctx, op, mk Key) {
	f := e.f
	n := c.count
	if mk == 0 {
		n2 := 0
		for {
			mk = e.in.Get()
			if mk >= '1' && mk <= '9' || mk == '0' && n2 > 0 {
				n2 = n2*10 + int(mk-'0')
				continue
			}
			break
		}
		if n2 > 0 {
			n = max(n, 1) * n2
		}
	}
	mc := &ctx{count: n, reg: c.reg, key: mk, op: op}
	var r Range
	var line bool
	switch {
	case mk == op:
		ln := f.line(f.cur)
		last := min(ln+mc.n()-1, f.t.Nlines()-1)
		r, line = Range{f.t.Bol(ln), f.lineEnd(last)}, true
	default:
		m, ok := motions[mk]
		if !ok {
			return
		}
		mc.kind = m.kind
		q, ok := m.fn(e, mc)
		if !ok {
			return
		}
		r, line = e.motionRange(f.cur, q, mc.kind)
	}
	o := operators[op]
	o.fn(e, r, line, c)
	c.changed = o.change
}

func (e *Editor) motionRange(a, b int, kind mkind) (Range, bool) {
	f := e.f
	if b < a {
		a, b = b, a
	}
	switch kind {
	case inclusive:
		b = min(b+1, f.t.Len())
	case linewise:
		return Range{f.bol(a), f.lineEnd(f.line(b))}, true
	}
	return Range{a, b}, false
}

func (e *Editor) lineMsg(n int, what string) {
	if n >= 3 {
		e.msg = fmt.Sprintf("%d lines %s", n, what)
	}
}

// opCut is c: the text goes into the register.
func opCut(e *Editor, r Range, line bool, c *ctx) {
	if r.Empty() {
		return
	}
	f := e.f
	nl := f.lastLine(r) - f.line(r.Q0) + 1
	e.setReg(c.reg, f.delete(r.Q0, r.Q1), line)
	f.cur = r.Q0
	if line {
		f.cur = f.firstNonBlank(f.line(r.Q0))
		e.lineMsg(nl, "cut")
	}
}

func opYank(e *Editor, r Range, line bool, c *ctx) {
	if r.Empty() {
		return
	}
	f := e.f
	e.setReg(c.reg, f.t.Runes(r.Q0, r.Q1), line)
	if !line || f.line(r.Q0) < f.line(f.cur) {
		f.cur = r.Q0
	}
	if line {
		e.lineMsg(f.line(r.Q1-1)-f.line(r.Q0)+1, "yanked")
	}
}

func opShift(e *Editor, r Range, line bool, c *ctx) {
	f := e.f
	l0, l1 := f.line(r.Q0), f.lastLine(r)
	dir := e.opt.sw
	if c.key == '<' {
		dir = -dir
	}
	for ln := l1; ln >= l0; ln-- {
		if f.emptyLine(ln) {
			continue
		}
		q, p := f.t.Bol(ln), f.firstNonBlank(ln)
		w := max(0, e.col(p)+dir)
		f.replace(q, p, e.makeIndent(w))
	}
	f.cur = f.firstNonBlank(l0)
	e.lineMsg(l1-l0+1, "shifted")
}

// makeIndent returns white space w columns wide.
func (e *Editor) makeIndent(w int) []rune {
	var s []rune
	if !e.opt.et {
		for ; w >= e.opt.ts; w -= e.opt.ts {
			s = append(s, '\t')
		}
	}
	for ; w > 0; w-- {
		s = append(s, ' ')
	}
	return s
}

// indent returns the leading white space of line ln if autoindent is on.
func (e *Editor) indent(ln int) []rune {
	if !e.opt.ai {
		return nil
	}
	f := e.f
	return f.t.Runes(f.t.Bol(ln), f.firstNonBlank(ln))
}

// insertMode reads text until ESC and returns what was typed, for
// repetition with a count.
func (e *Editor) insertMode() []rune {
	f := e.f
	e.mode = Insert
	var typed []rune
	add := func(r rune) {
		f.insert(f.cur, []rune{r})
		f.cur++
		typed = append(typed, r)
	}
	for {
		e.draw()
		k := e.in.Get()
		switch k {
		case KeyEsc, KeyEOF:
			return typed
		case KeyEnter, '\n':
			ind := e.indent(f.line(f.cur))
			add('\n')
			for _, r := range ind {
				add(r)
			}
		case KeyBS, ctrl('h'):
			if f.cur > 0 {
				f.delete(f.cur-1, f.cur)
				f.cur--
			}
			if len(typed) > 0 {
				typed = typed[:len(typed)-1]
			}
		case KeyDelete:
			if f.cur < f.t.Len() {
				f.delete(f.cur, f.cur+1)
			}
		case ctrl('w'), ctrl('u'):
			q, b := f.cur, f.bol(f.cur)
			if k == ctrl('u') {
				q = b
			} else {
				for q > b && isBlank(f.t.At(q-1)) {
					q--
				}
				for c := class(f.t.At(q-1), false); q > b && c != 0 && class(f.t.At(q-1), false) == c; {
					q--
				}
			}
			f.delete(q, f.cur)
			f.cur = q
			typed = nil
		case ctrl('v'):
			if k := e.in.Get(); k >= 0 {
				add(rune(k))
			}
		case '\t':
			if e.opt.et {
				for w := e.opt.ts - e.col(f.cur)%e.opt.ts; w > 0; w-- {
					add(' ')
				}
			} else {
				add('\t')
			}
		case KeyLeft, KeyRight, KeyUp, KeyDown, KeyHome, KeyEnd:
			e.insertMove(k)
			typed = nil
		default:
			if k >= ' ' {
				add(rune(k))
			}
		}
	}
}

func (e *Editor) insertMove(k Key) {
	f := e.f
	ln := f.line(f.cur)
	switch k {
	case KeyLeft:
		if f.cur > f.bol(f.cur) {
			f.cur--
		}
	case KeyRight:
		if f.cur < f.eol(f.cur) {
			f.cur++
		}
	case KeyUp:
		if ln > 0 {
			f.cur = e.atCol(ln-1, e.col(f.cur))
		}
	case KeyDown:
		if ln+1 < f.t.Nlines() {
			f.cur = e.atCol(ln+1, e.col(f.cur))
		}
	case KeyHome:
		f.cur = f.bol(f.cur)
	case KeyEnd:
		f.cur = f.eol(f.cur)
	}
}

// leaveInsert returns to normal mode; as in vi the cursor backs up
// onto the last character typed.
func (e *Editor) leaveInsert() {
	e.mode = Normal
	if f := e.f; f.cur > f.bol(f.cur) {
		f.cur--
	}
}

// expand replaces % in a shell command with the file name, as vi does.
func (e *Editor) expand(cmd string) string {
	var b strings.Builder
	for i := 0; i < len(cmd); i++ {
		switch {
		case cmd[i] == '\\' && i+1 < len(cmd) && cmd[i+1] == '%':
			b.WriteByte('%')
			i++
		case cmd[i] == '%':
			b.WriteString(e.f.name)
		default:
			b.WriteByte(cmd[i])
		}
	}
	return b.String()
}
