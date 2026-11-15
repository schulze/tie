package main

import (
	"slices"
	"sort"
)

// eof is returned by Text.At for positions outside the text.
const eof rune = -1

// Text is the editor's copy of a file: a flat sequence of runes.
// Positions (q) are rune offsets, as in sam and acme. A line index
// is built lazily and discarded on every change.
//
// Everything that touches the representation lives here, so the
// slice can later be replaced by a piece table or gap buffer.
type Text struct {
	r     []rune
	lines []int // start offset of each line; nil when stale
}

func (t *Text) Len() int { return len(t.r) }

func (t *Text) At(q int) rune {
	if q < 0 || q >= len(t.r) {
		return eof
	}
	return t.r[q]
}

// Runes returns a copy of the text in [q0, q1).
func (t *Text) Runes(q0, q1 int) []rune { return slices.Clone(t.r[q0:q1]) }

func (t *Text) String(q0, q1 int) string { return string(t.r[q0:q1]) }

func (t *Text) insert(q int, s []rune) {
	t.r = slices.Insert(t.r, q, s...)
	t.lines = nil
}

func (t *Text) delete(q0, q1 int) {
	t.r = slices.Delete(t.r, q0, q1)
	t.lines = nil
}

func (t *Text) index() []int {
	if t.lines == nil {
		t.lines = append(make([]int, 0, 64), 0)
		for i, r := range t.r {
			if r == '\n' && i+1 < len(t.r) {
				t.lines = append(t.lines, i+1)
			}
		}
	}
	return t.lines
}

// Nlines returns the number of lines. An empty text has one empty line.
func (t *Text) Nlines() int { return len(t.index()) }

// Line returns the (0-based) line containing q.
func (t *Text) Line(q int) int {
	l := t.index()
	return sort.Search(len(l), func(i int) bool { return l[i] > q }) - 1
}

// Bol returns the offset of the first rune of line ln.
func (t *Text) Bol(ln int) int {
	l := t.index()
	return l[max(0, min(ln, len(l)-1))]
}

// Eol returns the offset of the newline ending line ln, or Len
// if the line is not terminated.
func (t *Text) Eol(ln int) int {
	l := t.index()
	if ln+1 < len(l) {
		return l[ln+1] - 1
	}
	if n := len(t.r); n > 0 && t.r[n-1] == '\n' {
		return n - 1
	}
	return len(t.r)
}
