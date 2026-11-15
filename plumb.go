package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// Opening files from the text, as acme does with the right button.
//
// Look (buttons.go) takes a file name from the
// text and opens it. A name may carry an address, the way compilers and
// grep -n print them:
//
//	file.go:12      line 12        file.go:12:5   line 12, column 5
//	file.go:/re/    the match      file.go:#40    rune offset 40
//
// Relative names are relative to the directory of the current buffer.
// A directory opens as a listing of its entries, in which look opens
// files in turn. Output of executed commands and of >cmd goes to a
// scratch buffer dir/+Errors, where the same works: execute ls, then
// look at any name.

// sh makes a shell command run in dir; args become $1, $2, ...
func sh(cmd, dir string, args ...string) *exec.Cmd {
	c := exec.Command("/bin/sh", append([]string{"-c", cmd, "tie"}, args...)...)
	c.Dir = dir
	return c
}

func isFileChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-+/~@=%,", r)
}

// nameAt expands the file name (with its address) around q.
func (f *File) nameAt(q int) Range {
	in := func(r rune) bool { return isFileChar(r) || r == ':' }
	a, b := q, q
	for a > 0 && in(f.t.At(a-1)) {
		a--
	}
	for in(f.t.At(b)) {
		b++
	}
	return Range{a, b}
}

// nameAround is the file name around d: the name d is part of (d may
// be empty, or a search match inside a name), or else d itself, which
// may contain any characters.
func (f *File) nameAround(d Range) string {
	r := f.nameAt(d.Q0)
	if d.Q0 < r.Q0 || d.Q1 > r.Q1 {
		r = d
	}
	return strings.Trim(f.t.String(r.Q0, r.Q1), ":")
}

// base is the directory relative names are taken in.
func (e *Editor) base() string {
	f := e.buffer()
	if f.isDir {
		return f.name
	}
	return filepath.Dir(f.name) // "." for unnamed and scratch buffers
}

func expandHome(name string) string {
	if rest, ok := strings.CutPrefix(name, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return name
}

// resolve splits text into a file name, made absolute or relative to
// the buffer's directory, and an address.
func (e *Editor) resolve(text string) (name, addr string, ok bool) {
	name, addr, _ = strings.Cut(strings.TrimSpace(text), ":")
	if name == "" {
		return "", "", false
	}
	name = expandHome(name)
	if !filepath.IsAbs(name) {
		name = filepath.Join(e.base(), name)
	}
	return name, addr, true
}

// plumb opens the file named by text and goes to its address.
func (e *Editor) plumb(text string) error {
	name, addr, ok := e.resolve(text)
	if !ok {
		return errors.New("No file name")
	}
	if _, err := os.Stat(name); err != nil {
		return fmt.Errorf("%s: no such file", name)
	}
	e.open(name)
	addr = strings.TrimRight(addr, ":")
	if addr == "" {
		return nil
	}
	// line:column, as compilers print it; anything else is an address.
	l, c, _ := strings.Cut(addr, ":")
	n, err1 := strconv.Atoi(l)
	col, err2 := strconv.Atoi(c)
	if err1 != nil || err2 != nil {
		return e.ex(addr)
	}
	f := e.f
	ln := max(1, min(n, f.t.Nlines())) - 1
	f.marks['\''] = f.cur
	f.cur = min(f.t.Bol(ln)+max(0, col-1), f.t.Eol(ln))
	return nil
}

// runCaptured runs cmd in the buffer's directory and appends what it
// prints, standard output and error together, to dir/+Errors.
func (e *Editor) runCaptured(cmd string, input []rune) {
	dir := e.base()
	c := sh(cmd, dir)
	c.Stdin = bytes.NewReader(encode(input))
	out, err := c.CombinedOutput()
	text := decode(out)
	if err != nil {
		text = append(text, []rune(fmt.Sprintf("%s: %v\n", cmd, err))...)
	}
	if len(text) == 0 {
		e.msg = "(no output)"
		return
	}
	e.appendErrors(dir, text)
}

func (e *Editor) appendErrors(dir string, text []rune) {
	name := filepath.Join(dir, "+Errors")
	var f *File
	for _, g := range e.files {
		if g.name == name {
			f = g
		}
	}
	if f == nil {
		f = newFile(name)
		f.scratch = true
		e.addFile(f)
	}
	if text[len(text)-1] != '\n' {
		text = append(text, '\n')
	}
	q := f.t.Len()
	f.insert(q, text)
	f.commit()
	e.switchTo(f)
	f.cur, f.dot = q, Range{q, q}
}
