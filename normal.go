package main

import (
	"fmt"
	"syscall"
)

// cmd is a normal mode command. change marks commands that always
// change the text; operators decide for themselves (y does not).
type cmd struct {
	fn     func(*Editor, *ctx)
	change bool
}

// normal maps keys to commands. Keys not found here are looked up in
// motions, which then simply move the cursor.
var normal map[Key]cmd

func init() {
	ins := cmd{insertCmd, true}
	op := cmd{operatorCmd, false}
	paste := cmd{pasteCmd, true}
	scroll := cmd{scrollCmd, false}
	vis := cmd{visualCmd, false}
	normal = map[Key]cmd{
		'i': ins, 'a': ins, 'I': ins, 'A': ins, 'o': ins, 'O': ins,

		'c': op, 'y': op, '<': op, '>': op,
		'x': alias('c', 'l'), KeyDelete: alias('c', 'l'),

		'p': paste, 'P': paste,
		'J': {joinCmd, true},
		'r': {replaceCharCmd, true},
		'~': {tildeCmd, true},
		'u': {undoCmd, false}, ctrl('r'): {redoCmd, false},
		'.': {period, false},
		'v': vis, 'V': vis,
		':': {colonCmd, false},
		'?': {helpCmd, false},

		'm': {markCmd, false},
		'@': {atCmd, false},
		'd': {func(e *Editor, c *ctx) { e.msgErr(e.ex("del")) }, false},
		'Q': {func(e *Editor, c *ctx) { e.msgErr(e.ex("exit")) }, false},

		ctrl('d'): scroll, ctrl('u'): scroll, ctrl('f'): scroll, ctrl('b'): scroll,
		ctrl('e'): scroll, ctrl('y'): scroll, KeyPgDn: scroll, KeyPgUp: scroll,

		ctrl('g'): {func(e *Editor, c *ctx) { e.info() }, false},
		ctrl('l'): {func(e *Editor, c *ctx) { e.screen = nil }, false},
		ctrl('^'): {altCmd, false},
		ctrl('z'): {suspendCmd, false},
		KeyEsc:    {escCmd, false},
		KeyMouse:  {mouseCmd, false},
	}
}

// The buttons go last: they take the place of vi commands.
func init() {
	normal[keySelect] = cmd{selectCmd, false}
	normal[keyExecute] = cmd{executeCmd, false}
	normal[keyLook] = cmd{lookCmd, false}
	normal[keyTag] = cmd{tagCmd, false}
	normal[keyBuffers] = cmd{buffersCmd, false}
}

// alias makes a command that is an operator with a fixed motion,
// like x for dl.
func alias(op, m Key) cmd {
	return cmd{func(e *Editor, c *ctx) { e.operate(c, op, m) }, false}
}

func operatorCmd(e *Editor, c *ctx) { e.operate(c, c.key, 0) }

func insertCmd(e *Editor, c *ctx) {
	f := e.f
	nl := false
	switch c.key {
	case 'a':
		if f.cur < f.t.Len() && f.t.At(f.cur) != '\n' {
			f.cur++
		}
	case 'A':
		f.cur = f.eol(f.cur)
	case 'I':
		f.cur = f.firstNonBlank(f.line(f.cur))
	case 'o':
		ind := e.indent(f.line(f.cur))
		q := f.eol(f.cur)
		f.insert(q, append([]rune{'\n'}, ind...))
		f.cur = q + 1 + len(ind)
		nl = true
	case 'O':
		ind := e.indent(f.line(f.cur))
		q := f.bol(f.cur)
		f.insert(q, append(ind, '\n'))
		f.cur = q + len(ind)
		nl = true
	}
	typed := e.insertMode()
	if c.count > 1 && len(typed) > 0 {
		unit := typed
		if nl {
			unit = append([]rune{'\n'}, typed...)
		}
		for i := 1; i < c.count; i++ {
			f.insert(f.cur, unit)
			f.cur += len(unit)
		}
	}
	e.leaveInsert()
}

func pasteCmd(e *Editor, c *ctx) {
	r := e.getReg(c.reg)
	if r == nil || len(r.text) == 0 {
		e.msg = "Nothing in register"
		return
	}
	f := e.f
	var text []rune
	for range c.n() {
		text = append(text, r.text...)
	}
	if r.line {
		q := f.bol(f.cur)
		if c.key == 'p' {
			q = f.lineEnd(f.line(f.cur))
		}
		f.insert(q, text)
		f.cur = f.firstNonBlank(f.line(q))
		return
	}
	q := f.cur
	if c.key == 'p' && q < f.t.Len() && f.t.At(q) != '\n' {
		q++
	}
	f.insert(q, text)
	f.cur = q + len(text) - 1
}

func joinCmd(e *Editor, c *ctx) { e.join(e.f.line(e.f.cur), max(1, c.count-1)) }

// join joins line ln with the n lines that follow it.
func (e *Editor) join(ln, n int) {
	f := e.f
	for range n {
		q := f.t.Eol(ln)
		if q >= f.t.Len()-1 {
			break
		}
		j := q + 1
		for f.t.At(j) == ' ' || f.t.At(j) == '\t' {
			j++
		}
		sep := []rune{' '}
		if r := f.t.At(j); r == '\n' || r == eof || r == ')' || q == f.t.Bol(ln) {
			sep = nil
		}
		f.replace(q, j, sep)
		f.cur = q
	}
}

func replaceCharCmd(e *Editor, c *ctx) {
	k := e.in.Get()
	if k == ctrl('v') {
		k = e.in.Get()
	} else if k == KeyEsc || k < 0 {
		return
	}
	f := e.f
	n := c.n()
	if f.cur+n > f.eol(f.cur) {
		return
	}
	if k == KeyEnter || k == '\n' {
		f.replace(f.cur, f.cur+n, []rune{'\n'})
		f.cur++
		return
	}
	s := make([]rune, n)
	for i := range s {
		s[i] = rune(k)
	}
	f.replace(f.cur, f.cur+n, s)
	f.cur += n - 1
}

func tildeCmd(e *Editor, c *ctx) {
	f := e.f
	q1 := min(f.cur+c.n(), f.eol(f.cur))
	if q1 <= f.cur {
		return
	}
	f.replace(f.cur, q1, mapCase(f.t.Runes(f.cur, q1), '~'))
	f.cur = q1
}

func undoCmd(e *Editor, c *ctx) {
	for range c.n() {
		q, ok := e.f.undoStep()
		if !ok {
			e.msg = "Already at oldest change"
			return
		}
		e.f.cur = q
	}
}

func redoCmd(e *Editor, c *ctx) {
	for range c.n() {
		q, ok := e.f.redoStep()
		if !ok {
			e.msg = "Already at newest change"
			return
		}
		e.f.cur = q
	}
}

func colonCmd(e *Editor, c *ctx) { e.exline() }

func markCmd(e *Editor, c *ctx) {
	if k := e.in.Get(); k > ' ' {
		e.f.marks[rune(k)] = e.f.cur
	}
}

// atCmd executes a register as if its text were typed.
func atCmd(e *Editor, c *ctx) {
	r := rune(e.in.Get())
	if r == '@' {
		r = e.lastAt
	}
	reg := e.getReg(r)
	if reg == nil {
		e.msg = "Nothing in register"
		return
	}
	e.lastAt = r
	e.keepDot = true
	var keys []Key
	for range c.n() {
		for _, r := range reg.text {
			keys = append(keys, Key(r))
		}
	}
	e.in.push(keys)
}

func scrollCmd(e *Editor, c *ctx) {
	f := e.f
	nl := f.t.Nlines()
	rows := e.rows()
	ln := f.line(f.cur)
	switch c.key {
	case ctrl('d'), ctrl('u'):
		n := max(1, rows/2)
		if c.key == ctrl('u') {
			n = -n
		}
		f.top = max(0, min(f.top+n, nl-1))
		f.cur = f.firstNonBlank(max(0, min(ln+n, nl-1)))
	case ctrl('f'), KeyPgDn:
		_, last := e.visible()
		f.top = max(f.top+1, min(last-1, nl-1))
		f.top = min(f.top, nl-1)
		if ln < f.top {
			f.cur = f.firstNonBlank(f.top)
		}
	case ctrl('b'), KeyPgUp:
		f.top = max(0, f.top-max(1, rows-2))
		if _, last := e.visible(); ln > last {
			f.cur = f.firstNonBlank(last)
		}
	case ctrl('e'):
		f.top = min(f.top+c.n(), nl-1)
		if ln < f.top {
			f.cur = e.atCol(f.top, f.want)
			e.keepWant = true
		}
	case ctrl('y'):
		f.top = max(0, f.top-c.n())
		if _, last := e.visible(); ln > last {
			f.cur = e.atCol(last, f.want)
			e.keepWant = true
		}
	}
}

// altCmd is ^^: the alternate file, the one edited before.
func altCmd(e *Editor, c *ctx) {
	if e.alt == nil {
		e.msg = "No alternate file"
		return
	}
	e.switchTo(e.alt)
}

func suspendCmd(e *Editor, c *ctx) {
	if e.t.fd < 0 {
		return
	}
	e.t.suspend()
	syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)
	e.t.resume()
	e.t.enter()
	e.screen, e.shape = nil, ""
}

// info is ^G: the file name, whether it is modified, and where we are.
func (e *Editor) info() {
	f := e.buffer()
	mod := ""
	if f.dirty() {
		mod = " [Modified]"
	}
	if f.scratch {
		mod += " [scratch]"
	}
	ln, nl := f.line(f.cur)+1, f.t.Nlines()
	e.msg = fmt.Sprintf("%q%s line %d of %d --%d%%--", f.title(), mod, ln, nl, ln*100/nl)
}

// escCmd: ESC in normal mode cancels; in the tag it goes back to the
// buffer.
func escCmd(e *Editor, c *ctx) { e.leaveTag() }
