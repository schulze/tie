package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The screen: the buffer line on top, the text rows, then the bottom
// line: the tag of the buffer on the left and the position of the
// cursor on the right. A message, or the mode, takes the place of the
// tag until the next key, and the command line takes the whole line
// while it is typed. Long lines wrap. Dot is shown in reverse video.

// rows is the number of text rows: the screen less the buffer line
// and the bottom line.
func (e *Editor) rows() int { return max(1, e.t.H-2) }

// The buffer line lists the open buffers by number, the current one in
// reverse video, with + after the modified ones. A click on a name
// goes to that buffer. When the names do not fit, < and > show that
// there are more.
func (e *Editor) bufLine() row {
	e.tabs = e.tabs[:0]
	names := make([]string, len(e.files))
	cur := 0
	for i, f := range e.files {
		mod := ""
		if f.dirty() {
			mod = "+"
		}
		if f.stale {
			mod += "!"
		}
		names[i] = fmt.Sprintf(" %d %s%s ", i+1, e.shortName(f), mod)
		if f == e.buffer() {
			cur = i
		}
	}
	if e.choosing != nil {
		cur = *e.choosing
	}
	w := func(i int) int { return utf8.RuneCountInString(names[i]) }
	room := e.t.W - 3 // keep space for < and >
	a, b, used := cur, cur+1, w(cur)
	for a > 0 && used+w(a-1) <= room {
		a--
		used += w(a)
	}
	for b < len(names) && used+w(b) <= room {
		used += w(b)
		b++
	}
	var s strings.Builder
	x := 1
	if a > 0 {
		s.WriteByte('<')
		x++
	}
	for i := a; i < b; i++ {
		if i == cur {
			s.WriteString(sgrReverse + names[i] + sgrReset)
		} else {
			s.WriteString(names[i])
		}
		e.tabs = append(e.tabs, tab{x, x + w(i), e.files[i]})
		x += w(i)
	}
	if b < len(names) {
		s.WriteByte('>')
	}
	return row{s.String(), false}
}

type tab struct {
	x0, x1 int // columns [x0, x1), 1-based
	f      *File
}

// shortName is a buffer's name on the buffer line: the last element of
// its path, unless another buffer has the same one. +Errors goes with
// the last element of its directory: doc/+Errors.
func (e *Editor) shortName(f *File) string {
	if f.name == "" {
		return f.title()
	}
	base := filepath.Base(f.name)
	if base == "+Errors" {
		dir, err := filepath.Abs(filepath.Dir(f.name))
		if err != nil {
			dir = filepath.Dir(f.name)
		}
		return filepath.Base(dir) + "/+Errors"
	}
	for _, g := range e.files {
		if g != f && filepath.Base(g.name) == base {
			base = f.name
			break
		}
	}
	if f.isDir {
		base += "/"
	}
	return base
}

func (e *Editor) numberWidth() int {
	if e.opt.nu {
		return 8
	}
	return 0
}

func (e *Editor) textWidth() int { return max(1, e.t.W-e.numberWidth()) }

// wide reports runes that take two columns.
func wide(r rune) bool {
	switch {
	case r < 0x1100:
		return false
	case r <= 0x115f, r >= 0x2e80 && r <= 0x303e, r >= 0x3041 && r <= 0x33ff,
		r >= 0x3400 && r <= 0x4dbf, r >= 0x4e00 && r <= 0x9fff, r >= 0xa000 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3, r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60, r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1f64f, r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x20000 && r <= 0x3fffd:
		return true
	}
	return false
}

// width is the number of columns r takes at column col.
func (e *Editor) width(r rune, col int) int {
	switch {
	case r == '\t' && !e.opt.list:
		return e.opt.ts - col%e.opt.ts
	case r < ' ' || r == 0x7f:
		return 2
	case r >= rawBase && r <= rawBase+0xff:
		return 4
	case unicode.Is(unicode.Mn, r):
		return 0
	case wide(r):
		return 2
	}
	return 1
}

// col is the screen column of q in its line, ignoring wrapping.
func (e *Editor) col(q int) int { return e.colIn(e.f, q) }

func (e *Editor) colIn(f *File, q int) int {
	c := 0
	for p := f.bol(q); p < q && p < f.t.Len(); p++ {
		c += e.width(f.t.At(p), c)
	}
	return c
}

// atCol returns the position in line ln at screen column col.
func (e *Editor) atCol(ln, col int) int {
	f := e.f
	q, end, c := f.t.Bol(ln), f.t.Eol(ln), 0
	for q < end {
		w := e.width(f.t.At(q), c)
		if c+w > col {
			break
		}
		c += w
		q++
	}
	return q
}

// height is the number of rows line ln takes.
func (e *Editor) height(f *File, ln int) int {
	w := e.colIn(f, f.t.Eol(ln))
	if e.opt.list {
		w++
	}
	tw := e.textWidth()
	return max(1, (w+tw-1)/tw)
}

type cell struct {
	s string
	q int
}

func (e *Editor) layout(f *File, ln int, cells []cell) []cell {
	q0, q1 := f.t.Bol(ln), f.t.Eol(ln)
	cells = cells[:0]
	for q := q0; q < q1; q++ {
		r := f.t.At(q)
		w := e.width(r, len(cells))
		switch {
		case r == '\t' && !e.opt.list:
			for range w {
				cells = append(cells, cell{" ", q})
			}
		case r < ' ' || r == 0x7f:
			cells = append(cells, cell{"^", q}, cell{string(r ^ 0x40), q})
		case r >= rawBase && r <= rawBase+0xff:
			for _, c := range fmt.Sprintf("\\x%02x", r-rawBase) {
				cells = append(cells, cell{string(c), q})
			}
		case w == 0 && len(cells) > 0:
			cells[len(cells)-1].s += string(r)
		case w == 2:
			cells = append(cells, cell{string(r), q}, cell{"", q})
		default:
			cells = append(cells, cell{string(r), q})
		}
	}
	if e.opt.list {
		cells = append(cells, cell{"$", q1})
	}
	return cells
}

// visible returns the first and the last line that fit on the screen.
func (e *Editor) visible() (int, int) {
	f := e.f
	rows, nl := e.rows(), f.t.Nlines()
	used, ln := 0, f.top
	for ln < nl {
		h := e.height(f, ln)
		if used+h > rows && ln > f.top {
			break
		}
		used += h
		ln++
	}
	return f.top, max(f.top, ln-1)
}

// scroll moves the window on f so that the cursor line is on the
// screen. Big jumps put it in the middle, as vi does.
func (e *Editor) scroll(f *File) {
	rows := e.rows()
	cl := f.line(f.cur)
	if cl < f.top {
		if f.top-cl > rows/2 {
			e.center(f, cl)
		} else {
			f.top = cl
		}
		return
	}
	if cl-f.top > 2*rows {
		e.center(f, cl)
		return
	}
	used := 0
	for ln := f.top; ln <= cl; ln++ {
		used += e.height(f, ln)
	}
	if used-rows > rows/2 {
		e.center(f, cl)
		return
	}
	for used > rows && f.top < cl {
		used -= e.height(f, f.top)
		f.top++
	}
}

func (e *Editor) center(f *File, cl int) {
	avail := (e.rows() - e.height(f, cl)) / 2
	f.top = cl
	for f.top > 0 {
		h := e.height(f, f.top-1)
		if h > avail {
			break
		}
		avail -= h
		f.top--
	}
}

// hitRow maps the columns of a screen row back to positions in f, for
// the mouse. end is the position right of the text.
type hitRow struct {
	f   *File
	q   []int
	end int
}

// posAt is the file and position at the 1-based screen cell x, y: the
// body from row 2, the tag on the bottom line.
func (e *Editor) posAt(x, y int) (*File, int) {
	if len(e.hit) == 0 {
		return e.f, e.f.cur
	}
	h := e.hit[max(0, min(y-2, len(e.hit)-1))]
	if y >= e.t.H {
		if e.tagHit.f == nil { // a message hides the tag
			return e.buffer(), e.buffer().cur
		}
		h = e.tagHit
	}
	col := x - 1
	if h.f == e.buffer() {
		col -= e.numberWidth()
	}
	switch {
	case col >= 0 && col < len(h.q):
		return h.f, h.q[col]
	case col < 0 && len(h.q) > 0:
		return h.f, h.q[0]
	}
	return h.f, h.end
}

type row struct {
	s    string
	full bool // reaches the last column: no erase needed (or wanted)
}

// render makes a screen row of cells, with dot of f in reverse video.
// nlsel adds a reversed blank for a selected newline at the end.
func render(f *File, prefix string, seg []cell, nlsel bool, width int) row {
	var b strings.Builder
	b.WriteString(prefix)
	sel := false
	for _, c := range seg {
		if in := f.dot.Q0 <= c.q && c.q < f.dot.Q1; in != sel {
			if in {
				b.WriteString(sgrReverse)
			} else {
				b.WriteString(sgrReset)
			}
			sel = in
		}
		b.WriteString(c.s)
	}
	w := utf8.RuneCountInString(prefix) + len(seg)
	if nlsel && w < width {
		if !sel {
			b.WriteString(sgrReverse)
		}
		b.WriteByte(' ')
		sel = true
		w++
	}
	if sel {
		b.WriteString(sgrReset)
	}
	return row{b.String(), w >= width}
}

// cursorCell is the index of the cell the cursor is on, in a line.
func cursorCell(cells []cell, cur int) int {
	for i, c := range cells {
		if c.q >= cur {
			return i
		}
	}
	return len(cells)
}

func (e *Editor) draw() {
	body := e.buffer()
	tag := body.tagFile()
	e.scroll(body)
	nrows, tw, nw := e.rows(), e.textWidth(), e.numberWidth()
	W := e.t.W
	e.hit = e.hit[:0]
	cy, cx := 0, nw
	rows := []row{e.bufLine()}
	var cells []cell

	nl := body.t.Nlines()
	cl := body.line(body.cur)
	for ln := body.top; ln < nl && len(rows)-1 < nrows; ln++ {
		cells = e.layout(body, ln, cells)
		if ln == cl && e.f == body {
			idx := cursorCell(cells, body.cur)
			y, x := idx/tw, idx%tw
			if idx == len(cells) && idx > 0 && x == 0 {
				y, x = y-1, tw-1
			}
			cy, cx = min(len(rows)+y, nrows), nw+x
		}
		q1 := body.t.Eol(ln)
		nlsel := q1 < body.t.Len() && body.dot.Q0 <= q1 && q1 < body.dot.Q1
		h := max(1, (len(cells)+tw-1)/tw)
		for i := 0; i < h && len(rows)-1 < nrows; i++ {
			seg := cells[min(i*tw, len(cells)):min((i+1)*tw, len(cells))]
			prefix := ""
			if nw > 0 {
				prefix = strings.Repeat(" ", nw)
				if i == 0 {
					prefix = fmt.Sprintf("%*d ", nw-1, ln+1)
				}
			}
			rows = append(rows, render(body, prefix, seg, i == h-1 && nlsel, W))
			hr := hitRow{body, cellPos(seg), q1}
			if i < h-1 && len(seg) > 0 {
				hr.end = seg[len(seg)-1].q
			}
			e.hit = append(e.hit, hr)
		}
	}
	for len(rows)-1 < nrows {
		rows = append(rows, row{"~", false})
		e.hit = append(e.hit, hitRow{body, nil, body.t.Len()})
	}
	if e.choosing != nil {
		for _, t := range e.tabs {
			if t.f == e.files[*e.choosing] {
				cy, cx = 0, t.x0
			}
		}
	}
	bottom, x := e.bottomLine(body, tag)
	rows = append(rows, bottom)
	if x >= 0 {
		cy, cx = len(rows)-1, x
	}
	e.flush(rows, cy, cx)
}

func cellPos(cells []cell) []int {
	q := make([]int, len(cells))
	for i, c := range cells {
		q[i] = c.q
	}
	return q
}

func truncate(s string, w int) string {
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	return string([]rune(s)[:max(0, w)])
}

// bottomLine is the last row: the command line while it is typed; or
// else on the left a message, the mode, or the tag (always the tag
// while the cursor is in it), and on the right the position in the
// buffer and, if dot is not empty, its offsets as #q0,#q1. It returns
// the column for the cursor, or -1 if it is not on this line.
func (e *Editor) bottomLine(body, tag *File) (row, int) {
	w := e.t.W - 1
	e.tagHit = hitRow{}
	if e.prompting {
		s := truncate(e.cmdline, w)
		return row{s, false}, utf8.RuneCountInString(s)
	}
	ln := body.line(body.cur)
	right := fmt.Sprintf("%d,%d", ln+1, body.cur-body.t.Bol(ln)+1)
	if !body.dot.Empty() {
		right += fmt.Sprintf("  #%d,#%d", body.dot.Q0, body.dot.Q1)
	}
	rw := utf8.RuneCountInString(right)
	room := max(0, w-rw-2)
	if e.f != tag {
		left, _, _ := strings.Cut(e.msg, "\n") // a message is one line
		if left == "" && e.mode != Normal {
			left = modeNames[e.mode]
		}
		if left != "" {
			if lw := utf8.RuneCountInString(left); lw <= room {
				return row{left + strings.Repeat(" ", w-lw-rw) + right, false}, -1
			}
			return row{truncate(left, w), false}, -1
		}
	}
	cells := e.layout(tag, 0, nil)
	cells = cells[:min(len(cells), room)]
	e.tagHit = hitRow{tag, cellPos(cells), tag.t.Eol(0)}
	r := render(tag, "", cells, false, w)
	x := -1
	if e.f == tag {
		x = min(cursorCell(cells, tag.cur), room)
	}
	return row{r.s + strings.Repeat(" ", w-len(cells)-rw) + right, false}, x
}

// flush sends the rows that changed since the last time.
func (e *Editor) flush(rows []row, cy, cx int) {
	var b strings.Builder
	b.WriteString(cursorHide)
	if len(e.screen) != len(rows) {
		b.WriteString(sgrReset + clear)
		e.screen = make([]string, len(rows))
		for i := range e.screen {
			e.screen[i] = "\x00"
		}
	}
	for i, r := range rows {
		if e.screen[i] == r.s {
			continue
		}
		b.WriteString(cup(i+1, 1))
		b.WriteString(r.s)
		if !r.full {
			b.WriteString(eraseEOL)
		}
		e.screen[i] = r.s
	}
	shape := cursorBlock
	switch {
	case e.mode == Insert || e.prompting:
		shape = cursorBar
	}
	if shape != e.shape {
		b.WriteString(shape)
		e.shape = shape
	}
	b.WriteString(cup(cy+1, cx+1))
	b.WriteString(cursorShow)
	e.t.write(b.String())
}

// completion completes the last word of a command line as a file
// name. The first TAB completes as far as the names agree; more TABs
// cycle through them.
type completion struct {
	head    string
	matches []string
	i       int
}

func (c *completion) next(buf []rune) []rune {
	if c.matches == nil {
		s := string(buf)
		i := strings.LastIndexAny(s, " \t") + 1
		word := s[i:]
		m, _ := filepath.Glob(expandHome(word) + "*")
		if len(m) == 0 {
			return buf
		}
		for j, name := range m {
			if fi, err := os.Stat(name); err == nil && fi.IsDir() {
				m[j] += "/"
			}
		}
		c.head, c.matches, c.i = s[:i], m, -1
		p := m[0]
		for _, name := range m[1:] {
			for !strings.HasPrefix(name, p) {
				p = p[:len(p)-1]
			}
		}
		if len(m) == 1 {
			c.matches = nil // a directory can be completed further
		}
		if len(m) == 1 || len(p) > len(word) {
			return []rune(c.head + p)
		}
	}
	c.i = (c.i + 1) % len(c.matches)
	return []rune(c.head + c.matches[c.i])
}

// prompt reads a line on the status row.
func (e *Editor) prompt(prefix, init string) (string, bool) {
	buf := []rune(init)
	h := e.hist[prefix]
	hi := len(h)
	e.prompting = true
	defer func() { e.prompting = false }()
	var comp completion
	for {
		e.cmdline = prefix + string(buf)
		e.draw()
		k := e.in.Get()
		if k != '\t' {
			comp = completion{}
		}
		switch {
		case k == '\t' && prefix == ":":
			buf = comp.next(buf)
		case k == KeyEsc || k == KeyEOF || k == ctrl('c'):
			return "", false
		case k == KeyEnter || k == '\n':
			s := string(buf)
			if s != "" && (len(h) == 0 || h[len(h)-1] != s) {
				e.hist[prefix] = append(h, s)
			}
			return s, true
		case k == KeyBS || k == ctrl('h'):
			if len(buf) == 0 {
				return "", false
			}
			buf = buf[:len(buf)-1]
		case k == ctrl('u'):
			buf = buf[:0]
		case k == ctrl('w'):
			i := len(buf)
			for i > 0 && buf[i-1] == ' ' {
				i--
			}
			for i > 0 && buf[i-1] != ' ' {
				i--
			}
			buf = buf[:i]
		case k == ctrl('v'):
			if k := e.in.Get(); k >= 0 {
				buf = append(buf, rune(k))
			}
		case k == KeyUp:
			if hi > 0 {
				hi--
				buf = []rune(h[hi])
			}
		case k == KeyDown:
			if hi < len(h)-1 {
				hi++
				buf = []rune(h[hi])
			} else {
				hi, buf = len(h), buf[:0]
			}
		case k >= ' ' || k == '\t':
			buf = append(buf, rune(k))
		}
	}
}
