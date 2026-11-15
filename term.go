package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"
)

// Control sequences. The first group is what sim uses (VT100 / ECMA-48);
// the rest are the xterm extensions every current terminal understands.
const (
	csi      = "\x1b["
	clear    = csi + "2J" // ED: erase display
	eraseEOL = csi + "K"  // EL: erase to end of line
	home     = csi + "H"  // CUP 1;1
	cprQuery = csi + "6n" // DSR: terminal answers CSI row;col R

	sgrReset   = csi + "m"
	sgrReverse = csi + "7m"

	cursorHide   = csi + "?25l"
	cursorShow   = csi + "?25h"
	altScreenOn  = csi + "?1049h"
	altScreenOff = csi + "?1049l"
	cursorBlock  = csi + "2 q" // DECSCUSR
	cursorBar    = csi + "6 q"
	cursorReset  = csi + "0 q"

	// Mouse reporting: button presses and releases (1000), motion while
	// a button is down (1002), in the SGR encoding (1006).
	mouseOn  = csi + "?1000h" + csi + "?1002h" + csi + "?1006h"
	mouseOff = csi + "?1006l" + csi + "?1002l" + csi + "?1000l"
)

// cup moves the cursor; row and col are 1-based.
func cup(row, col int) string { return fmt.Sprintf(csi+"%d;%dH", row, col) }

// Term is the display. A Term with fd < 0 is headless (used by tests).
type Term struct {
	fd   int
	old  *term.State
	out  io.Writer
	W, H int
	tab  int         // the terminal's tab width
	pipe [2]*os.File // SIGWINCH self-pipe; kept so it is not collected

	mouse bool // mouse reporting is wanted
}

// setMouse turns mouse reporting on or off.
func (t *Term) setMouse(on bool) {
	t.mouse = on
	if t.fd < 0 {
		return
	}
	if on {
		t.write(mouseOn)
	} else {
		t.write(mouseOff)
	}
}

func headlessTerm(w, h int) (*Term, *Input) {
	return &Term{fd: -1, out: io.Discard, W: w, H: h, tab: 8}, &Input{fd: -1, sigfd: -1}
}

func openTerm() (*Term, *Input, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, nil, errors.New("standard input is not a terminal")
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, nil, err
	}
	r, w, err := os.Pipe()
	if err != nil {
		term.Restore(fd, old)
		return nil, nil, err
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	go func() {
		for range sig {
			w.Write([]byte{0})
		}
	}()
	t := &Term{fd: fd, old: old, out: os.Stdout, pipe: [2]*os.File{r, w}}
	in := &Input{fd: fd, sigfd: int(r.Fd())}
	t.write(altScreenOn)
	t.tab = t.queryTab(in)
	t.size(in)
	return t, in, nil
}

func (t *Term) write(s string) { io.WriteString(t.out, s) }

// queryTab finds the terminal's tab width: print a tab at the home
// position and ask where the cursor went.
func (t *Term) queryTab(in *Input) int {
	t.write(home + "\t" + cprQuery)
	_, col, ok := in.cpr(500 * time.Millisecond)
	t.write(home + clear)
	if ok && col > 1 && col < 64 {
		return col - 1
	}
	return 8
}

// size asks the kernel for the window size (TIOCGWINSZ). If that fails,
// it falls back to the VT100 way: move far down and right, then ask
// where the cursor ended up.
func (t *Term) size(in *Input) {
	if t.fd < 0 {
		return
	}
	if w, h, err := term.GetSize(t.fd); err == nil && w > 0 && h > 0 {
		t.W, t.H = w, h
		return
	}
	if in != nil {
		t.write(cup(999, 999) + cprQuery)
		if r, c, ok := in.cpr(500 * time.Millisecond); ok {
			t.W, t.H = c, r
			return
		}
	}
	if t.W == 0 {
		t.W, t.H = 80, 24
	}
}

// suspend gives the terminal back (for shell commands and ^Z).
func (t *Term) suspend() {
	if t.fd < 0 {
		return
	}
	t.write(mouseOff + sgrReset + cursorReset + cursorShow + altScreenOff)
	term.Restore(t.fd, t.old)
}

// resume puts the terminal back into raw mode, on the normal screen.
func (t *Term) resume() {
	if t.fd < 0 {
		return
	}
	term.MakeRaw(t.fd)
}

// enter switches to the alternate screen and clears it.
func (t *Term) enter() {
	if t.fd < 0 {
		return
	}
	t.write(altScreenOn + sgrReset + clear + home)
	if t.mouse {
		t.write(mouseOn)
	}
	t.size(nil)
}

func (t *Term) close() { t.suspend() }
