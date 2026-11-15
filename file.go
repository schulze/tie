package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"unicode/utf8"
)

// Bytes that are not valid UTF-8 are kept as runes rawBase+b so that
// a file survives a load/save cycle unchanged.
const rawBase = 0x10FF00

func decode(b []byte) []rune {
	r := make([]rune, 0, len(b))
	for len(b) > 0 {
		c, n := utf8.DecodeRune(b)
		if c == utf8.RuneError && n == 1 {
			c = rawBase + rune(b[0])
		}
		r = append(r, c)
		b = b[n:]
	}
	return r
}

func encode(r []rune) []byte {
	b := make([]byte, 0, len(r))
	for _, c := range r {
		if c >= rawBase && c <= rawBase+0xff {
			b = append(b, byte(c-rawBase))
		} else {
			b = utf8.AppendRune(b, c)
		}
	}
	return b
}

// An edit is either an insertion or a deletion at q.
type edit struct {
	q        int
	del, ins []rune
}

// A change is the group of edits made by one command; it is the unit of undo.
type change struct {
	id    int
	edits []edit
}

// File is a buffer being edited.
type File struct {
	name  string
	t     Text
	cur   int   // cursor
	dot   Range // the selection, as in acme
	top   int   // first line on the screen
	want  int   // column that j and k try to keep
	marks map[rune]int
	// A scratch buffer (help, +Errors, a directory) is never "modified".
	scratch bool
	isDir   bool
	tag     *File     // the tag line (tag.go)
	owner   *File     // for a tag: its buffer
	disk    diskState // the file when last read or written (disk.go)
	stale   bool      // it changed on disk, and so did the buffer

	undo, redo []*change
	pend       *change // edits of the running command
	seq, saved int
}

func newFile(name string) *File { return &File{name: name, marks: map[rune]int{}} }

func (f *File) line(q int) int         { return f.t.Line(q) }
func (f *File) bol(q int) int          { return f.t.Bol(f.t.Line(q)) }
func (f *File) eol(q int) int          { return f.t.Eol(f.t.Line(q)) }
func (f *File) lineEnd(ln int) int     { return min(f.t.Eol(ln)+1, f.t.Len()) }
func (f *File) lineRange(ln int) Range { return Range{f.t.Bol(ln), f.lineEnd(ln)} }
func (f *File) emptyLine(ln int) bool  { return f.t.Bol(ln) == f.t.Eol(ln) }

func (f *File) firstNonBlank(ln int) int {
	q := f.t.Bol(ln)
	for r := f.t.At(q); r == ' ' || r == '\t'; r = f.t.At(q) {
		q++
	}
	return q
}

// lastLine is the line of the last rune in r (or of r.Q0 if r is empty).
func (f *File) lastLine(r Range) int { return f.line(r.last()) }

// lines extends r to whole lines.
func (f *File) lines(r Range) Range { return Range{f.bol(r.Q0), f.lineEnd(f.lastLine(r))} }

// title is the file's name for people.
func (f *File) title() string {
	if f.name == "" {
		return "[No Name]"
	}
	return f.name
}

func (f *File) state() int {
	if len(f.undo) == 0 {
		return 0
	}
	return f.undo[len(f.undo)-1].id
}

// dirty reports unwritten changes. A buffer without a file is scratch,
// as +Errors is: it closes without asking, until :w name names it.
func (f *File) dirty() bool {
	return !f.scratch && f.name != "" && (f.pend != nil || f.state() != f.saved)
}

// rawInsert and rawDelete change the text and keep marks in place.
func (f *File) rawInsert(q int, s []rune) {
	f.t.insert(q, s)
	for k, m := range f.marks {
		if m >= q {
			f.marks[k] = m + len(s)
		}
	}
}

func (f *File) rawDelete(q0, q1 int) {
	f.t.delete(q0, q1)
	for k, m := range f.marks {
		switch {
		case m >= q1:
			f.marks[k] = m - (q1 - q0)
		case m > q0:
			f.marks[k] = q0
		}
	}
}

// record adds ed to the pending change, merging it with the previous
// edit when typing or backspacing.
func (f *File) record(ed edit) {
	if f.pend == nil {
		f.seq++
		f.pend = &change{id: f.seq}
	}
	p := f.pend
	if n := len(p.edits); n > 0 {
		l := &p.edits[n-1]
		switch {
		case ed.ins != nil && l.ins != nil && l.q+len(l.ins) == ed.q:
			l.ins = append(l.ins, ed.ins...)
			return
		case ed.del != nil && l.del != nil && ed.q+len(ed.del) == l.q:
			l.del = append(slices.Clone(ed.del), l.del...)
			l.q = ed.q
			return
		case ed.del != nil && l.ins != nil && ed.q >= l.q && ed.q+len(ed.del) == l.q+len(l.ins):
			l.ins = l.ins[:ed.q-l.q]
			if len(l.ins) == 0 {
				p.edits = p.edits[:n-1]
			}
			return
		}
	}
	p.edits = append(p.edits, ed)
}

func (f *File) insert(q int, s []rune) {
	if len(s) == 0 {
		return
	}
	f.record(edit{q: q, ins: slices.Clone(s)})
	f.rawInsert(q, s)
}

func (f *File) delete(q0, q1 int) []rune {
	if q1 <= q0 {
		return nil
	}
	d := f.t.Runes(q0, q1)
	f.record(edit{q: q0, del: d})
	f.rawDelete(q0, q1)
	return d
}

func (f *File) replace(q0, q1 int, s []rune) {
	f.delete(q0, q1)
	f.insert(q0, s)
}

// commit closes the pending change. A non-empty text always ends in
// a newline, as in vi.
func (f *File) commit() {
	if f.pend == nil {
		return
	}
	if n := f.t.Len(); n > 0 && f.t.At(n-1) != '\n' {
		f.insert(n, []rune{'\n'})
	}
	if len(f.pend.edits) > 0 {
		f.undo = append(f.undo, f.pend)
		f.redo = nil
	}
	f.pend = nil
}

// undoStep reverts the last change and returns where it happened.
func (f *File) undoStep() (int, bool) {
	f.commit()
	n := len(f.undo)
	if n == 0 {
		return 0, false
	}
	ch := f.undo[n-1]
	f.undo = f.undo[:n-1]
	q := f.t.Len()
	for i := len(ch.edits) - 1; i >= 0; i-- {
		ed := ch.edits[i]
		if ed.ins != nil {
			f.rawDelete(ed.q, ed.q+len(ed.ins))
		}
		if ed.del != nil {
			f.rawInsert(ed.q, ed.del)
		}
		q = min(q, ed.q)
	}
	f.redo = append(f.redo, ch)
	return q, true
}

func (f *File) redoStep() (int, bool) {
	n := len(f.redo)
	if n == 0 {
		return 0, false
	}
	ch := f.redo[n-1]
	f.redo = f.redo[:n-1]
	q := f.t.Len()
	for _, ed := range ch.edits {
		if ed.del != nil {
			f.rawDelete(ed.q, ed.q+len(ed.del))
		}
		if ed.ins != nil {
			f.rawInsert(ed.q, ed.ins)
		}
		q = min(q, ed.q)
	}
	f.undo = append(f.undo, ch)
	return q, true
}

// load reads the file from disk, discarding the history.
func (f *File) load() (string, error) {
	*f = File{name: f.name, marks: map[rune]int{}, scratch: f.scratch, seq: f.seq, tag: f.tag}
	f.noteDisk()
	if fi, err := os.Stat(f.name); err == nil && fi.IsDir() {
		return f.loadDir()
	}
	b, err := os.ReadFile(f.name)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Sprintf("%q [New File]", f.name), nil
	}
	if err != nil {
		return "", err
	}
	f.t = Text{r: decode(b)}
	msg := fmt.Sprintf("%q %dL, %dB", f.name, f.t.Nlines(), len(b))
	if n := f.t.Len(); n > 0 && f.t.At(n-1) != '\n' {
		f.t.insert(n, []rune{'\n'})
		msg += " [noeol]"
	}
	if n := f.t.Len(); n == 0 {
		msg = fmt.Sprintf("%q 0L, 0B", f.name)
	}
	return msg, nil
}

// loadDir lists a directory, one entry per line, as acme does.
// Subdirectories end in a slash.
func (f *File) loadDir() (string, error) {
	ents, err := os.ReadDir(f.name)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, d := range ents {
		b.WriteString(d.Name())
		if d.IsDir() {
			b.WriteByte('/')
		}
		b.WriteByte('\n')
	}
	f.t = Text{r: []rune(b.String())}
	f.isDir, f.scratch = true, true
	return fmt.Sprintf("%q %d entries", f.name+"/", len(ents)), nil
}

func writeFile(name string, r []rune) (int, error) {
	b := encode(r)
	return len(b), os.WriteFile(name, b, 0666)
}
