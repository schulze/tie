package main

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"unicode"
)

// The three acme buttons, on the keyboard (keySelect, keyExecute and
// keyLook in config.go), on the command line (:s, :x, :l) and on the
// mouse (mouse.go):
//
//	select   make dot: the word under the cursor; again, the line,
//	         the paragraph, the file. Then motions extend it, as in v.
//	execute  run dot, or the word (non-blank text) under the cursor:
//	         a builtin word (config.go), a :command, a pipe |<> on
//	         dot, or else a shell command whose output goes to +Errors.
//	look     open dot, or the name under the cursor: a lookRule
//	         (config.go), a file or directory (file:addr), or else
//	         search for the next occurrence of the text.

// unitAt is what selecting at q takes. Like acme's double click: on a
// bracket or quote, the text up to its partner; at either end of a line
// (if lines is set), the whole line; else the word, or the character.
func (e *Editor) unitAt(q int, lines bool) Range {
	f := e.f
	r := f.t.At(q)
	if r == eof {
		return Range{q, q}
	}
	if lines && (q == f.bol(q) || r == '\n') {
		return f.lineRange(f.line(q))
	}
	if strings.ContainsRune(brackets, r) {
		if p, ok := f.matchBracket(q); ok {
			return Range{min(p, q) + 1, max(p, q)}
		}
	}
	if strings.ContainsRune("\"'`", r) {
		for p := q + 1; p < f.eol(q); p++ {
			if f.t.At(p) == r {
				return Range{q + 1, p}
			}
		}
	}
	c := class(r, false)
	if c == 0 {
		return f.lineRange(f.line(q))
	}
	a, b := q, q+1
	if c == 1 {
		for a > 0 && class(f.t.At(a-1), false) == 1 {
			a--
		}
		for class(f.t.At(b), false) == 1 {
			b++
		}
	}
	return Range{a, b}
}

// grow makes d bigger by one step: lines, then the paragraph, then
// the whole file.
func (e *Editor) grow(d Range) Range {
	f := e.f
	if l := f.lines(d); l != d {
		return l
	}
	l0, l1 := f.line(d.Q0), f.lastLine(d)
	for l0 > 0 && !f.emptyLine(l0-1) {
		l0--
	}
	for l1 < f.t.Nlines()-1 && !f.emptyLine(l1+1) {
		l1++
	}
	if p := (Range{f.t.Bol(l0), f.lineEnd(l1)}); p != d {
		return p
	}
	return Range{0, f.t.Len()}
}

// wordAt is the text execute takes when nothing is selected: the
// non-blank text around q.
func (f *File) wordAt(q int) Range {
	if isBlank(f.t.At(q)) {
		return Range{q, q}
	}
	a, b := q, q
	for a > 0 && !isBlank(f.t.At(a-1)) {
		a--
	}
	for b < f.t.Len() && !isBlank(f.t.At(b)) {
		b++
	}
	return Range{a, b}
}

func (e *Editor) dotText() string { return e.f.t.String(e.f.dot.Q0, e.f.dot.Q1) }

// selectCmd is button 1: it selects the word at the cursor (or keeps
// dot, if there is one, as after a search) and goes on in visual mode,
// where motions extend dot and the key again grows it.
func selectCmd(e *Editor, c *ctx) {
	f := e.f
	if f.dot.Empty() {
		f.dot = e.unitAt(f.cur, false)
	}
	f.cur = f.dot.Q0
	vc := &ctx{key: 'v', count: c.count, reg: c.reg}
	visualCmd(e, vc)
	c.changed = vc.changed
}

// executeCmd is button 2.
func executeCmd(e *Editor, c *ctx) {
	f := e.f
	text := e.dotText()
	if f.dot.Empty() {
		w := f.wordAt(f.cur)
		text = f.t.String(w.Q0, w.Q1)
	}
	e.keepDot = true
	e.msgErr(e.execute(text))
}

// lookCmd is button 3.
func lookCmd(e *Editor, c *ctx) {
	f := e.f
	if f.dot.Empty() {
		e.msgErr(e.lookAt(f, f.cur))
	} else {
		e.msgErr(e.look(e.dotText(), f.dot.Q0))
	}
}

// lookAt looks at the text around q when nothing is selected. The look
// rules see the whole non-blank word (a URL has characters no file name
// has), stripped of brackets and quotes around it; if no rule takes it,
// it goes on with the file name at q.
//
// The text may be in the tag (f is then not the buffer); the search
// then starts at the cursor in the buffer.
func (e *Editor) lookAt(f *File, q int) error {
	w := f.wordAt(q)
	word := trimWord(f.t.String(w.Q0, w.Q1))
	if done, err := e.lookRule(word); done {
		return err
	}
	text := f.nameAround(Range{q, q})
	if f != e.buffer() {
		q = e.buffer().cur
	}
	return e.look(text, q)
}

// execute runs text as acme does with button 2, on the buffer, also
// when the text is in the tag.
func (e *Editor) execute(text string) error {
	e.leaveTag()
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return nil
	case text[0] == ':':
		return e.ex(text[1:])
	case strings.ContainsRune("|<>", rune(text[0])):
		return e.ex(text) // a pipe on dot
	}
	// A capitalized word that names a command is that command: Write,
	// Get, Undo, Edit ,s/a/b/g, Look name. Del! is :del!.
	word, arg, _ := strings.Cut(text, " ")
	if name := strings.TrimSuffix(word, "!"); name != "" && unicode.IsUpper(rune(name[0])) {
		if c := excommand(strings.ToLower(name)); c != nil && c.name == strings.ToLower(name) {
			return e.ex(strings.ToLower(word) + " " + strings.TrimSpace(arg))
		}
	}
	e.runCaptured(text, nil)
	return nil
}

type lookRule struct {
	pattern string
	cmd     string
	fn      func(e *Editor, m []string) error
	re      *regexp.Regexp
}

// look acts on text as acme's button 3 and the plumber do. from is
// where the text is, so a search starts after it.
// Text in the tag is looked at in the buffer, from its cursor.
func (e *Editor) look(text string, from int) error {
	if e.leaveTag() {
		from = e.f.cur
	}
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return errors.New("Nothing to look at")
	}
	if done, err := e.lookRule(text); done {
		return err
	}
	if name, _, ok := e.resolve(text); ok {
		if _, err := os.Stat(name); err == nil {
			return e.plumb(text)
		}
	}
	f := e.f
	e.pat = regexp.QuoteMeta(text)
	e.back = false
	re, _ := e.compile(e.pat)
	m, wrapped, ok := f.search(re, from, false)
	if !ok {
		return errors.New("Not found: " + text)
	}
	if wrapped {
		e.msg = "search wrapped"
	}
	f.marks['\''] = f.cur
	f.cur, f.dot = m.Q0, m
	e.keepDot = true
	return nil
}

// trimWord strips what surrounds a word in prose: quotes, punctuation
// at the end of a sentence, and brackets that are not part of it, so
// "(see https://x.org/a_(b))." gives https://x.org/a_(b) and ls(1).
// gives ls(1).
func trimWord(s string) string {
	pairs := map[byte]byte{')': '(', ']': '[', '>': '<', '}': '{'}
	for len(s) > 0 && strings.ContainsRune("\"'`([<{", rune(s[0])) {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if o, ok := pairs[c]; ok && strings.Count(s, string(o)) >= strings.Count(s, string(c)) {
			break
		}
		if !strings.ContainsRune(".,;:!?\"'`)]>}", rune(c)) {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

// lookRule applies the first look rule that matches text.
func (e *Editor) lookRule(text string) (bool, error) {
	for i := range lookRules {
		r := &lookRules[i]
		if r.re == nil {
			r.re = regexp.MustCompile(r.pattern)
		}
		m := r.re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		if r.fn != nil {
			return true, r.fn(e, m)
		}
		c := sh(r.cmd, e.base(), m...)
		if err := c.Start(); err != nil {
			return true, err
		}
		go c.Wait()
		e.msg = r.cmd
		return true, nil
	}
	return false, nil
}

// The command-line forms of the buttons: :s select, :x execute, :l look.

// exSelect makes dot: the address given, or the word at the cursor.
func exSelect(e *Editor, x *exArgs) error {
	f := e.f
	if x.addr {
		f.dot = x.r
	} else if f.dot.Empty() {
		f.dot = e.unitAt(f.cur, false)
	}
	f.cur = f.dot.Q0
	e.keepDot = true
	return nil
}

// exExecute executes its argument, or dot, or the word at the cursor.
func exExecute(e *Editor, x *exArgs) error {
	f := e.f
	if x.arg != "" {
		e.keepDot = true
		return e.execute(x.arg)
	}
	if x.addr {
		return e.execute(f.t.String(x.r.Q0, x.r.Q1))
	}
	executeCmd(e, &ctx{})
	return nil
}

// exLook looks at its argument, or dot, or the name at the cursor.
func exLook(e *Editor, x *exArgs) error {
	if x.arg != "" {
		return e.look(x.arg, e.f.cur)
	}
	if x.addr {
		return e.look(e.f.t.String(x.r.Q0, x.r.Q1), x.r.Q0)
	}
	lookCmd(e, &ctx{})
	return nil
}

// pasteOver replaces dot with register reg, which then is dot.
func (e *Editor) pasteOver(reg rune) error {
	f := e.f
	r := e.getReg(reg)
	if r == nil {
		return errors.New("Nothing in register")
	}
	d := f.dot
	f.replace(d.Q0, d.Q1, r.text)
	f.dot = Range{d.Q0, d.Q0 + len(r.text)}
	f.cur = d.Q0
	e.keepDot = true
	return nil
}
