#!/usr/bin/env python3
"""Smoke test of tie on a real pseudo-terminal: the parts go test cannot
reach (raw mode, the tab-width and size queries, poll, resizing, mouse
reporting, the alternate screen).

    go build && python3 testdata/ptysmoke.py ./tie

It answers the terminal's cursor position query like a terminal would,
drives tie with keys and mouse reports, and checks the output.
"""
import fcntl, os, pty, select, signal, struct, sys, tempfile, termios, time

tie = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "./tie")
work = tempfile.mkdtemp()
os.chdir(work)
open("notes", "w").write("ls\n")
open("other.txt", "w").write("alpha\nbeta\n")

pid, fd = pty.fork()
if pid == 0:
    os.execv(tie, [tie, "notes"])

def winsize(rows, cols):
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))

out = b""
def pump(t=0.4):
    global out
    end = time.time() + t
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.05)
        if not r:
            continue
        try:
            d = os.read(fd, 65536)
        except OSError:
            return
        out += d
        if b"\x1b[6n" in d:
            os.write(fd, b"\x1b[1;9R")  # a tab is 8 columns wide

def send(b, t=0.4):
    global out
    out = b""
    os.write(fd, b)
    pump(t)

failed = 0
def check(what, ok):
    global failed
    print(("ok   " if ok else "FAIL ") + what)
    failed += not ok

winsize(24, 80)
pump(1.0)
check("alternate screen and mouse on", b"\x1b[?1049h" in out and b"\x1b[?1006h" in out)
check("buffer line", b" 1 notes " in out)
send(b"l")  # the first key replaces the message with the tag
check("tag on the bottom line", b"Write Get Diff Undo" in out)
send(b"\x1b[<1;2;2M\x1b[<1;2;2m", 0.8)  # button 2 on "ls", row 2
check("button 2 executes into +Errors", b"other.txt" in out and b"+Errors" in out)
send(b"\x1b[<2;3;3M\x1b[<2;3;3m", 0.8)  # button 3 on "other.txt", row 3
check("button 3 opens the file", b"alpha" in out)
out = b""
open("other.txt", "w").write("alpha\nbeta\ngamma\n")  # changed while tie waits
later = time.time() + 60
os.utime("other.txt", (later, later))
pump(1.6)
check("a file changed on disk is read again while idle", b"gamma" in out)
winsize(30, 100)
os.kill(pid, signal.SIGWINCH)
pump(0.6)
check("redraw after resize", b"\x1b[30;1H" in out)
send(b"ihello\x1b\x1b[D")
check("insert and arrow keys", b"hello" in out)
send(b":w\r")
check("write", open("other.txt").read() == "helloalpha\nbeta\ngamma\n")
send(b"Q", 0.8)
check("mouse off and normal screen at exit", b"\x1b[?1006l" in out and b"\x1b[?1049l" in out)
_, status = os.waitpid(pid, 0)
check("exit status 0", status == 0)
sys.exit(1 if failed else 0)
