package main

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
)

type mkind int

const (
	exclusive mkind = iota // the target character is not included
	inclusive              // the target character is included
	linewise               // whole lines
)

// motion flags
const (
	mVert   = 1 << iota // keeps the wanted column (j, k)
	mDollar             // sets the wanted column to the end of line
	mJump               // remembers the previous position in mark '
)

// A motion computes a target position from the cursor. Motions move
// the cursor in normal mode, extend dot in visual mode and give the
// extent of an operator (d, c, y, ...).
type motion struct {
	fn   func(*Editor, *ctx) (int, bool)
	kind mkind
	flag int
}

var motions map[Key]motion

func init() {
	left := motion{mLeft, exclusive, 0}
	right := motion{mRight, exclusive, 0}
	down := motion{mDown, linewise, mVert}
	up := motion{mUp, linewise, mVert}
	bol := motion{mBol, exclusive, 0}
	eol := motion{mEol, inclusive, mDollar}
	screen := motion{mScreen, linewise, mJump}
	search := motion{mSearch, exclusive, mJump}
	again := motion{mNext, exclusive, mJump}
	star := motion{mStar, exclusive, mJump}
	motions = map[Key]motion{
		'h': left, KeyLeft: left,
		'l': right, KeyRight: right,
		'j': down, KeyDown: down,
		'k': up, KeyUp: up,
		'0': bol, KeyHome: bol,
		'^': {mFirst, exclusive, 0},
		'$': eol, KeyEnd: eol,
		'w': {mWord, exclusive, 0}, 'W': {mWord, exclusive, 0},
		'b': {mBack, exclusive, 0}, 'B': {mBack, exclusive, 0},
		'e': {mWordEnd, inclusive, 0}, 'E': {mWordEnd, inclusive, 0},
		'G': {mGoto, linewise, mJump},
		'H': screen, 'M': screen, 'L': screen,
		'f': {mFind, inclusive, 0}, 'F': {mFind, exclusive, 0},
		';': {mRefind, inclusive, 0}, ',': {mRefind, inclusive, 0},
		'%': {mMatch, inclusive, mJump},
		'}': {mParaFwd, exclusive, mJump}, '{': {mParaBack, exclusive, mJump},
		'/': search,
		'n': again, 'N': again,
		'*': star, '#': star,
		'\'': {mMark, linewise, mJump},
	}
}

// move applies motion m to the cursor.
func (e *Editor) move(c *ctx, m motion) bool {
	f := e.f
	c.kind = m.kind
	e.match = nil
	from := f.cur
	q, ok := m.fn(e, c)
	if !ok {
		return false
	}
	if m.flag&mJump != 0 {
		f.marks['\''] = from
	}
	f.cur = q
	e.keepWant = true
	switch {
	case m.flag&mDollar != 0:
		f.want = math.MaxInt
	case m.flag&mVert == 0:
		f.want = e.col(q)
	}
	if e.match != nil && e.mode == Normal {
		f.dot = *e.match
		e.keepDot = true
	}
	return true
}

func mLeft(e *Editor, c *ctx) (int, bool) {
	f := e.f
	q, b := f.cur, f.bol(f.cur)
	for i := 0; i < c.n() && q > b; i++ {
		q--
	}
	return q, true
}

func mRight(e *Editor, c *ctx) (int, bool) {
	f := e.f
	q, end := f.cur, f.eol(f.cur)
	for range c.n() {
		if q+1 < end || c.op != 0 && q < end {
			q++
		}
	}
	return q, true
}

func (e *Editor) vertical(n int) (int, bool) {
	f := e.f
	cl := f.line(f.cur)
	ln := max(0, min(cl+n, f.t.Nlines()-1))
	if ln == cl {
		return 0, false
	}
	return ln, true
}

func mDown(e *Editor, c *ctx) (int, bool) {
	ln, ok := e.vertical(c.n())
	return e.atCol(ln, e.f.want), ok
}

func mUp(e *Editor, c *ctx) (int, bool) {
	ln, ok := e.vertical(-c.n())
	return e.atCol(ln, e.f.want), ok
}

func mBol(e *Editor, c *ctx) (int, bool)   { return e.f.bol(e.f.cur), true }
func mFirst(e *Editor, c *ctx) (int, bool) { return e.f.firstNonBlank(e.f.line(e.f.cur)), true }

func mEol(e *Editor, c *ctx) (int, bool) {
	f := e.f
	ln := min(f.line(f.cur)+c.n()-1, f.t.Nlines()-1)
	q := f.t.Eol(ln)
	if q == f.t.Bol(ln) {
		c.kind = exclusive // nothing to include on an empty line
		return q, true
	}
	return q - 1, true
}

func isBlank(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }

// class sorts runes for word motions: 0 blank, 1 word, 2 punctuation.
// For W, B and E (big) every non-blank is a word character.
func class(r rune, big bool) int {
	switch {
	case r == eof || isBlank(r):
		return 0
	case big, r == '_', unicode.IsLetter(r), unicode.IsDigit(r):
		return 1
	}
	return 2
}

func (f *File) emptyLineAt(q int) bool {
	return f.t.At(q) == '\n' && (q == 0 || f.t.At(q-1) == '\n')
}

func (f *File) wordFwd(q int, big bool) int {
	n := f.t.Len()
	if c := class(f.t.At(q), big); c != 0 {
		for q < n && class(f.t.At(q), big) == c {
			q++
		}
	}
	for q < n && isBlank(f.t.At(q)) {
		if f.t.At(q) == '\n' && f.t.At(q+1) == '\n' {
			return q + 1 // an empty line counts as a word
		}
		q++
	}
	return q
}

func (f *File) wordEnd(q int, big bool) int {
	n := f.t.Len()
	q++
	for q < n && isBlank(f.t.At(q)) {
		q++
	}
	if q >= n {
		return max(0, n-1)
	}
	c := class(f.t.At(q), big)
	for q+1 < n && class(f.t.At(q+1), big) == c {
		q++
	}
	return q
}

func (f *File) wordBack(q int, big bool) int {
	if q == 0 {
		return 0
	}
	q--
	for q > 0 && isBlank(f.t.At(q)) && !f.emptyLineAt(q) {
		q--
	}
	c := class(f.t.At(q), big)
	if c == 0 {
		return q
	}
	for q > 0 && class(f.t.At(q-1), big) == c {
		q--
	}
	return q
}

func isBig(k Key) bool { return k >= 'A' && k <= 'Z' }

func mWord(e *Editor, c *ctx) (int, bool) {
	f := e.f
	q := f.cur
	for range c.n() {
		q = f.wordFwd(q, isBig(c.key))
	}
	// With an operator, a w that crosses the end of a line stops
	// there: dw on the last word of a line leaves the newline alone.
	if c.op != 0 {
		ln := f.t.Nlines()
		if q < f.t.Len() {
			ln = f.line(q)
		}
		if ln > f.line(f.cur) && (q >= f.t.Len() || q <= f.firstNonBlank(ln)) {
			q = max(f.cur, f.t.Eol(ln-1))
		}
	}
	return q, true
}

func mBack(e *Editor, c *ctx) (int, bool) {
	q := e.f.cur
	for range c.n() {
		q = e.f.wordBack(q, isBig(c.key))
	}
	return q, true
}

func mWordEnd(e *Editor, c *ctx) (int, bool) {
	q := e.f.cur
	for range c.n() {
		q = e.f.wordEnd(q, isBig(c.key))
	}
	return q, true
}

func mGoto(e *Editor, c *ctx) (int, bool) {
	nl := e.f.t.Nlines()
	ln := nl - 1
	if c.count > 0 {
		ln = min(c.count, nl) - 1
	}
	return e.f.firstNonBlank(ln), true
}

func mScreen(e *Editor, c *ctx) (int, bool) {
	first, last := e.visible()
	var ln int
	switch c.key {
	case 'H':
		ln = min(first+c.n()-1, last)
	case 'L':
		ln = max(last-c.n()+1, first)
	default:
		ln = (first + last) / 2
	}
	return e.f.firstNonBlank(ln), true
}

func mFind(e *Editor, c *ctx) (int, bool) {
	k := e.in.Get()
	if k < ' ' && k != '\t' {
		return 0, false
	}
	e.find.key, e.find.ch = c.key, rune(k)
	return e.findChar(c.key, rune(k), c.n())
}

var reverseFind = map[Key]Key{'f': 'F', 'F': 'f'}

func mRefind(e *Editor, c *ctx) (int, bool) {
	k := e.find.key
	if k == 0 {
		return 0, false
	}
	if c.key == ',' {
		k = reverseFind[k]
	}
	c.kind = inclusive
	if k == 'F' {
		c.kind = exclusive
	}
	return e.findChar(k, e.find.ch, c.n())
}

// findChar looks for the n-th ch on the current line, forward (f) or back (F).
func (e *Editor) findChar(k Key, ch rune, n int) (int, bool) {
	f := e.f
	q, b, end := f.cur, f.bol(f.cur), f.eol(f.cur)
	for range n {
		if k == 'f' {
			for q++; q < end && f.t.At(q) != ch; q++ {
			}
			if q >= end {
				return 0, false
			}
		} else {
			for q--; q >= b && f.t.At(q) != ch; q-- {
			}
			if q < b {
				return 0, false
			}
		}
	}
	return q, true
}

const brackets = "()[]{}"

// mMatch is %: the partner of the first bracket at or after the cursor
// on its line.
func mMatch(e *Editor, c *ctx) (int, bool) {
	f := e.f
	q, end := f.cur, f.eol(f.cur)
	for q < end && !strings.ContainsRune(brackets, f.t.At(q)) {
		q++
	}
	if q >= end {
		return 0, false
	}
	return f.matchBracket(q)
}

// matchBracket finds the partner of the bracket at q.
func (f *File) matchBracket(q int) (int, bool) {
	r := f.t.At(q)
	i := strings.IndexRune(brackets, r)
	pair := rune(brackets[i^1])
	step := 1
	if i%2 == 1 {
		step = -1
	}
	depth := 0
	for p := q; p >= 0 && p < f.t.Len(); p += step {
		switch f.t.At(p) {
		case r:
			depth++
		case pair:
			depth--
			if depth == 0 {
				return p, true
			}
		}
	}
	return 0, false
}

func mParaFwd(e *Editor, c *ctx) (int, bool) {
	f := e.f
	nl := f.t.Nlines()
	ln := f.line(f.cur)
	for range c.n() {
		for ln < nl-1 && f.emptyLine(ln) {
			ln++
		}
		for ln < nl-1 && !f.emptyLine(ln) {
			ln++
		}
	}
	if f.emptyLine(ln) {
		return f.t.Bol(ln), true
	}
	return f.t.Eol(ln), true
}

func mParaBack(e *Editor, c *ctx) (int, bool) {
	f := e.f
	ln := f.line(f.cur)
	for range c.n() {
		for ln > 0 && f.emptyLine(ln) {
			ln--
		}
		for ln > 0 && !f.emptyLine(ln) {
			ln--
		}
	}
	return f.t.Bol(ln), true
}

func mSearch(e *Editor, c *ctx) (int, bool) {
	pat, ok := e.prompt("/", "")
	if !ok {
		return 0, false
	}
	if pat != "" {
		e.pat = pat
	}
	e.back = false
	return e.searchNext(c, false, e.f.cur)
}

func mNext(e *Editor, c *ctx) (int, bool) {
	back := e.back
	if c.key == 'N' {
		back = !back
	}
	return e.searchNext(c, back, e.f.cur)
}

// mStar searches for the word under the cursor (* forward, # backward).
func mStar(e *Editor, c *ctx) (int, bool) {
	f := e.f
	a, b := f.cur, f.cur
	if class(f.t.At(a), false) != 1 {
		e.msg = "No identifier under cursor"
		return 0, false
	}
	for a > 0 && class(f.t.At(a-1), false) == 1 {
		a--
	}
	for class(f.t.At(b), false) == 1 {
		b++
	}
	e.pat = `\<` + regexp.QuoteMeta(f.t.String(a, b)) + `\>`
	e.back = c.key == '#'
	return e.searchNext(c, e.back, a)
}

// searchNext finds the n-th match of the last pattern and records it
// in e.match, so that in normal mode the match becomes dot.
func (e *Editor) searchNext(c *ctx, back bool, from int) (int, bool) {
	if e.pat == "" {
		e.msg = "No previous regular expression"
		return 0, false
	}
	re, err := e.compile(e.pat)
	if err != nil {
		e.msg = err.Error()
		return 0, false
	}
	q := from
	var r Range
	for range c.n() {
		var wrapped, ok bool
		r, wrapped, ok = e.f.search(re, q, back)
		if !ok {
			e.msg = fmt.Sprintf("Pattern not found: %s", e.pat)
			return 0, false
		}
		if wrapped {
			e.msg = "search wrapped"
		}
		q = r.Q0
	}
	e.match = &r
	return r.Q0, true
}

func mMark(e *Editor, c *ctx) (int, bool) {
	f := e.f
	k := e.in.Get()
	q, ok := f.marks[rune(k)]
	if !ok {
		e.msg = "Mark not set"
		return 0, false
	}
	return f.firstNonBlank(f.line(min(q, f.t.Len()))), true
}
