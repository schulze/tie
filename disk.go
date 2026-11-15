package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Files that change on disk. Each buffer remembers the modification
// time and size of its file when it was last read or written. tie looks
// again after every command and, while waiting for a key, every second:
//
//   - a buffer without changes is read again, keeping the cursor where
//     it was;
//   - a buffer with changes is marked stale: the status line says so, the
//     buffer line shows ! after its name, and :w refuses to overwrite the
//     file. :diff (Diff in the tag) shows the differences; then Get takes
//     the file from disk and :w! keeps the buffer.

type diskState struct {
	mod  time.Time
	size int64
	ok   bool // the file was on disk
}

func statDisk(name string) diskState {
	fi, err := os.Stat(name)
	if err != nil {
		return diskState{}
	}
	return diskState{fi.ModTime(), fi.Size(), true}
}

// noteDisk records the file as it is on disk now.
func (f *File) noteDisk() {
	f.disk, f.stale = statDisk(f.name), false
}

// checkDisk looks for files that changed on disk.
func (e *Editor) checkDisk() {
	for _, f := range e.files {
		if f.name == "" || f.scratch && !f.isDir {
			continue
		}
		d := statDisk(f.name)
		if d == f.disk || f.stale {
			continue
		}
		switch {
		case !d.ok && !f.disk.ok:
			continue
		case d.ok && !f.dirty():
			e.reload(f)
			if f == e.buffer() {
				e.msg = fmt.Sprintf("%q changed on disk: read again", f.name)
			}
		default:
			f.stale = true
			if f == e.buffer() {
				e.staleMsg(f)
			}
		}
	}
}

func (e *Editor) staleMsg(f *File) {
	what := "changed"
	if !statDisk(f.name).ok {
		what = "was removed"
	}
	e.msg = fmt.Sprintf("%q %s on disk: Diff compares, Get takes it, :w! keeps yours", f.name, what)
}

// reload reads f again, keeping the cursor on the same line and column.
func (e *Editor) reload(f *File) error {
	ln, col, top := f.line(f.cur), f.cur-f.bol(f.cur), f.top
	if _, err := f.load(); err != nil {
		return err
	}
	ln = min(ln, f.t.Nlines()-1)
	f.cur = min(f.t.Bol(ln)+col, f.t.Eol(ln))
	f.top = min(top, ln)
	return nil
}

// exDiff shows how the buffer differs from its file on disk, as
// diff -u: lines with - are on disk, lines with + in the buffer.
func exDiff(e *Editor, x *exArgs) error {
	f := e.f
	if f.name == "" || f.scratch {
		return errors.New("Diff: no file to compare with")
	}
	tmp, err := os.CreateTemp("", "tie-diff-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(encode(f.t.r))
	tmp.Close()
	if err != nil {
		return err
	}
	disk := f.name
	if !statDisk(disk).ok {
		disk = "/dev/null"
	}
	c := sh(`diff -u -L "$1 (on disk)" -L "$1 (buffer)" "$2" "$3"`, "", f.name, disk, tmp.Name())
	out, err := c.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		e.msg = fmt.Sprintf("%q: no differences", f.name)
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		e.appendErrors(e.base(), decode(out))
	default:
		return fmt.Errorf("Diff: %v %s", err, out)
	}
	return nil
}
