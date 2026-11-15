package main

import (
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

type Mode int

const (
	Normal Mode = iota
	Insert
	Visual
	VisualLine
)

var modeNames = [...]string{"", "-- INSERT --", "-- VISUAL --", "-- VISUAL LINE --"}

// Range is a span [Q0, Q1) of rune offsets. dot, the current
// selection, is a Range.
type Range struct{ Q0, Q1 int }

func (r Range) Empty() bool { return r.Q1 <= r.Q0 }

// last is the position of the last rune in r, or r.Q0 if r is empty.
func (r Range) last() int { return max(r.Q0, r.Q1-1) }

// Reg is a register (vi's named buffers). line marks whole-line text.
type Reg struct {
	text []rune
	line bool
}

// ctx carries the arguments of the command being executed.
type ctx struct {
	count   int   // 0 if none was typed
	reg     rune  // register named with "x, or 0
	key     Key   // the command (or motion) key
	op      Key   // pending operator, when running a motion for one
	kind    mkind // motion kind; a motion may change it
	changed bool  // the command changed the text: period repeats it
}

func (c *ctx) n() int { return max(1, c.count) }

// lastChange is what period repeats.
type lastChange struct {
	keys  []Key
	count int
	reg   rune
}

type Options struct {
	ts, sw               int
	ai, et, ic, list, nu bool
	mouse                bool
}

type Editor struct {
	t      *Term
	in     *Input
	files  []*File
	f, alt *File
	mode   Mode
	opt    Options
	regs   map[rune]*Reg
	last   *lastChange
	lastAt rune

	pat   string // last search pattern
	back  bool   // last search was backwards
	match *Range // set by a search motion
	find  struct {
		key Key
		ch  rune
	}

	msg       string
	keepDot   bool // the command set dot itself
	keepWant  bool // the command set the wanted column itself
	prompting bool
	cmdline   string
	hist      map[string][]string

	screen   []string // what is on the terminal, row by row
	hit      []hitRow // the text positions shown there, for the mouse
	tagHit   hitRow   // and those of the tag on the bottom line
	tabs     []tab    // where the buffer names are on the buffer line
	choosing *int     // the buffer chosen on the buffer line (buffersCmd)
	click    struct { // the last button 1 click, to see double clicks
		at time.Time
		q  int
	}
	shape string
	quit  bool

	ncmd   int      // commands so far, to tell what came right before
	warned struct { // the last Del that only warned
		f *File
		n int
	}
}

func newEditor(t *Term, in *Input) *Editor {
	e := &Editor{
		t:    t,
		in:   in,
		regs: map[rune]*Reg{},
		hist: map[string][]string{},
		opt:  defaultOptions,
	}
	if e.opt.ts == 0 {
		e.opt.ts = t.tab
	}
	t.setMouse(e.opt.mouse)
	in.onResize = func() {
		e.t.size(nil)
		e.screen = nil
		e.draw()
	}
	in.onIdle = func() { // see files that change while we wait
		msg := e.msg
		e.checkDisk()
		if e.msg != msg {
			e.draw()
		}
	}
	return e
}

// run is the main loop: draw, read a command, execute it.
func (e *Editor) run() {
	for !e.quit {
		for _, f := range e.files {
			f.commit()
		}
		e.checkDisk()
		e.clamp()
		e.draw()
		if !e.command() {
			return
		}
	}
}

// msgErr shows err, if any, on the status line.
func (e *Editor) msgErr(err error) {
	if err != nil {
		e.msg = err.Error()
	}
}

// readPrefix reads a count and a register name ("x) before a command.
func (e *Editor) readPrefix(c *ctx) Key {
	for {
		k := e.in.Get()
		switch {
		case k >= '1' && k <= '9' || k == '0' && c.count > 0:
			c.count = c.count*10 + int(k-'0')
		case k == '"':
			c.reg = rune(e.in.Get())
		default:
			return k
		}
	}
}

// command reads and executes one normal mode command.
func (e *Editor) command() bool {
	c := &ctx{}
	k := e.readPrefix(c)
	if k == KeyEOF {
		return false
	}
	e.ncmd++
	e.msg = ""
	c.key = k
	e.keepDot, e.keepWant = false, false
	e.in.record(k)
	if cm, ok := normal[k]; ok {
		cm.fn(e, c)
		if cm.change {
			c.changed = true
		}
	} else if m, ok := motions[k]; ok {
		e.move(c, m)
	}
	keys := e.in.stop()
	if c.changed {
		e.last = &lastChange{keys: keys, count: c.count, reg: c.reg}
	}
	e.clamp()
	f := e.f
	if !e.keepDot {
		f.dot = Range{f.cur, f.cur}
	}
	if !e.keepWant {
		f.want = e.col(f.cur)
	}
	return true
}

// period repeats the last change: vi's "." command. It is not called
// dot, because dot, as in acme, is the name of the selection.
//
// It replays the keys of the change, so anything that can be typed can
// be repeated: an insertion, an operator with any motion, or a visual
// mode edit such as "v" followed by c, which then applies to the new dot.
func period(e *Editor, c *ctx) {
	l := e.last
	if l == nil {
		return
	}
	var keys []Key
	if l.reg != 0 {
		keys = append(keys, '"', Key(l.reg))
	}
	if n := c.count; n > 0 || l.count > 0 {
		if n == 0 {
			n = l.count
		}
		for _, d := range strconv.Itoa(n) {
			keys = append(keys, Key(d))
		}
	}
	e.in.push(append(keys, l.keys...))
	e.keepDot = true // the change may be one on dot
}

// clamp keeps the cursor and dot inside the text; outside insert mode
// the cursor never rests on the newline of a non-empty line.
func (e *Editor) clamp() {
	f := e.f
	n := f.t.Len()
	f.cur = max(0, min(f.cur, n))
	if e.mode != Insert {
		if f.cur == n && n > 0 {
			f.cur--
		}
		if f.cur > 0 && f.t.At(f.cur) == '\n' && f.t.At(f.cur-1) != '\n' {
			f.cur--
		}
	}
	f.dot.Q0 = max(0, min(f.dot.Q0, n))
	f.dot.Q1 = max(f.dot.Q0, min(f.dot.Q1, n))
	f.top = max(0, min(f.top, f.t.Nlines()-1))
}

func (e *Editor) setReg(name rune, text []rune, line bool) {
	text = slices.Clone(text)
	if line && (len(text) == 0 || text[len(text)-1] != '\n') {
		text = append(text, '\n')
	}
	r := &Reg{text, line}
	if name >= 'A' && name <= 'Z' {
		name += 'a' - 'A'
		if old := e.regs[name]; old != nil {
			r = &Reg{append(slices.Clone(old.text), text...), old.line || line}
		}
	}
	if name != 0 && name != '"' {
		e.regs[name] = r
	}
	e.regs['"'] = r
}

func (e *Editor) getReg(name rune) *Reg {
	if name == 0 {
		name = '"'
	}
	if name >= 'A' && name <= 'Z' {
		name += 'a' - 'A'
	}
	return e.regs[name]
}

// Files.

func (e *Editor) switchTo(f *File) {
	e.leaveTag()
	if f != e.f {
		e.alt, e.f = e.f, f
	}
	if f.stale {
		e.staleMsg(f)
	}
}

func (e *Editor) addFile(f *File) {
	if p := e.f; len(e.files) == 1 && p.name == "" && !p.dirty() && p.t.Len() == 0 {
		e.files[0], e.f = f, f
		return
	}
	e.files = append(e.files, f)
	e.switchTo(f)
}

func (e *Editor) open(name string) {
	name = filepath.Clean(name)
	for _, f := range e.files {
		if f.name == name {
			e.switchTo(f)
			return
		}
	}
	f := newFile(name)
	msg, err := f.load()
	if err != nil {
		e.msg = err.Error()
	} else {
		e.msg = msg
	}
	e.addFile(f)
}

func (e *Editor) openText(name, text string) {
	f := newFile(name)
	f.t = Text{r: []rune(text)}
	f.scratch = true
	e.addFile(f)
}
