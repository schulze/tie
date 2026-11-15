package main

import "slices"

// The tag, as in acme: every buffer has a line of text of its own,
// shown on the bottom line, that holds commands to execute and
// anything else worth keeping at hand. It starts as defaultTag
// (config.go) and is edited like any text: keyTag goes there and back.
//
// A tag is itself a small scratch File whose owner is the buffer. While
// the cursor is in the tag, e.f is the tag, so every editing command
// works on it. Everything else, :commands, execute and look, works on
// the buffer: they leave the tag first. Dot in the buffer stays as it
// was meanwhile, so select text in the buffer, go to the tag and
// execute |sort there: the selection is sorted.

// tagFile returns f's tag, making it on first use.
func (f *File) tagFile() *File {
	if f.tag == nil {
		f.tag = newFile("")
		f.tag.t = Text{r: []rune(defaultTag + "\n")}
		f.tag.scratch = true
		f.tag.owner = f
	}
	return f.tag
}

// buffer is the buffer being edited: e.f, or the owner of the tag the
// cursor is in.
func (e *Editor) buffer() *File {
	if e.f.owner != nil {
		return e.f.owner
	}
	return e.f
}

// leaveTag puts the cursor back in the buffer. It reports whether the
// cursor was in the tag.
func (e *Editor) leaveTag() bool {
	if e.f.owner == nil {
		return false
	}
	e.f = e.f.owner
	return true
}

// tagCmd (keyTag) goes into the tag and back.
func tagCmd(e *Editor, c *ctx) {
	if !e.leaveTag() {
		e.f = e.f.tagFile()
	}
	e.keepDot = true
}

// buffersCmd (keyBuffers) puts the cursor on the buffer line to choose
// a buffer: its number goes there (keyBuffers 2: buffer 2), h and l move
// along the line and RETURN goes to the buffer under the cursor; ESC or
// keyBuffers again stays where you were.
func buffersCmd(e *Editor, c *ctx) {
	e.keepDot = true
	sel := slices.Index(e.files, e.buffer())
	n := 0
	e.choosing = &sel
	defer func() { e.choosing = nil }()
	for {
		e.msg = "buffer: number, h l and RETURN, or ESC"
		e.draw()
		k := e.in.Get()
		switch {
		case k >= '0' && k <= '9':
			n = n*10 + int(k-'0')
			if n < 1 || n > len(e.files) {
				n = 0
				continue
			}
			sel = n - 1
			if n*10 <= len(e.files) {
				continue // buffer 1 of 12 could still become 12
			}
		case k == 'h' || k == KeyLeft:
			sel, n = max(0, sel-1), 0
			continue
		case k == 'l' || k == KeyRight:
			sel, n = min(len(e.files)-1, sel+1), 0
			continue
		case k != KeyEnter && k != '\n' && k != ' ':
			e.msg = ""
			return
		}
		e.msg = ""
		e.switchTo(e.files[sel])
		return
	}
}
