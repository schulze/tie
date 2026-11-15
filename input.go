package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// Key is a typed key: a rune, or one of the special keys below.
type Key rune

const (
	KeyNone Key = -2 - iota
	KeyEOF
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDn
	KeyDelete
	KeyInsert
	KeyMouse // a mouse event; the event is in Input.mouse
)

// Mouse is a mouse event, as reported in xterm's SGR mode (1006).
type Mouse struct {
	b       int  // 0, 1, 2: buttons 1, 2, 3; 64, 65: wheel up and down
	x, y    int  // 1-based cell
	release bool // the button went up
	drag    bool // the mouse moved with the button down
}

const (
	KeyEsc   Key = 0x1b
	KeyEnter Key = '\r'
	KeyBS    Key = 0x7f
)

func ctrl(c rune) Key { return Key(c & 0x1f) }

// How long to wait for the rest of an escape sequence after ESC.
const escTimeout = 50 * time.Millisecond

// Input decodes keys from the terminal. Keys pushed with push (period,
// @x) are read before the terminal. While recording, every key read is
// remembered; that is how period knows what to repeat.
type Input struct {
	fd, sigfd int
	buf       []byte
	eof       bool
	queue     []Key
	rec       []Key
	recording bool
	mouse     Mouse
	onResize  func()
	onIdle    func() // called every second while waiting for a key
}

func (in *Input) Get() Key {
	k := in.get()
	if in.recording {
		in.rec = append(in.rec, k)
	}
	return k
}

func (in *Input) push(keys []Key) { in.queue = append(slices.Clone(keys), in.queue...) }

func (in *Input) record(k Key) {
	in.rec = []Key{k}
	in.recording = true
}

func (in *Input) stop() []Key {
	in.recording = false
	return in.rec
}

func (in *Input) get() Key {
	for {
		if len(in.queue) > 0 {
			k := in.queue[0]
			in.queue = in.queue[1:]
			return k
		}
		b, ok := in.byte(-1)
		switch {
		case !ok:
			return KeyEOF
		case b == 0x1b:
			if k := in.escape(); k != KeyNone {
				return k
			}
		case b < 0x80:
			return Key(b)
		default:
			return in.utf8(b)
		}
	}
}

// fill waits up to timeout (forever if negative) for terminal input.
// A window size change arrives on the self-pipe and is handled here,
// so it can never interrupt a command half way.
func (in *Input) fill(timeout time.Duration) bool {
	if in.fd < 0 || in.eof {
		return false
	}
	ms := -1
	idle := timeout < 0 && in.onIdle != nil
	if idle {
		ms = 1000
	}
	if timeout >= 0 {
		ms = max(1, int(timeout/time.Millisecond))
	}
	for {
		fds := []unix.PollFd{
			{Fd: int32(in.fd), Events: unix.POLLIN},
			{Fd: int32(in.sigfd), Events: unix.POLLIN},
		}
		n, err := unix.Poll(fds, ms)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			in.eof = true
			return false
		}
		if n == 0 {
			if idle {
				in.onIdle()
				continue
			}
			return false
		}
		if fds[1].Revents&unix.POLLIN != 0 {
			var b [64]byte
			unix.Read(in.sigfd, b[:])
			if in.onResize != nil {
				in.onResize()
			}
			continue
		}
		if fds[0].Revents != 0 {
			var b [512]byte
			n, err := unix.Read(in.fd, b[:])
			if n <= 0 {
				if err == unix.EINTR || err == unix.EAGAIN {
					continue
				}
				in.eof = true
				return false
			}
			in.buf = append(in.buf, b[:n]...)
			return true
		}
	}
}

func (in *Input) byte(timeout time.Duration) (byte, bool) {
	if len(in.buf) == 0 && !in.fill(timeout) {
		return 0, false
	}
	b := in.buf[0]
	in.buf = in.buf[1:]
	return b, true
}

func (in *Input) unread(b ...byte) { in.buf = append(slices.Clone(b), in.buf...) }

// escape decodes what follows ESC. A lone ESC is the escape key.
func (in *Input) escape() Key {
	b, ok := in.byte(escTimeout)
	if !ok {
		return KeyEsc
	}
	if b != '[' && b != 'O' {
		in.unread(b)
		return KeyEsc
	}
	var param []byte
	for {
		c, ok := in.byte(escTimeout)
		if !ok {
			return KeyNone
		}
		if c >= 0x40 && c <= 0x7e {
			if len(param) > 0 && param[0] == '<' && (c == 'M' || c == 'm') {
				var n int
				m := Mouse{release: c == 'm'}
				if _, err := fmt.Sscanf(string(param[1:]), "%d;%d;%d", &n, &m.x, &m.y); err != nil {
					return KeyNone
				}
				m.b, m.drag = n&^(4|8|16|32), n&32 != 0
				m.b = clickButton(m.b, n)
				in.mouse = m
				return KeyMouse
			}
			return csiKey(string(param), c)
		}
		param = append(param, c)
	}
}

var csiFinal = map[byte]Key{
	'A': KeyUp, 'B': KeyDown, 'C': KeyRight, 'D': KeyLeft,
	'H': KeyHome, 'F': KeyEnd,
}

var csiTilde = map[string]Key{
	"1": KeyHome, "7": KeyHome, "4": KeyEnd, "8": KeyEnd,
	"2": KeyInsert, "3": KeyDelete, "5": KeyPgUp, "6": KeyPgDn,
}

func csiKey(param string, final byte) Key {
	k, ok := csiFinal[final]
	if final == '~' {
		param, _, _ = strings.Cut(param, ";") // drop modifiers
		k, ok = csiTilde[param]
	}
	if !ok {
		return KeyNone
	}
	return k
}

func (in *Input) utf8(b byte) Key {
	n := 0
	switch {
	case b&0xe0 == 0xc0:
		n = 1
	case b&0xf0 == 0xe0:
		n = 2
	case b&0xf8 == 0xf0:
		n = 3
	default:
		return Key(utf8.RuneError)
	}
	p := []byte{b}
	for range n {
		c, ok := in.byte(escTimeout)
		if !ok {
			break
		}
		if c&0xc0 != 0x80 {
			in.unread(c)
			break
		}
		p = append(p, c)
	}
	r, _ := utf8.DecodeRune(p)
	return Key(r)
}

// clickButton makes button 1 with Option (Alt) or Control held down
// count as the button set in config.go, as acme on macOS does. The
// modifiers are bits of the report: 8 is Alt, 16 is Control.
func clickButton(b, report int) int {
	switch {
	case b != 0:
		return b
	case report&8 != 0 && optionClick > 0:
		return optionClick - 1
	case report&16 != 0 && controlClick > 0:
		return controlClick - 1
	}
	return b
}

// cpr reads a cursor position report (CSI row;col R), keeping any
// other input that arrives meanwhile.
func (in *Input) cpr(timeout time.Duration) (row, col int, ok bool) {
	deadline := time.Now().Add(timeout)
	var keep, seq []byte
	for {
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		b, got := in.byte(left)
		if !got {
			break
		}
		if len(seq) == 0 && b != 0x1b {
			keep = append(keep, b)
			continue
		}
		seq = append(seq, b)
		if b == 'R' {
			if _, err := fmt.Sscanf(string(seq), "\x1b[%d;%dR", &row, &col); err == nil {
				in.unread(keep...)
				return row, col, true
			}
			keep, seq = append(keep, seq...), nil
		} else if len(seq) > 16 {
			keep, seq = append(keep, seq...), nil
		}
	}
	in.unread(append(keep, seq...)...)
	return 0, 0, false
}
