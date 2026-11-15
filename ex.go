package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// The command line (:). A command is
//
//	[address] command [!] [argument]
//
// The address sets the range a command works on. Without one, the range
// is dot if dot is not empty, and otherwise the current line. Addresses:
//
//	N      line N             $      the last line
//	.      dot, or the current line if dot is empty
//	'x     the line of mark x  #N     the empty range at rune offset N
//	/re/   the next match      ?re?   the previous match
//	a+N a-N  lines relative to a      a,b  from a to b
//	,      the whole file, as in sam: a, is a to the end, ,b the top to b
//
// An address alone moves there; a pattern address sets dot to the match.
//
// As in acme, a command line starting with |, < or > is a pipe on the
// range (dot by default):
//
//	|cmd   replace the range with the output of cmd run on it
//	<cmd   replace the range with the output of cmd
//	>cmd   send the range to cmd and show the output in +Errors
//
// and !cmd runs cmd on the terminal, as in vi.
type exArgs struct {
	r    Range
	line bool // r is whole lines
	addr bool // an address was given
	bang bool
	name string
	arg  string
}

type excmd struct {
	name  string // the command, and the word in the tag: write, Write
	short string // the short form, if there is one: w
	fn    func(*Editor, *exArgs) error
	help  string // what it does, for the key help (?)
}

// exTab holds every named command. Each is typed with its name or its
// short form (:write or :w), and its name with a capital letter is the
// word that executing runs (Write, in the tag).
var exTab []excmd

func init() {
	exTab = []excmd{
		{"write", "w", exWrite, "write the file; :w name, :w! to overwrite"},
		{"wq", "", exWq, "write and close"},
		{"del", "q", exDel, "close the buffer (d); modified: twice, or :q!"},
		{"exit", "qa", exExit, "leave tie (Q); :qa! dropping all changes"},
		{"get", "", exGet, "read the file again from disk"},
		{"diff", "", exDiff, "the differences between the buffer and its file"},
		{"new", "", exNew, "a buffer for a new file: :new name"},
		{"look", "l", exLook, "look at text: open a file, plumb, or find it"},
		{"select", "s", exSelect, "select the address, or the word"},
		{"execute", "x", exExecute, "execute text, dot or the word"},
		{"edit", "e", exSam, "sam's commands: :e ,s/re/new/g  :e ,x/re/cmd"},
		{"cut", "", exCut, "cut dot or the addressed lines"},
		{"yank", "", exYank, "yank dot or the addressed lines"},
		{"paste", "", exPaste, "paste over dot"},
		{"undo", "", exUndo, "undo"},
		{"redo", "", exRedo, "redo"},
		{"set", "", exSet, "options: ai et ic list mouse nu sw= ts="},
		{"help", "", exHelp, "the introduction"},
		{"tutor", "", exTutor, "the tutor"},
	}
}

// excommand finds the command called name, by its name or short form.
func excommand(name string) *excmd {
	for i := range exTab {
		if c := &exTab[i]; name == c.name || name != "" && name == c.short {
			return c
		}
	}
	return nil
}

// exline reads a command line and executes it.
func (e *Editor) exline() {
	s, ok := e.prompt(":", "")
	if !ok {
		e.keepDot = true // a cancelled command leaves dot alone
		return
	}
	e.msgErr(e.ex(s))
}

// ex executes one command line. It works on the buffer, also when
// typed in the tag.
func (e *Editor) ex(s string) error {
	e.leaveTag()
	f := e.f
	s = strings.TrimLeft(s, " \t:")
	r, line, has, rest, err := e.parseRange(s)
	if err != nil {
		return err
	}
	x := &exArgs{r: r, line: line, addr: has}
	if !has {
		if !f.dot.Empty() {
			x.r, x.line = f.dot, false
		} else {
			x.r, x.line = f.lineRange(f.line(f.cur)), true
		}
	}
	rest = strings.TrimLeft(rest, " \t")
	if rest == "" {
		if has {
			e.exGoto(x)
		}
		return nil
	}
	switch rest[0] {
	case '%':
		return errors.New("The whole file is , (as in sam): :e ,s/a/b/g")
	case '|', '<', '>':
		return e.pipe(rest[0], strings.TrimSpace(rest[1:]), x)
	case '!':
		if has {
			return errors.New("To filter, use | (as in :,|sort)")
		}
		e.shell(e.expand(strings.TrimSpace(rest[1:])), nil)
		return nil
	}
	i := 0
	for i < len(rest) && (rest[i] >= 'a' && rest[i] <= 'z' || rest[i] >= 'A' && rest[i] <= 'Z') {
		i++
	}
	x.name, rest = rest[:i], rest[i:]
	if strings.HasPrefix(rest, "!") {
		x.bang, rest = true, rest[1:]
	}
	x.arg = strings.TrimSpace(rest)
	if c := excommand(x.name); c != nil {
		return c.fn(e, x)
	}
	return fmt.Errorf("Not an editor command: %s", strings.TrimSpace(x.name+rest))
}

func (e *Editor) exGoto(x *exArgs) {
	f := e.f
	f.marks['\''] = f.cur
	if x.line {
		f.cur = f.firstNonBlank(f.lastLine(x.r))
		return
	}
	f.cur, f.dot = x.r.Q0, x.r
	e.keepDot = true
}

func (e *Editor) parseRange(s string) (Range, bool, bool, string, error) {
	n := e.f.t.Len()
	a, aline, ok, s, err := e.parseAddr(s)
	if err != nil {
		return a, aline, false, s, err
	}
	if !ok {
		if !strings.HasPrefix(s, ",") {
			return a, aline, false, s, nil
		}
		a, aline = Range{0, 0}, true // ",b" starts at the top, as in sam
	}
	if !strings.HasPrefix(s, ",") {
		return a, aline, true, s, nil
	}
	b, bline, ok, s, err := e.parseAddr(s[1:])
	if err != nil {
		return b, bline, false, s, err
	}
	if !ok {
		b, bline = Range{n, n}, aline // "a," runs to the end
	}
	if b.Q1 < a.Q0 {
		return a, aline, false, s, errors.New("Backwards range")
	}
	return Range{a.Q0, max(a.Q1, b.Q1)}, aline && bline, true, s, nil
}

func leadingInt(s string) (int, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	n, _ := strconv.Atoi(s[:i])
	return n, s[i:]
}

func (e *Editor) parseAddr(s string) (r Range, line, ok bool, rest string, err error) {
	f := e.f
	nl := f.t.Nlines()
	line = true
	if s == "" {
		return r, line, false, s, nil
	}
	switch c := s[0]; {
	case c >= '0' && c <= '9':
		var n int
		n, s = leadingInt(s)
		if n == 0 {
			r = Range{0, 0}
		} else {
			r = f.lineRange(min(n, nl) - 1)
		}
	case c == '.':
		if !f.dot.Empty() {
			r, line = f.dot, false
		} else {
			r = f.lineRange(f.line(f.cur))
		}
		s = s[1:]
	case c == '$':
		r, s = f.lineRange(nl-1), s[1:]
	case c == '\'':
		if len(s) < 2 {
			return r, line, false, s, errors.New("Missing mark name")
		}
		q, found := f.marks[rune(s[1])]
		if !found {
			return r, line, false, s, errors.New("Mark not set")
		}
		r, s = f.lineRange(f.line(min(q, f.t.Len()))), s[2:]
	case c == '#':
		var n int
		n, s = leadingInt(s[1:])
		n = min(n, f.t.Len())
		r, line = Range{n, n}, false
	case c == '/' || c == '?':
		var pat string
		pat, s = splitDelim(s[1:], c)
		if pat == "" {
			pat = e.pat
		}
		e.pat, e.back = pat, c == '?'
		re, err := e.compile(pat)
		if err != nil {
			return r, line, false, s, err
		}
		m, _, found := f.search(re, f.cur, c == '?')
		if !found {
			return r, line, false, s, fmt.Errorf("Pattern not found: %s", pat)
		}
		r, line = m, false
	case c == '+' || c == '-':
		r = f.lineRange(f.line(f.cur))
	default:
		return r, line, false, s, nil
	}
	for len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		sign := 1
		if s[0] == '-' {
			sign = -1
		}
		n, t := leadingInt(s[1:])
		if len(t) == len(s)-1 {
			n = 1
		}
		s = t
		base := f.line(r.Q0)
		if sign > 0 {
			base = f.lastLine(r)
		}
		r, line = f.lineRange(max(0, min(base+sign*n, nl-1))), true
	}
	return r, line, true, s, nil
}

// Pipes and the shell.

func (e *Editor) pipe(op byte, cmd string, x *exArgs) error {
	f := e.f
	if cmd == "" {
		return errors.New("No command")
	}
	r := f.dot
	if x.addr {
		r = x.r
	}
	if op == '>' {
		e.runCaptured(cmd, f.t.Runes(r.Q0, r.Q1))
		return nil
	}
	nr, err := e.filter(r, cmd, op == '|')
	if err != nil {
		return err
	}
	f.dot = nr
	e.keepDot = true
	return nil
}

// filter replaces r with the output of cmd, which reads r if input is
// set. It returns the range of the new text.
func (e *Editor) filter(r Range, cmd string, input bool) (Range, error) {
	f := e.f
	var in []rune
	if input {
		in = f.t.Runes(r.Q0, r.Q1)
	}
	out, errs, err := runCmd(cmd, in)
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || len(out) == 0 {
			if errs != "" {
				return r, errors.New(strings.TrimSpace(errs))
			}
			return r, err
		}
	}
	// A command's output ends in a newline. If the text it replaces
	// did not, drop it: <date in the middle of a line keeps the line.
	if n := len(out); n > 0 && out[n-1] == '\n' && (r.Empty() || f.t.At(r.Q1-1) != '\n') {
		out = out[:n-1]
	}
	f.replace(r.Q0, r.Q1, out)
	f.cur = r.Q0
	if errs != "" {
		e.msg = strings.TrimSpace(errs)
	}
	return Range{r.Q0, r.Q0 + len(out)}, nil
}

func runCmd(cmd string, in []rune) ([]rune, string, error) {
	c := sh(cmd, "")
	c.Stdin = bytes.NewReader(encode(in))
	var out, errs bytes.Buffer
	c.Stdout, c.Stderr = &out, &errs
	err := c.Run()
	return decode(out.Bytes()), errs.String(), err
}

// shell runs cmd on the terminal, like vi's :!. If input is not nil it
// is the command's standard input.
func (e *Editor) shell(cmd string, input []rune) {
	if e.t.fd < 0 {
		runCmd(cmd, input)
		return
	}
	e.t.suspend()
	os.Stdout.WriteString("\n")
	c := sh(cmd, "")
	c.Stdin = os.Stdin
	if input != nil {
		c.Stdin = bytes.NewReader(encode(input))
	}
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	// The child gets ^C; tie must not die of it.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGQUIT)
	err := c.Run()
	signal.Stop(sig)
	e.t.resume()
	if err != nil {
		e.t.write("\r\n[" + err.Error() + "]")
	}
	e.t.write("\r\n[Hit return to continue]")
	e.in.Get()
	e.t.enter()
	e.screen, e.shape = nil, ""
}

// Commands.

func exWrite(e *Editor, x *exArgs) error {
	f := e.f
	name := x.arg
	if name == "" {
		name = f.name
	}
	if name == "" {
		// Ask for a name rather than refuse.
		s, ok := e.prompt("File name: ", "")
		if !ok || s == "" {
			return errors.New("No file name")
		}
		name = s
	}
	if f.scratch && name == f.name && !x.bang {
		return errors.New("Scratch buffer: write it with :w name")
	}
	if f.stale && name == f.name && !x.bang {
		return errors.New("The file changed on disk: Diff compares, :w! overwrites it")
	}
	if x.arg != "" && x.arg != f.name && !x.bang {
		if _, err := os.Stat(x.arg); err == nil {
			return fmt.Errorf("%q exists (add ! to override)", x.arg)
		}
	}
	if f.name == "" {
		f.name = name
	}
	r := Range{0, f.t.Len()}
	if x.addr {
		r = x.r
	}
	n, err := writeFile(name, f.t.Runes(r.Q0, r.Q1))
	if err != nil {
		return err
	}
	if name == f.name {
		f.noteDisk()
	}
	if name == f.name && !x.addr {
		f.commit()
		f.saved = f.state()
	}
	e.msg = fmt.Sprintf("%q %dL, %dB written", name, strings.Count(f.t.String(r.Q0, r.Q1), "\n"), n)
	return nil
}

// exDel closes the current buffer, as acme's Del does; closing the
// last one leaves the editor. A buffer with unwritten changes is closed
// only by :del! (:q!), or, as in acme, by a second Del right after the
// first, which warned.
func exDel(e *Editor, x *exArgs) error {
	f := e.f
	force := x.bang || e.warned.f == f && e.warned.n == e.ncmd-1
	if f.dirty() && !force {
		e.warned.f, e.warned.n = f, e.ncmd
		return fmt.Errorf("%q has unwritten changes: Del (d) again closes it, :w writes it", f.title())
	}
	if len(e.files) == 1 {
		return exExit(e, &exArgs{bang: force})
	}
	i := slices.Index(e.files, f)
	e.files = slices.Delete(e.files, i, i+1)
	next := e.alt
	if next == nil || next == f {
		next = e.files[min(i, len(e.files)-1)]
	}
	e.f, e.alt = next, nil
	return nil
}

// exExit leaves the editor, as Q does.
func exExit(e *Editor, x *exArgs) error {
	if !x.bang {
		for _, f := range e.files {
			if f.dirty() {
				return fmt.Errorf("No write since last change to %q (add ! to override)", f.title())
			}
		}
	}
	e.quit = true
	return nil
}

func exWq(e *Editor, x *exArgs) error {
	if err := exWrite(e, x); err != nil {
		return err
	}
	return exDel(e, &exArgs{bang: x.bang})
}

// exCut and exYank work on dot or the addressed lines, never on
// nothing in particular, as the builtins Cut and Yank do.
func exCut(e *Editor, x *exArgs) error {
	if !x.addr && e.f.dot.Empty() {
		return errors.New("Cut: no selection")
	}
	opCut(e, x.r, x.line, &ctx{reg: regName(x.arg)})
	return nil
}

func exYank(e *Editor, x *exArgs) error {
	if !x.addr && e.f.dot.Empty() {
		return errors.New("Yank: no selection")
	}
	opYank(e, x.r, x.line, &ctx{reg: regName(x.arg)})
	if !x.addr {
		e.keepDot = true
	}
	return nil
}

func exPaste(e *Editor, x *exArgs) error { return e.pasteOver(regName(x.arg)) }
func exUndo(e *Editor, x *exArgs) error  { undoCmd(e, &ctx{}); return nil }
func exRedo(e *Editor, x *exArgs) error  { redoCmd(e, &ctx{}); return nil }

func regName(s string) rune {
	if s == "" {
		return 0
	}
	return rune(s[0])
}

// Files.

// exNew opens a buffer for a new file, named relative to the buffer's
// directory; it is created on disk by :w. An existing file is opened.
// Without a name the buffer has none, until :w name gives it one.
func exNew(e *Editor, x *exArgs) error {
	if x.arg == "" {
		f := newFile("")
		e.files = append(e.files, f)
		e.switchTo(f)
		return nil
	}
	name, _, _ := e.resolve(x.arg)
	e.open(name)
	return nil
}

// exGet reloads the buffer from disk, dropping its changes: acme's Get.
func exGet(e *Editor, x *exArgs) error {
	f := e.buffer()
	if f.name == "" || f.scratch && !f.isDir {
		return errors.New("Get: no file")
	}
	if err := e.reload(f); err != nil {
		return err
	}
	e.msg = fmt.Sprintf("%q read from disk", f.name)
	return nil
}

func exHelp(e *Editor, x *exArgs) error {
	e.openText("[intro]", introText)
	return nil
}

func exTutor(e *Editor, x *exArgs) error { return e.openTutor() }

func (e *Editor) openTutor() error {
	tf, err := os.CreateTemp("", "tutor-*.txt")
	if err != nil {
		return err
	}
	_, err = tf.WriteString(tutorText)
	tf.Close()
	if err != nil {
		return err
	}
	e.open(tf.Name())
	e.msg = "Welcome to the tie tutor"
	return nil
}

// Options.

type option struct {
	name, abbr string
	b          *bool
	i          *int
}

func (e *Editor) options() []option {
	o := &e.opt
	return []option{
		{"autoindent", "ai", &o.ai, nil},
		{"expandtab", "et", &o.et, nil},
		{"ignorecase", "ic", &o.ic, nil},
		{"list", "list", &o.list, nil},
		{"mouse", "mouse", &o.mouse, nil},
		{"number", "nu", &o.nu, nil},
		{"shiftwidth", "sw", nil, &o.sw},
		{"tabstop", "ts", nil, &o.ts},
	}
}

func (o option) String() string {
	if o.i != nil {
		return fmt.Sprintf("%s=%d", o.name, *o.i)
	}
	if *o.b {
		return o.name
	}
	return "no" + o.name
}

func exSet(e *Editor, x *exArgs) error {
	opts := e.options()
	find := func(name string) *option {
		for i := range opts {
			if opts[i].name == name || opts[i].abbr == name {
				return &opts[i]
			}
		}
		return nil
	}
	if x.arg == "" || x.arg == "all" {
		var s []string
		for _, o := range opts {
			s = append(s, o.String())
		}
		sort.Strings(s)
		e.msg = strings.Join(s, "  ")
		return nil
	}
	var shown []string
	for _, a := range strings.Fields(x.arg) {
		name, val, hasVal := strings.Cut(a, "=")
		query := strings.HasSuffix(name, "?")
		name = strings.TrimSuffix(name, "?")
		o := find(name)
		neg := false
		if o == nil && strings.HasPrefix(name, "no") {
			o, neg = find(name[2:]), true
		}
		switch {
		case o == nil:
			return fmt.Errorf("Unknown option: %s", name)
		case query || o.i != nil && !hasVal:
			shown = append(shown, o.String())
		case o.b != nil:
			*o.b = !neg
		default:
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 || n > 64 {
				return fmt.Errorf("Bad value: %s", a)
			}
			*o.i = n
		}
	}
	e.msg = strings.Join(shown, "  ")
	e.screen = nil
	if e.opt.mouse != e.t.mouse {
		e.t.setMouse(e.opt.mouse)
	}
	return nil
}
