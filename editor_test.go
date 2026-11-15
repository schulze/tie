package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func testEditor(text string) *Editor {
	t, in := headlessTerm(80, 24)
	e := newEditor(t, in)
	f := newFile("")
	f.t = Text{r: []rune(text)}
	e.files, e.f = []*File{f}, f
	return e
}

func (e *Editor) feed(keys string) {
	var k []Key
	for _, r := range keys {
		k = append(k, Key(r))
	}
	e.in.push(k)
	e.quit = false
	e.run()
}

const esc = "\x1b"

var editTests = []struct {
	name, text, keys, want string
	cur                    int // -1: don't check
}{
	{"x", "abc\n", "x", "bc\n", 0},
	{"count x", "abcdef\n", "3x", "def\n", 0},
	{"x at end", "abc\n", "$x", "ab\n", 1},
	{"X", "abc\n", "$ch", "ac\n", 1},
	{"cw", "one two three\n", "cw", "two three\n", 0},
	{"cw last worc", "one two\nnext\n", "wcw", "one \nnext\n", 3},
	{"c2w", "one two three\n", "c2w", "three\n", 0},
	{"2cw", "one two three\n", "2cw", "three\n", 0},
	{"cc", "a\nb\nc\n", "jcc", "a\nc\n", 2},
	{"cc last", "a\nb\n", "jcc", "a\n", 0},
	{"3cc", "a\nb\nc\nd\n", "3cc", "d\n", 0},
	{"cj", "a\nb\nc\n", "cj", "c\n", 0},
	{"cG", "a\nb\nc\n", "jcG", "a\n", 0},
	{"c1G", "a\nb\nc\n", "jc1G", "c\n", 0},
	{"c$", "abc def\n", "wc$", "abc \n", 3},
	{"c$ alias", "abc def\n", "wc$", "abc \n", 3},
	{"c0", "abc def\n", "wc0", "def\n", 0},
	{"cf", "abc, def\n", "cf,", " def\n", 0},
	{"c;", "a,b,c\n", "f,c;", "ac\n", 1},
	{"c%", "f(a(b)c) x\n", "f(c%", "f x\n", 1},
	{"c}", "a\nb\n\nc\n", "c}", "\nc\n", 0},
	{"A", "abc\n", "Axy" + esc, "abcxy\n", 4},
	{"i", "abc\n", "lix" + esc, "axbc\n", 1},
	{"a", "abc\n", "ax" + esc, "axbc\n", 1},
	{"$a", "abc\n", "$axy" + esc, "abcxy\n", 4},
	{"I", "  abc\n", "$Ix" + esc, "  xabc\n", 2},
	{"o", "a\nb\n", "ox" + esc, "a\nx\nb\n", 2},
	{"O", "a\nb\n", "jOx" + esc, "a\nx\nb\n", 2},
	{"o empty", "", "ox" + esc, "\nx\n", 1},
	{"3i", "\n", "3iab" + esc, "ababab\n", 5},
	{"3o", "a\n", "3ox" + esc, "a\nx\nx\nx\n", 6},
	{"insert newline", "abcd\n", "lli\r" + esc, "ab\ncd\n", 3},
	{"backspace", "abc\n", "$axy\x7f\x7fz" + esc, "abcz\n", 3},
	{"ctrl-w", "abc\n", "$a def\x17" + esc, "abc \n", 3},
	{"r", "abc\n", "rx", "xbc\n", 0},
	{"3r", "abcd\n", "3rx", "xxxd\n", 2},
	{"r enter", "abcd\n", "lr\r", "a\ncd\n", 2},
	{"J", "a\n  b\nc\n", "J", "a b\nc\n", 1},
	{"3J", "a\nb\nc\nd\n", "3J", "a b c\nd\n", 3},
	{"yyp", "a\nb\n", "yyp", "a\na\nb\n", 2},
	{"yyP", "a\nb\n", "jyyP", "a\nb\nb\n", 2},
	{"ywP", "one two\n", "ywP", "one one two\n", 3},
	{"xp", "ab\n", "xp", "ba\n", 1},
	{"ccp", "a\nb\n", "ccp", "b\na\n", 2},
	{"register", "a\nb\n", "\"ayyj\"ap", "a\nb\na\n", 4},
	{"append register", "a\nb\n", "\"ayyj\"Ayy\"aP", "a\na\nb\nb\n", 2},
	{"3p", "a\n", "x3p", "aaa\n", 2},
	{"u", "abc\n", "xxu", "bc\n", 0},
	{"uu", "abc\n", "xxuu", "abc\n", 0},
	{"redo", "abc\n", "xxuu\x12", "bc\n", 0},
	{"undo insert", "abc\n", "ixyz" + esc + "u", "abc\n", 0},
	{"period x", "abcdef\n", "x..", "def\n", 0},
	{"period dw", "a b c d\n", "cw.", "c d\n", 0},
	{"period count", "abcdefgh\n", "2x3.", "fgh\n", 0},
	{"period insert", "ab\n", "ix" + esc + "l.", "xxab\n", 1},
	{"period o", "a\n", "ob" + esc + ".", "a\nb\nb\n", 4},
	{"period after undo", "abc\n", "xu.", "bc\n", 0},
	{">>", "a\nb\n", ">>", "\ta\nb\n", 1},
	{">j", "a\nb\nc\n", ">j", "\ta\n\tb\nc\n", 1},
	{"<<", "\t\ta\n", "<<", "\ta\n", 1},
	{"> with sw", "a\n", ":set sw=4\r>>", "    a\n", 4},
	{"j k column", "abcdef\nab\nabcdef\n", "4ljj", "abcdef\nab\nabcdef\n", 14},
	{"$ j", "abc\nabcdef\n", "$j", "abc\nabcdef\n", 9},
	{"w across lines", "a\nb\n", "w", "a\nb\n", 2},
	{"w empty line", "a\n\nb\n", "w", "a\n\nb\n", 2},
	{"b", "one two\n", "$b", "one two\n", 4},
	{"e", "one two\n", "e", "one two\n", 2},
	{"W", "a.b c\n", "W", "a.b c\n", 4},
	{"G", "a\nb\nc\n", "G", "a\nb\nc\n", 4},
	{"2G", "a\nb\nc\n", "2G", "a\nb\nc\n", 2},
	{"1G", "a\nb\nc\n", "G1G", "a\nb\nc\n", 0},
	{"0 ^", "  ab\n", "$0", "  ab\n", 0},
	{"^", "  ab\n", "$^", "  ab\n", 2},
	{"f", "abcabc\n", "fc", "abcabc\n", 2},
	{"2f", "abcabc\n", "2fc", "abcabc\n", 5},
	{"; ,", "abcabc\n", "fc;,", "abcabc\n", 2},
	{"F", "abcabc\n", "$Fa", "abcabc\n", 3},
	{"%", "(a)\n", "%", "(a)\n", 2},
	{"marks", "a\nb\nc\n", "jmaG'ax", "a\n\nc\n", 2},
	{"mark d'", "a\nb\nc\n", "majjc'a", "", 0},
	{"search", "foo bar foo\n", "/foo\r", "foo bar foo\n", 8},
	{"search wraps", "foo bar foo\n", "$/foo\r", "foo bar foo\n", 0},
	{"n", "x a a a\n", "/a\rn", "x a a a\n", 4},
	{"N", "x a a a\n", "$/a\rN", "x a a a\n", 6},
	{"c/", "abc def ghi\n", "c/gh\r", "ghi\n", 0},
	{"*", "foo bar foo\n", "*", "foo bar foo\n", 8},
	{"#", "foo bar foo\n", "$#", "foo bar foo\n", 0},
	{"search regexp", "a1 b22 c\n", "/[0-9]+\rx", "a b22 c\n", 1},

	// ex commands
	{":d", "a\nb\nc\n", ":2cut\r", "a\nc\n", 2},
	{":range d", "a\nb\nc\nd\n", ":2,3cut\r", "a\nd\n", 2},
	{":,d", "a\nb\n", ":,cut\r", "", 0},
	{":.,$d", "a\nb\nc\n", "j:.,$cut\r", "a\n", 0},
	{":+d", "a\nb\nc\n", ":+cut\r", "a\nc\n", 2},
	{":'a,.d", "a\nb\nc\nd\n", "jmaj:'a,.cut\r", "a\nd\n", 2},
	{":N", "a\nb\nc\n", ":3\r", "a\nb\nc\n", 4},
	{":$", "a\nb\nc\n", ":$\r", "a\nb\nc\n", 4},
	{":e s", "aaa\n", ":e s/a/b/\r", "baa\n", 0},
	{":e s g", "aaa\n", ":e s/a/b/g\r", "bbb\n", 0},
	{":e ,s", "ab\nab\n", ":e ,s/b/c/g\r", "ac\nac\n", -1},
	{":e ,s first only", "ab\nab\n", ":e ,s/b/c/\r", "ac\nab\n", -1},
	{":, e s", "ab\nab\n", ":,e s/b/c/g\r", "ac\nac\n", -1},
	{":e s &", "ab\n", ":e s/b/[&]/\r", "a[b]\n", 0},
	{":e s group", "ab cd\n", `:e s/(\w+) (\w+)/\2 \1/` + "\r", "cd ab\n", 0},
	{":e s newline", "a,b\n", `:e s/,/\n/` + "\r", "a\nb\n", -1},
	{":e s other delim", "a/b\n", ":e s#/#-#\r", "a-b\n", 0},
	{":e x s first on each line", "aa\naa\n", ":e ,x/.*\\n/s/a/b/\r", "ba\nba\n", -1},
	{":e 2,3s", "a\na\na\n", ":e 2,3s/a/b/g\r", "a\nb\nb\n", -1},
	{":e .,$s", "a\na\na\n", "j:e .,$s/a/b/g\r", "a\nb\nb\n", -1},
	{":e s range", "a\na\na\n", ":2,3e s/a/b/g\r", "a\nb\nb\n", -1},
	{":set ic", "Foo\n", ":set ic\r/foo\rx", "oo\n", 0},
	{":y", "a\nb\n", ":1yank\rjp", "a\nb\na\n", 4},
	{":/re/ dot", "foo bar baz\n", ":/ba./\rx", "foo ar baz\n", 4},

	// visual mode and dot
	{"vc", "abcdef\n", "vllc", "def\n", 0},
	{"v backwarcs", "abcdef\n", "$vhhc", "abc\n", 2},
	{"vo", "abcdef\n", "lvlohc", "def\n", 0},
	{"Vc", "a\nb\nc\n", "Vjc", "c\n", 0},
	{"vy p", "abc\n", "vly$p", "abcab\n", 4},
	{"Vy P", "a\nb\n", "VyjP", "a\na\nb\n", 2},
	{"vJ", "a\nb\nc\n", "VjjJ", "a b c\n", 3},
	{"vr", "abc\nd\n", "vjrx", "xxx\nx\n", 0},
	{"vp", "abc def\n", "yewvep", "abc abc\n", 4},
	{"v>", "a\nb\n", "Vj>", "\ta\n\tb\n", 1},
	{"v<", "\ta\n\tb\n", "Vj<", "a\nb\n", 0},
	{"V:|", "b\na\nc\n", "Vj:|sort\r", "a\nb\nc\n", 0},
	{"v:<", "abc\n", "vl:<echo X\r", "Xc\n", 0},
	{"< empty dot", "ab\n", "l:<echo X\r", "aXb\n", 1},
	{"v: s on dot", "aaa aaa\n", "vee:e s/a/b/g\r", "bbb bbb\n", 0},
	{"v: s charwise", "aaa aaa\n", "ve:e s/a/b/g\r", "bbb aaa\n", 0},
	{"search dot s c", "foo bar foo\n", "/bar\rsc", "foo  foo\n", 4},
	{"search dot period", "x a y a\n", "/a\rscn.", "x  y \n", -1},
	{"search dot pipe", "abc def\n", "/def\r:|tr a-z A-Z\r", "abc DEF\n", 4},
	{"period pipe", "b\na\n\nd\nc\n", "Vj:|sort\r}j.", "a\nb\n\nc\nd\n", 5},

	// acme pipes on addresses
	{":2,|sort", "x\nb\na\n", ":2,|sort\r", "x\na\nb\n", 2},
	{":,|sort", "b\na\n", ":,|sort\r", "a\nb\n", 0},
	{":<", "a\nb\n", ":2<echo x\r", "a\nx\n", 2},
	{"% is ,", "a\n", ":%d\r", "a\n", 0},
	{"no filter with :!", "b\na\n", ":,!sort\r", "b\na\n", 0},
	{":.|", "a\nb\n", "j:.|echo x\r", "a\nx\n", 2},
	{":.,|sort", "x\nc\nb\n", "j:.,|sort\r", "x\nb\nc\n", 2},
	{":| inserts", "ab\n", "l:|echo x\r", "axb\n", 1},
	{"pipe failure keeps text", "a\n", ":,|exit 1\r", "a\n", 0},

	// macros
	{"@", "a\nb\nc\n", "\"qyyjjcc:1\r" + `"qp1Gi` + "x" + esc, "xa\na\nb\n", 0},
}

func TestEdits(t *testing.T) {
	for _, tt := range editTests {
		t.Run(tt.name, func(t *testing.T) {
			e := testEditor(tt.text)
			e.feed(tt.keys)
			if got := string(e.f.t.r); got != tt.want {
				t.Errorf("keys %q on %q:\ngot  %q\nwant %q (msg %q)", tt.keys, tt.text, got, tt.want, e.msg)
			}
			if tt.cur >= 0 && e.f.cur != tt.cur {
				t.Errorf("keys %q on %q: cursor %d, want %d", tt.keys, tt.text, e.f.cur, tt.cur)
			}
			if e.mode != Normal {
				t.Errorf("mode %d after %q", e.mode, tt.keys)
			}
		})
	}
}

func TestMacro(t *testing.T) {
	// Put a command into register a, then execute it with @a.
	e := testEditor("a\nb\nc\n")
	e.regs['a'] = &Reg{text: []rune("cc")}
	e.feed("@a@@")
	if got := string(e.f.t.r); got != "c\n" {
		t.Errorf("got %q", got)
	}
}

func TestDotAfterSearch(t *testing.T) {
	e := testEditor("one two three\n")
	e.feed("/t.o\r")
	if d := e.f.dot; d != (Range{4, 7}) {
		t.Errorf("dot %v, want {4 7}", d)
	}
	e.feed("l")
	if !e.f.dot.Empty() {
		t.Errorf("motion did not collapse dot: %v", e.f.dot)
	}
}

// y on dot leaves visual mode and keeps dot, to do more with it.
func TestDotAfterYank(t *testing.T) {
	e := testEditor("one two\n")
	e.feed("vey")
	if e.f.dot != (Range{0, 3}) || e.mode != Normal || string(e.regs['"'].text) != "one" {
		t.Errorf("dot %v, mode %d, register %q", e.f.dot, e.mode, string(e.regs['"'].text))
	}
	e.feed(":x Yank\r") // dot is still there for the next command
	if e.msg != "" || e.f.dot != (Range{0, 3}) {
		t.Errorf("Yank after y: dot %v, msg %q", e.f.dot, e.msg)
	}
}

func TestUndoGroups(t *testing.T) {
	e := testEditor("abc\n")
	e.feed("ixy" + esc + "x")
	e.feed("u")
	if got := string(e.f.t.r); got != "xyabc\n" {
		t.Fatalf("after one undo: %q", got)
	}
	e.feed("u")
	if got := string(e.f.t.r); got != "abc\n" {
		t.Fatalf("after two undos: %q", got)
	}
	if e.f.dirty() {
		t.Errorf("dirty after undoing everything")
	}
}

func TestWriteQuit(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "f.txt")
	e := testEditor("")
	e.open(name)
	e.feed("ihello" + esc + ":q\r")
	if e.quit {
		t.Fatal(":q quit with unsaved changes")
	}
	e.feed(":wq\r")
	if !e.quit {
		t.Fatal(":wq did not quit")
	}
	b, err := os.ReadFile(name)
	if err != nil || string(b) != "hello\n" {
		t.Fatalf("file %q, %v", b, err)
	}
}

func TestEditFiles(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	os.WriteFile(a, []byte("A\n"), 0666)
	os.WriteFile(b, []byte("B\n"), 0666)
	e := testEditor("")
	e.open(a)
	e.feed(":l " + b + "\r")
	if e.f.name != b || len(e.files) != 2 {
		t.Fatalf("now on %q with %d files", e.f.name, len(e.files))
	}
	e.feed("\x1e")
	if e.f.name != a {
		t.Fatalf("^^ went to %q", e.f.name)
	}
}

func TestRawBytes(t *testing.T) {
	in := []byte("a\xffb\n")
	if out := encode(decode(in)); string(out) != string(in) {
		t.Errorf("round trip: %q", out)
	}
}

func TestRedrawDoesNotPanic(t *testing.T) {
	e := testEditor("a\tb\x01c\n" + string(make([]rune, 300)) + "\n世界\n")
	e.opt.nu, e.opt.list = true, true
	e.feed("jjj\x04\x15\x06\x02\x05\x19zz:set nonu nolist\rG")
}

var docTests = []struct {
	name, text, keys, want string
}{
	{">%", "{\na\n}\n", ">%", "\t{\n\ta\n\t}\n"},
	{"V}:|", "b\na\n\nc\n", "V}:|sort\r", "\na\nb\nc\n"},
	{":'a,'by", "a\nb\nc\n", "majmb:'a,'byank\rGp", "a\nb\nc\na\nb\n"},
	{":.,+1|", "b\na\nc\n", ":.,+1|sort\r", "a\nb\nc\n"},
	{"macro ^V ESC", "x\ny\n", "o$a!\x16\x1b\x1b0\"ay$cc1G@aj@@", "x!\ny!\n"},
	{"execute :cmd", "a\n:1cut\n", "jVz", ":1cut\n"},
	{"execute ex on dot", "aaa\n:e ,s/a/b/g\n", "jVz", "bbb\n:e ,s/b/b/g\n"},
	{"5i", "\n", "5i-\x1b", "-----\n"},
	{"cwwP", "one two three\n", "cwwP", "two one three\n"},
}

func TestDocExamples(t *testing.T) {
	for _, tt := range docTests {
		t.Run(tt.name, func(t *testing.T) {
			e := testEditor(tt.text)
			e.feed(tt.keys)
			if got := string(e.f.t.r); got != tt.want {
				t.Errorf("keys %q on %q:\ngot  %q\nwant %q (msg %q)", tt.keys, tt.text, got, tt.want, e.msg)
			}
		})
	}
}

func plumbDir(t *testing.T) string {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0666)
	os.Mkdir(filepath.Join(dir, "sub"), 0777)
	os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("bee\n"), 0666)
	return dir
}

func editorIn(dir, name, text string) *Editor {
	e := testEditor(text)
	e.f.name = filepath.Join(dir, name)
	return e
}

func TestPlumb(t *testing.T) {
	dir := plumbDir(t)
	a := filepath.Join(dir, "a.txt")
	tests := []struct {
		name, text, keys, file string
		line, col              int
	}{
		{"look", "see a.txt here\n", "wwt", a, 0, 0},
		{"gf on address", "a.txt:2: error\n", "t", a, 1, 0},
		{"gf line:col", "x a.txt:3:4\n", "$t", a, 2, 3},
		{"gf regexp address", "a.txt:/hre/\n", "v$t", a, 2, 1},
		{"v gf", "[a.txt]\n", "lvllllt", a, 0, 0},
		{"v t takes dot as is", "a.txt\n", "vllt", filepath.Join(dir, "notes"), 0, 0},
		{"directory", "sub\n", "tt", filepath.Join(dir, "sub", "b.txt"), 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := editorIn(dir, "notes", tt.text)
			e.feed(tt.keys)
			f := e.f
			if f.name != tt.file {
				t.Fatalf("on %q (msg %q), want %q", f.name, e.msg, tt.file)
			}
			if ln, col := f.line(f.cur), f.cur-f.bol(f.cur); ln != tt.line || col != tt.col {
				t.Errorf("at %d:%d, want %d:%d", ln, col, tt.line, tt.col)
			}
		})
	}
}

func TestExecuteLs(t *testing.T) {
	dir := plumbDir(t)
	e := editorIn(dir, "notes", "ls\n")
	e.feed("Vz")
	if want := filepath.Join(dir, "+Errors"); e.f.name != want {
		t.Fatalf("on %q, want %q (msg %q)", e.f.name, want, e.msg)
	}
	if got := string(e.f.t.r); got != "a.txt\nnotes\nsub\n" && got != "a.txt\nsub\n" {
		t.Fatalf("+Errors holds %q", got)
	}
	e.feed("t")
	if e.f.name != filepath.Join(dir, "a.txt") {
		t.Fatalf("look in +Errors opened %q", e.f.name)
	}
	e.feed("Q")
	if !e.quit {
		t.Errorf("+Errors kept Q from quitting: %s", e.msg)
	}
}

func TestSendToErrors(t *testing.T) {
	dir := plumbDir(t)
	e := editorIn(dir, "notes", "a b c\n")
	e.feed("V:>wc -w\r")
	if got := strings.TrimSpace(string(e.f.t.r)); got != "3" {
		t.Errorf("+Errors holds %q", got)
	}
}

func TestBuffers(t *testing.T) {
	dir := plumbDir(t)
	e := testEditor("")
	for _, name := range []string{"a.txt", "sub", "sub/b.txt"} {
		e.open(filepath.Join(dir, name))
	}
	for _, tt := range []struct {
		keys string
		want int
	}{
		{"\t1", 0},     // a number goes there
		{"\t3", 2},     //
		{"\thh\r", 0},  // h and l move, RETURN goes
		{"\tl\x1b", 0}, // ESC stays
		{"\tl\t", 0},   // so does the key again
		{"\t9", 0},     // no such buffer: nothing happens
	} {
		e.feed(tt.keys)
		if e.f != e.files[tt.want] || e.choosing != nil {
			t.Errorf("%q: on %q, want %q", tt.keys, e.f.name, e.files[tt.want].name)
		}
	}
	e.feed("\t2")
	if e.f != e.files[1] {
		t.Fatalf("\\t2: on %q", e.f.name)
	}
	e.feed("\x1e")
	if e.f != e.files[0] {
		t.Fatalf("^^ went to %q, not back to a.txt", e.f.name)
	}
}

func TestBuffersMany(t *testing.T) {
	e := testEditor("")
	dir := t.TempDir()
	for i := 1; i <= 12; i++ {
		e.open(filepath.Join(dir, fmt.Sprint(i)))
	}
	e.feed("\t12")
	if e.f != e.files[11] {
		t.Errorf("12: on %q", e.f.name)
	}
	e.feed("\t1\r")
	if e.f != e.files[0] {
		t.Errorf("1 RETURN: on %q", e.f.name)
	}
	e.feed("\t5")
	if e.f != e.files[4] {
		t.Errorf("5: on %q", e.f.name)
	}
}

func TestQuit(t *testing.T) {
	dir := plumbDir(t)
	e := testEditor("")
	e.open(filepath.Join(dir, "a.txt"))
	e.open(filepath.Join(dir, "sub", "b.txt"))
	e.feed("d")
	if e.quit || len(e.files) != 1 || e.f.name != filepath.Join(dir, "a.txt") {
		t.Fatalf("d: quit %v, %d files, on %q", e.quit, len(e.files), e.f.name)
	}
	e.feed("xd")
	if e.quit {
		t.Fatalf("d quit with a modified buffer")
	}
	e.feed(":q!\r")
	if !e.quit {
		t.Fatalf(":q! on the last buffer did not quit: %s", e.msg)
	}
	e = testEditor("")
	e.open(filepath.Join(dir, "a.txt"))
	e.open(filepath.Join(dir, "sub", "b.txt"))
	e.feed("Q")
	if !e.quit {
		t.Fatalf("Q did not quit: %s", e.msg)
	}
}

func TestBufLine(t *testing.T) {
	dir := plumbDir(t)
	e := testEditor("")
	e.open(filepath.Join(dir, "a.txt"))
	e.open(filepath.Join(dir, "sub"))
	e.feed("")
	r := e.bufLine()
	if !strings.Contains(r.s, " 1 a.txt ") || !strings.Contains(r.s, sgrReverse+" 2 sub/ ") {
		t.Errorf("buffer line %q", r.s)
	}
	e.feedBytes("\x1b[<0;3;1M\x1b[<0;3;1m") // click on " 1 a.txt "
	if e.f.name != filepath.Join(dir, "a.txt") {
		t.Errorf("click went to %q", e.f.name)
	}
}

func TestWritePromptsForName(t *testing.T) {
	name := filepath.Join(t.TempDir(), "new.txt")
	e := testEditor("")
	e.feed("ihi\x1b:w\r" + name + "\r")
	if b, err := os.ReadFile(name); err != nil || string(b) != "hi\n" {
		t.Fatalf("got %q, %v (msg %q)", b, err, e.msg)
	}
}

func TestCompletion(t *testing.T) {
	dir := plumbDir(t)
	var c completion
	got := string(c.next([]rune("e " + dir + "/s")))
	if got != "e "+dir+"/sub/" {
		t.Fatalf("got %q", got)
	}
	c = completion{}
	os.WriteFile(filepath.Join(dir, "a2.txt"), nil, 0666)
	first := string(c.next([]rune("e " + dir + "/a")))
	second := string(c.next([]rune(first)))
	if first != "e "+dir+"/a" && first != "e "+dir+"/a.txt" {
		t.Fatalf("first %q", first)
	}
	if second == first {
		t.Fatalf("TAB did not cycle: %q", second)
	}
}

var buttonTests = []struct {
	name, text, keys, want string
	dot                    Range // checked unless zero
}{
	{"select word", "one two\n", "wsc", "one \n", Range{}},
	{"select grows to line", "ab cd\nx\n", "ssc", "x\n", Range{}},
	{"select grows to paragraph", "a\nb\n\nc\n", "sssc", "\nc\n", Range{}},
	{"select then motion", "one two three\n", "swc", "wo three\n", Range{}},
	{"select brackets", "f(abc)\n", "lsc", "f()\n", Range{}},
	{"select quotes", `say "hi there"` + "\n", "fhhsc", `say ""` + "\n", Range{}},
	{"select keeps search dot", "a foo b\n", "/foo\rsc", "a  b\n", Range{}},
	{"select then change, period", "x a y a\n", "/a\rscn.", "x  y \n", Range{}},
	{"execute builtin word", "abc Undo\n", "x$z", "abc Undo\n", Range{}},
	{"look searches", "foo bar foo\n", "t", "foo bar foo\n", Range{8, 11}},
	{"look on dot", "ab x ab\n", "vlt", "ab x ab\n", Range{5, 7}},
	{":s address", "a\nb\nc\n", ":2s\r", "a\nb\nc\n", Range{2, 4}},
	{":s word", "one two\n", "w:s\r", "one two\n", Range{4, 7}},
	{":l text", "a b c b\n", ":l b\r", "a b c b\n", Range{2, 3}},
	{":x Cut", "one two\n", "ve:x Cut\r", " two\n", Range{}},
	{":x Yank Paste", "one two\n", "vey$:s\r:x Paste\r", "one one\n", Range{}},
	{":x Cut needs dot", "one\n", ":x Cut\r", "one\n", Range{}},
	{"Edit s", "a a\na\n", ":edit ,s/a/b/g\r", "b b\nb\n", Range{}},
	{"Edit s first only", "a a\na\n", ":e ,s/a/b/\r", "b a\na\n", Range{}},
	{"Edit x c", "aa b aaa\n", ":e ,x/a+/c/X/\r", "X b X\n", Range{}},
	{"Edit y", "ab cd\n", ":e ,y/ /c/_/\r", "_ _\n", Range{}},
	{"Edit x g d", "a\nb\nc\n", ":e ,x/.*\\n/g/b/d\r", "a\nc\n", Range{}},
	{"Edit x v d", "a\nb\nc\n", ":e ,x/.*\\n/v/b/d\r", "b\n", Range{}},
	{"Edit x i", "a\nb\n", ":e ,x/.*\\n/i/> /\r", "> a\n> b\n", Range{}},
	{"Edit a newline", "a\n", ":e ,x/a/a/\\nb/\r", "a\nb\n", Range{}},
	{"Edit on dot", "a b a\n", "/b\r:e c/X/\r", "a X a\n", Range{2, 3}},
	{"Edit nested", "foo(a,b) bar(a)\n", ":e ,x/foo\\([^)]*\\)/x/a/c/z/\r", "foo(z,b) bar(a)\n", Range{}},
	{"Edit pipe", "b\na\n", ":e ,|sort\r", "a\nb\n", Range{}},
	{"Edit address alone", "a\nb\n", ":e 2\r", "a\nb\n", Range{2, 4}},
}

func TestButtons(t *testing.T) {
	for _, tt := range buttonTests {
		t.Run(tt.name, func(t *testing.T) {
			e := testEditor(tt.text)
			e.feed(tt.keys)
			if got := string(e.f.t.r); got != tt.want {
				t.Errorf("keys %q on %q:\ngot  %q\nwant %q (msg %q)", tt.keys, tt.text, got, tt.want, e.msg)
			}
			if tt.dot != (Range{}) && e.f.dot != tt.dot {
				t.Errorf("keys %q: dot %v, want %v (msg %q)", tt.keys, e.f.dot, tt.dot, e.msg)
			}
		})
	}
}

func TestExecuteButton(t *testing.T) {
	dir := plumbDir(t)
	e := editorIn(dir, "notes", "echo hi\n")
	e.feed("Vz")
	if got := string(e.f.t.r); got != "hi\n" || filepath.Base(e.f.name) != "+Errors" {
		t.Fatalf("on %q: %q", e.f.name, got)
	}
	e = editorIn(dir, "notes", "\n")
	e.feed(":x echo hi\r")
	if got := string(e.f.t.r); got != "hi\n" {
		t.Fatalf(":x got %q", got)
	}
}

func TestLookOpens(t *testing.T) {
	dir := plumbDir(t)
	e := editorIn(dir, "notes", "see a.txt:2\n")
	e.feed("wt")
	if e.f.name != filepath.Join(dir, "a.txt") || e.f.line(e.f.cur) != 1 {
		t.Fatalf("on %q line %d (msg %q)", e.f.name, e.f.line(e.f.cur), e.msg)
	}
}

func TestTrimWord(t *testing.T) {
	for in, want := range map[string]string{
		"ls(1).":                 "ls(1)",
		"(https://x.org/a_(b)).": "https://x.org/a_(b)",
		`"quoted",`:              "quoted",
		"<https://go.dev/doc/>":  "https://go.dev/doc/",
		"[link](https://a.b/c)":  "link](https://a.b/c)",
		"plain":                  "plain",
	} {
		if got := trimWord(in); got != want {
			t.Errorf("trimWord(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLookRules(t *testing.T) {
	saved := lookRules
	defer func() { lookRules = saved }()
	var got []string
	lookRules = []lookRule{{pattern: `^issue#([0-9]+)$`, fn: func(e *Editor, m []string) error {
		got = m
		return nil
	}}}
	e := testEditor("see issue#42 now\n")
	e.feed("wt")
	if len(got) != 2 || got[1] != "42" {
		t.Fatalf("rule got %q", got)
	}
}

// mouse makes the SGR report of a mouse event at column x of text row
// y (from 1); above the text is the buffer line.
func mouse(b, x, y int, up bool) string {
	y++
	end := "M"
	if up {
		end = "m"
	}
	return fmt.Sprintf("\x1b[<%d;%d;%d%s", b, x, y, end)
}

func (e *Editor) feedBytes(s string) {
	e.in.buf = append(e.in.buf, s...)
	e.in.eof = false
	e.quit = false
	e.run()
}

func TestMouse(t *testing.T) {
	e := testEditor("one two three\nfour\n")
	e.feedBytes(mouse(0, 6, 1, false) + mouse(0, 6, 1, true))
	if e.f.cur != 5 || !e.f.dot.Empty() {
		t.Errorf("click: cur %d dot %v", e.f.cur, e.f.dot)
	}
	e.feedBytes(mouse(0, 6, 1, false) + mouse(0, 6, 1, true))
	if e.f.dot != (Range{4, 7}) {
		t.Errorf("double click: dot %v", e.f.dot)
	}
	e.click.at = time.Time{}
	e.feedBytes(mouse(0, 1, 1, false) + mouse(0, 1, 1, true) + mouse(0, 1, 1, false) + mouse(0, 1, 1, true))
	if e.f.dot != (Range{0, 14}) {
		t.Errorf("double click at line start: dot %v", e.f.dot)
	}
	e.click.at = time.Time{}
	e.feedBytes(mouse(0, 5, 1, false) + mouse(32, 7, 1, false) + mouse(0, 7, 1, true))
	if e.f.dot != (Range{4, 7}) {
		t.Errorf("sweep: dot %v", e.f.dot)
	}
	e.click.at = time.Time{}
	e.feedBytes(mouse(0, 5, 1, false) + mouse(32, 7, 1, false) + mouse(1, 7, 1, false) + mouse(1, 7, 1, true) + mouse(0, 7, 1, true))
	if got := string(e.f.t.r); got != "one  three\nfour\n" {
		t.Errorf("cut chord: %q", got)
	}
	e.feedBytes(mouse(0, 1, 2, false) + mouse(32, 4, 2, false) + mouse(2, 4, 2, false) + mouse(2, 4, 2, true) + mouse(0, 4, 2, true))
	if got := string(e.f.t.r); got != "one  three\ntwo\n" {
		t.Errorf("paste chord: %q", got)
	}
	e.feedBytes(mouse(2, 7, 1, false) + mouse(2, 7, 1, true))
	if e.f.dot != (Range{5, 10}) {
		t.Errorf("button 3 look: dot %v (msg %q)", e.f.dot, e.msg)
	}
	e.feedBytes(mouse(65, 1, 1, false))
}

func TestMouseExecuteKeepsDot(t *testing.T) {
	// Button 2 on |sort pipes dot, as in acme.
	e := testEditor("|sort\nb\na\n")
	e.feed("jVj")
	e.feed("\x1b") // leave visual mode, dot dropped; select again by address
	e.feed(":2,3s\r")
	e.feedBytes(mouse(1, 2, 1, false) + mouse(1, 2, 1, true))
	if got := string(e.f.t.r); got != "|sort\na\nb\n" {
		t.Errorf("got %q (msg %q)", got, e.msg)
	}
}

func TestOpenerInRules(t *testing.T) {
	if !strings.HasPrefix(lookRules[0].cmd, opener+" ") || opener == "" {
		t.Errorf("rule %q, opener %q", lookRules[0].cmd, opener)
	}
}

func (e *Editor) feedKeys(keys ...Key) {
	e.in.push(keys)
	e.quit = false
	e.run()
}

// lines makes a text of n numbered lines.
func lines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// TestMoves checks where motions and scrolling put the cursor, as a
// line and column (both from 0), on a 24-row screen: 22 text rows.
func TestMoves(t *testing.T) {
	long := lines(100)
	tests := []struct {
		name, text, keys string
		line, col        int
	}{
		{"k", "ab\ncd\n", "jlk", 0, 1},
		{"k keeps column", "abcd\nx\nabcd\n", "jj$kk", 0, 3},
		{"{", "a\n\nb\nc\n", "G{", 1, 0},
		{"} {", "a\nb\n\nc\n\nd\n", "}}{", 2, 0},
		{"b over empty line", "a\n\nb\n", "Gbb", 0, 0},
		{"H", long, "50GH", 39, 0},
		{"L", long, "50GL", 60, 0},
		{"M", long, "50GM", 49, 0},
		{"3H", long, "H3H", 2, 0},
		{"^D", long, "\x04", 11, 0},
		{"^D ^U", long, "\x04\x04\x15", 11, 0},
		{"^F", long, "\x06", 20, 0},
		{"^F ^B", long, "\x06\x06\x02", 40, 0},
		{"^B moves cursor up", long, "\x06\x06L\x02", 41, 0},
		{"^E moves cursor along", long, "5\x05", 5, 0},
		{"^Y", long, "G\x19", 99, 0},
		{"^Y keeps cursor on screen", long, "\x06L\x19", 40, 0},
		{"$ count", "ab\ncd\n", "2$", 1, 1},
		{"2G", "a\nb\nc\n", "G2G", 1, 0},
		{"''", "a\nb\nc\n", "G''", 0, 0},
		{"; F", "a.b.c.d\n", "$F.;", 0, 3},
		{"% back", "(a)\n", "$%", 0, 0},
		{"W", "a.b c.d e\n", "WW", 0, 8},
		{"B", "a.b c.d e\n", "$B", 0, 4},
		{"E", "a.b c.d\n", "E", 0, 2},
		{"#", "x foo y foo\n", "$#", 0, 2},
		{"N", "a b a b a\n", "/a\rnN", 0, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := testEditor(tt.text)
			e.feed(tt.keys)
			f := e.f
			if ln, col := f.line(f.cur), f.cur-f.bol(f.cur); ln != tt.line || col != tt.col {
				t.Errorf("keys %q: at %d:%d, want %d:%d (top %d, msg %q)", tt.keys, ln, col, tt.line, tt.col, f.top, e.msg)
			}
		})
	}
}

func TestInsertKeys(t *testing.T) {
	e := testEditor("ab\ncd\n")
	e.feedKeys('i', 'x', KeyRight, 'y', KeyDown, 'z', KeyUp, KeyEnd, '!', KeyHome, '<', KeyLeft, KeyDelete, KeyEsc)
	if got := string(e.f.t.r); got != "xayb!\ncdz\n" {
		t.Errorf("got %q", got)
	}
	e = testEditor("\n")
	e.opt.ai = true
	e.feed("i  a\rb" + esc)
	if got := string(e.f.t.r); got != "  a\n  b\n" {
		t.Errorf("autoindent: %q", got)
	}
	e = testEditor("\n")
	e.opt.et, e.opt.ts = true, 4
	e.feed("ia\tb\x16\tc" + esc)
	if got := string(e.f.t.r); got != "a   b\tc\n" {
		t.Errorf("expandtab: %q", got)
	}
	e = testEditor("one two\n")
	e.feed("A\x15x" + esc)
	if got := string(e.f.t.r); got != "x\n" {
		t.Errorf("^U: %q", got)
	}
}

func TestDecode(t *testing.T) {
	_, in := headlessTerm(80, 24)
	in.buf = []byte("\x1b[A\x1b[B\x1b[C\x1b[D\x1b[H\x1b[F\x1b[3~\x1b[5~\x1b[6~\x1b[2~\x1b[1;5A\x1bOAé€\x1bx\x1b[24;80R")
	want := []Key{KeyUp, KeyDown, KeyRight, KeyLeft, KeyHome, KeyEnd, KeyDelete, KeyPgUp, KeyPgDn, KeyInsert, KeyUp, KeyUp, 'é', '€', KeyEsc, 'x'}
	for i, w := range want {
		if k := in.Get(); k != w {
			t.Fatalf("key %d: got %d, want %d", i, k, w)
		}
	}
	if r, c, ok := in.cpr(time.Second); !ok || r != 24 || c != 80 {
		t.Errorf("cpr: %d %d %v", r, c, ok)
	}
	if in.Get() != KeyEOF {
		t.Errorf("no EOF at the end")
	}
}

func TestExCommands(t *testing.T) {
	dir := plumbDir(t)
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "sub", "b.txt")
	e := testEditor("")
	e.open(a)
	e.open(b)
	e.feed("x:x Get\r")
	if got := string(e.f.t.r); got != "bee\n" || e.f.dirty() {
		t.Fatalf("Get got %q", got)
	}
	e.feed("\x07")
	if !strings.Contains(e.msg, "b.txt") || !strings.Contains(e.msg, "line 1 of 1") {
		t.Fatalf("^G: %q", e.msg)
	}
	e.feed("1Gx:x Undo\r:x Redo\r")
	if got := string(e.f.t.r); got != "ee\n" {
		t.Fatalf("Undo, Redo got %q", got)
	}
	e.feed(":wq\r")
	if e.quit || e.f.name != a {
		t.Fatalf(":wq should close b.txt only: quit %v on %q", e.quit, e.f.name)
	}
	if got, _ := os.ReadFile(b); string(got) != "ee\n" {
		t.Fatalf(":wq wrote %q", got)
	}
	e.feed(":help\r")
	if e.f.name != "[intro]" || !e.f.scratch || !strings.Contains(string(e.f.t.r), "Appendix") {
		t.Fatalf(":help opened %q", e.f.name)
	}
	e.feed(":tutor\r")
	if !strings.Contains(string(e.f.t.r), "Lesson 1.1") {
		t.Fatalf(":tutor opened %q", e.f.name)
	}
	os.Remove(e.f.name)
	e.feed(":nonsense\r")
	if !strings.Contains(e.msg, "Not an editor command") {
		t.Fatalf("msg %q", e.msg)
	}
}

func TestSet(t *testing.T) {
	e := testEditor("")
	e.feed(":set ts=4 nolist ic\r")
	if e.opt.ts != 4 || e.opt.list || !e.opt.ic {
		t.Fatalf("options %+v", e.opt)
	}
	e.feed(":set ts sw?\r")
	if e.msg != "tabstop=4  shiftwidth=8" {
		t.Errorf("query: %q", e.msg)
	}
	e.feed(":set\r")
	if !strings.Contains(e.msg, "noautoindent") || !strings.Contains(e.msg, "ignorecase") {
		t.Errorf("all: %q", e.msg)
	}
	e.feed(":set ts=0\r")
	if e.opt.ts != 4 || !strings.Contains(e.msg, "Bad value") {
		t.Errorf("bad value: %d %q", e.opt.ts, e.msg)
	}
	e.feed(":set nonsense\r")
	if !strings.Contains(e.msg, "Unknown option") {
		t.Errorf("unknown: %q", e.msg)
	}
}

func TestPrompt(t *testing.T) {
	e := testEditor("abc\n")
	e.feed(":set ic\r:set noic\r")
	e.feedKeys(':', KeyUp, KeyUp, KeyEnter)
	if !e.opt.ic {
		t.Errorf("history did not bring back :set ic")
	}
	e.feed(":junk\x15set noic\r")
	if e.opt.ic {
		t.Errorf("^U did not clear the line")
	}
	e.feed(":set junk\x17ic\r")
	if !e.opt.ic {
		t.Errorf("^W did not erase the word")
	}
	e.feed(":\x7fx")
	if got := string(e.f.t.r); got != "bc\n" {
		t.Errorf("backspace on an empty line should cancel: %q", got)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := expandHome("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("got %q", got)
	}
	if got := expandHome("x/~"); got != "x/~" {
		t.Errorf("got %q", got)
	}
}

func TestButtonsCommandLine(t *testing.T) {
	dir := plumbDir(t)
	e := editorIn(dir, "notes", "a b c b\necho hi\n")
	e.feed(":l c\r")
	if e.f.dot != (Range{4, 5}) {
		t.Errorf(":l c: dot %v", e.f.dot)
	}
	e.feed(":2x\r")
	if got := string(e.f.t.r); got != "hi\n" {
		t.Errorf(":2x: %q", got)
	}
	e = editorIn(dir, "notes", "a.txt\n")
	e.feed(":1l\r")
	if e.f.name != filepath.Join(dir, "a.txt") {
		t.Errorf(":1l opened %q", e.f.name)
	}
}

func TestLookRuleCommand(t *testing.T) {
	saved := lookRules
	defer func() { lookRules = saved }()
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	lookRules = []lookRule{{pattern: `^x-(\w+)$`, cmd: `echo "$1 $2" > ` + out}}
	e := editorIn(dir, "notes", "see x-ray\n")
	e.feed("wt")
	for i := 0; i < 50; i++ { // the command runs without tie waiting
		if b, _ := os.ReadFile(out); string(b) == "x-ray ray\n" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, _ := os.ReadFile(out)
	t.Errorf("rule command wrote %q", b)
}

func TestShellExpand(t *testing.T) {
	e := testEditor("")
	e.f.name = "my file.txt"
	if got := e.expand(`wc % \% x`); got != `wc my file.txt % x` {
		t.Errorf("got %q", got)
	}
}

func TestWqClosesBuffer(t *testing.T) {
	dir := plumbDir(t)
	e := testEditor("")
	e.open(filepath.Join(dir, "a.txt"))
	e.open(filepath.Join(dir, "sub", "b.txt"))
	e.feed("x:wq\r")
	if e.quit || len(e.files) != 1 {
		t.Fatalf(":wq: quit %v, %d files", e.quit, len(e.files))
	}
	e.feed(":wq\r")
	if !e.quit {
		t.Fatalf(":wq on the last buffer did not quit")
	}
}

func TestTag(t *testing.T) {
	e := testEditor("abc\n")
	e.feed("g")
	if e.f.owner == nil || e.buffer().t.String(0, 3) != "abc" {
		t.Fatalf("tab did not go to the tag")
	}
	if got := string(e.f.t.r); got != defaultTag+"\n" {
		t.Fatalf("tag holds %q", got)
	}
	e.feed("\x1b")
	if e.f.owner != nil {
		t.Fatalf("ESC did not leave the tag")
	}
	e.feed("g0cwA make\x1bg")
	if e.f.owner != nil {
		t.Fatalf("second tab did not leave the tag")
	}
	if got := e.f.tag.t.String(0, 4); got != "Get " {
		t.Fatalf("editing the tag: %q", e.f.tag.t.String(0, e.f.tag.t.Len()))
	}
	if !strings.HasSuffix(string(e.f.tag.t.r), " make\n") {
		t.Fatalf("A in the tag: %q", string(e.f.tag.t.r))
	}
	if got := string(e.f.t.r); got != "abc\n" {
		t.Fatalf("tag edits reached the body: %q", got)
	}
	e.draw()
	if !strings.Contains(e.screen[len(e.screen)-1], "Get Diff Undo") {
		t.Errorf("tag row %q", e.screen[1])
	}
}

func TestTagCommands(t *testing.T) {
	// Undo in the tag undoes in the body.
	e := testEditor("abc\n")
	e.feed("xgfUz") // fU: to "Undo"
	if got := string(e.f.t.r); got != "abc\n" || e.f.owner != nil {
		t.Errorf("Undo from the tag: %q, in tag %v", got, e.f.owner != nil)
	}
	// Select in the body, add |sort to the tag, execute it there.
	e = testEditor("b\na\nc\n")
	e.feed("VjgA |sort\x1bz")
	if got := string(e.f.t.r); got != "a\nb\nc\n" {
		t.Errorf("|sort from the tag: %q (msg %q)", got, e.msg)
	}
	// Look at a word in the tag: search the body.
	e = testEditor("x foo y foo\n")
	e.feed("gA foo\x1bt")
	if e.f.owner != nil || e.f.dot != (Range{2, 5}) {
		t.Errorf("look from the tag: dot %v (msg %q)", e.f.dot, e.msg)
	}
	// Command lines work on the buffer.
	name := filepath.Join(t.TempDir(), "f")
	e = testEditor("hi\n")
	e.f.name = name
	e.feed("xg:w\r")
	if b, _ := os.ReadFile(name); string(b) != "i\n" {
		t.Errorf(":w from the tag wrote %q", b)
	}
	// Each buffer has its own tag.
	dir := plumbDir(t)
	e = testEditor("")
	e.open(filepath.Join(dir, "a.txt"))
	e.open(filepath.Join(dir, "sub", "b.txt"))
	e.feed("gA one\x1b\t1")
	if strings.Contains(string(e.f.tagFile().t.r), "one") || e.f.owner != nil {
		t.Errorf("tags are shared: %q", string(e.f.tagFile().t.r))
	}
}

func TestTagFmt(t *testing.T) {
	// The example of the introduction, section 7.8.
	e := testEditor("one\ntwo\n\nthree\n")
	e.feed("V}g$z")
	if got := string(e.f.t.r); got != "one two\n\nthree\n" {
		t.Errorf("got %q (msg %q)", got, e.msg)
	}
}

func TestExecuteOnBlank(t *testing.T) {
	e := testEditor("ls  x\n")
	e.feed("llz")
	if e.f.name != "" || len(e.files) != 1 {
		t.Errorf("X on a blank executed something: on %q", e.f.name)
	}
}

func TestTagMouse(t *testing.T) {
	e := testEditor("abc\n")
	e.feed("x")
	// The bottom row, 24, is the tag; "Undo" starts at column 16.
	e.feedBytes("\x1b[<1;17;24M\x1b[<1;17;24m")
	if got := string(e.f.t.r); got != "abc\n" {
		t.Errorf("button 2 on Undo in the tag: %q (msg %q)", got, e.msg)
	}
	e.feedBytes("\x1b[<0;3;24M\x1b[<0;3;24m")
	if e.f.owner == nil || e.f.cur != 2 {
		t.Errorf("button 1 in the tag: in tag %v, cur %d", e.f.owner != nil, e.f.cur)
	}
	e.feedBytes(mouse(0, 2, 1, false) + mouse(0, 2, 1, true))
	if e.f.owner != nil || e.f.cur != 1 {
		t.Errorf("button 1 in the body: in tag %v, cur %d", e.f.owner != nil, e.f.cur)
	}
}

func TestModifierClicks(t *testing.T) {
	// The report codes of button 1 with each modifier, by the button
	// config.go makes of it.
	code := map[int]int{optionClick: 8, controlClick: 16}
	if clickButton(0, 8) != optionClick-1 || clickButton(0, 16) != controlClick-1 || clickButton(1, 8) != 1 {
		t.Fatalf("clickButton does not follow config.go")
	}
	if c, ok := code[2]; ok { // button 2: execute
		e := testEditor("x\nUndo\n")
		e.feed("x")
		e.feedBytes(fmt.Sprintf("\x1b[<%d;2;3M\x1b[<%d;2;3m", c, c))
		if got := string(e.f.t.r); got != "x\nUndo\n" {
			t.Errorf("click (%d) on Undo: %q (msg %q)", c, got, e.msg)
		}
		// A sweep executes the text swept over.
		e = testEditor("a b\n:x Undo\n")
		e.feed("x")
		e.feedBytes(fmt.Sprintf("\x1b[<%d;1;3M\x1b[<%d;9;3M\x1b[<%d;9;3m", c, c+32, c))
		if got := string(e.f.t.r); got != "a b\n:x Undo\n" {
			t.Errorf("sweep (%d): %q (msg %q)", c, got, e.msg)
		}
	}
	if c, ok := code[3]; ok { // button 3: look
		e := testEditor("foo bar foo\n")
		e.feedBytes(fmt.Sprintf("\x1b[<%d;2;2M\x1b[<%d;2;2m", c, c))
		if e.f.dot != (Range{8, 11}) {
			t.Errorf("click (%d) look: dot %v", c, e.f.dot)
		}
	}
}

// changeOnDisk writes text to name as another program would.
func changeOnDisk(t *testing.T, name, text string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(text), 0666); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	os.Chtimes(name, later, later)
}

func TestDiskChangeClean(t *testing.T) {
	name := filepath.Join(t.TempDir(), "f")
	os.WriteFile(name, []byte("one\ntwo\nthree\n"), 0666)
	e := testEditor("")
	e.open(name)
	e.feed("gA mine\x1bgjl") // a tag to keep, and the cursor at 1:1
	changeOnDisk(t, name, "one\nTWO\nthree\nfour\n")
	e.feed("")
	if got := string(e.f.t.r); got != "one\nTWO\nthree\nfour\n" {
		t.Fatalf("not read again: %q", got)
	}
	if e.f.line(e.f.cur) != 1 || e.f.cur-e.f.bol(e.f.cur) != 1 {
		t.Errorf("cursor moved to %d", e.f.cur)
	}
	if !strings.Contains(e.msg, "changed on disk") {
		t.Errorf("msg %q", e.msg)
	}
	if !strings.HasSuffix(string(e.f.tag.t.r), " mine\n") {
		t.Errorf("the tag was lost: %q", string(e.f.tag.t.r))
	}
}

func TestDiskChangeModified(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "f")
	os.WriteFile(name, []byte("one\ntwo\n"), 0666)
	e := testEditor("")
	e.open(name)
	e.feed("x") // "ne"
	changeOnDisk(t, name, "one\nTWO\n")
	e.feed("")
	if !e.f.stale || !strings.Contains(e.msg, "Diff compares") {
		t.Fatalf("not stale: msg %q", e.msg)
	}
	if !strings.Contains(e.bufLine().s, "f+!") {
		t.Errorf("buffer line %q", e.bufLine().s)
	}
	if got := string(e.f.t.r); got != "ne\ntwo\n" {
		t.Fatalf("the buffer changed: %q", got)
	}
	e.feed(":w\r")
	if b, _ := os.ReadFile(name); string(b) != "one\nTWO\n" {
		t.Fatalf(":w overwrote the changed file")
	}
	e.feed(":diff\r")
	diff := string(e.f.t.r)
	if filepath.Base(e.f.name) != "+Errors" || !strings.Contains(diff, "-one\n") || !strings.Contains(diff, "+ne\n") ||
		!strings.Contains(diff, "-TWO\n") || !strings.Contains(diff, "f (on disk)") {
		t.Fatalf("diff in %q:\n%s", e.f.name, diff)
	}
	e.feed("\x1e:w!\r") // back, and keep the buffer
	if b, _ := os.ReadFile(name); string(b) != "ne\ntwo\n" || e.f.stale {
		t.Fatalf(":w! wrote %q, stale %v", b, e.f.stale)
	}
	e.feed(":diff\r")
	if !strings.Contains(e.msg, "no differences") {
		t.Errorf("diff after :w!: %q", e.msg)
	}
	// Get takes the file from disk.
	e.feed("x")
	changeOnDisk(t, name, "disk\n")
	e.feed("")
	e.feed("gwz") // in the tag, the second word: Get
	if got := string(e.f.t.r); got != "disk\n" || e.f.stale || e.f.dirty() {
		t.Fatalf("Get: %q, stale %v", got, e.f.stale)
	}
}

func TestDiskRemoved(t *testing.T) {
	name := filepath.Join(t.TempDir(), "f")
	os.WriteFile(name, []byte("a\n"), 0666)
	e := testEditor("")
	e.open(name)
	os.Remove(name)
	e.feed("")
	if !e.f.stale || !strings.Contains(e.msg, "removed") || string(e.f.t.r) != "a\n" {
		t.Fatalf("removed file: stale %v, msg %q, text %q", e.f.stale, e.msg, string(e.f.t.r))
	}
	e.feed(":w!\r")
	if b, _ := os.ReadFile(name); string(b) != "a\n" {
		t.Fatalf(":w! did not write it back")
	}
}

func TestDiskDirectory(t *testing.T) {
	dir := t.TempDir()
	e := testEditor("")
	e.open(dir)
	os.WriteFile(filepath.Join(dir, "new"), nil, 0666)
	later := time.Now().Add(time.Minute)
	os.Chtimes(dir, later, later)
	e.feed("")
	if got := string(e.f.t.r); got != "new\n" {
		t.Errorf("listing not read again: %q", got)
	}
}

func TestNew(t *testing.T) {
	dir := plumbDir(t)
	e := testEditor("")
	e.open(filepath.Join(dir, "a.txt"))
	e.feed(":new notes\r")
	name := filepath.Join(dir, "notes")
	if e.f.name != name || e.f.t.Len() != 0 || len(e.files) != 2 {
		t.Fatalf(":new notes: on %q, %d files", e.f.name, len(e.files))
	}
	e.feed("ihi\x1b:w\r")
	if b, _ := os.ReadFile(name); string(b) != "hi\n" {
		t.Fatalf(":w wrote %q", b)
	}
	e.feed(":new\r")
	if e.f.name != "" || len(e.files) != 3 {
		t.Fatalf(":new: on %q, %d files", e.f.name, len(e.files))
	}
	e.feed(":new " + filepath.Join(dir, "a.txt") + "\r")
	if e.f != e.files[0] {
		t.Fatalf(":new on an open file did not go there: %q", e.f.name)
	}
	e.feed(":x New other\r") // the builtin
	if e.f.name != filepath.Join(dir, "other") {
		t.Fatalf("New other: on %q", e.f.name)
	}
}

func TestSamClasses(t *testing.T) {
	for in, want := range map[string]string{
		`[^a]`:           `[^a\n]`,
		`a[^b]c[^d]`:     `a[^b\n]c[^d\n]`,
		`[a]`:            `[a]`,
		`[]a]`:           `[]a]`,
		`[^]a]`:          `[^]a\n]`,
		`\[^a]`:          `\[^a]`,
		`[^\]]`:          `[^\]\n]`,
		`[^[:alpha:]]x`:  `[^[:alpha:]\n]x`,
		`[^a`:            `[^a`,
		`(\w+) [^)]* \)`: `(\w+) [^)\n]* \)`,
	} {
		if got := samClasses(in); got != want {
			t.Errorf("samClasses(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSamMatching(t *testing.T) {
	// The longest match wins, as in sam.
	e := testEditor("ab\n")
	e.feed("/a|ab\r")
	if e.f.dot != (Range{0, 2}) {
		t.Errorf("a|ab: dot %v, want {0 2}", e.f.dot)
	}
	e = testEditor("abc\n")
	e.feed(":e s/a|ab|abc/X/\r")
	if got := string(e.f.t.r); got != "X\n" {
		t.Errorf("s with alternation: %q", got)
	}
	// A negated class stops at the end of a line.
	e = testEditor("(a\nb) (c)\n")
	e.feed(":e ,x/\\([^)]*\\)/c/X/\r")
	if got := string(e.f.t.r); got != "(a\nb) X\n" {
		t.Errorf("[^)]* crossed a line: %q", got)
	}
	// Groups in the replacement.
	e = testEditor("2026-11-15\n")
	e.feed(`:e s/([0-9]+)-([0-9]+)-([0-9]+)/\3.\2.\1 (&)/` + "\r")
	if got := string(e.f.t.r); got != "15.11.2026 (2026-11-15)\n" {
		t.Errorf("\\1 and &: %q", got)
	}
}

func TestBottomLine(t *testing.T) {
	last := func(e *Editor) string { e.draw(); return e.screen[len(e.screen)-1] }
	e := testEditor("abc\nxyz\n")
	e.feed("jl")
	if l := last(e); !strings.HasPrefix(l, "Write Get Diff") || !strings.HasSuffix(l, "2,2") {
		t.Errorf("tag and position: %q", l)
	}
	e.feed("\x07") // ^G: a message
	if l := last(e); strings.Contains(l, "Write Get") || !strings.Contains(l, "line 2 of 2") {
		t.Errorf("message: %q", l)
	}
	e.feed("l") // the next key brings the tag back
	if l := last(e); !strings.HasPrefix(l, "Write Get") {
		t.Errorf("after a key: %q", l)
	}
	e.in.push([]Key{'i'})
	e.mode = Insert
	if l := last(e); !strings.HasPrefix(l, "-- INSERT --") {
		t.Errorf("insert mode: %q", l)
	}
	e.mode = Normal
	e.in.queue = nil
	e.feed("g")
	e.msg = "something"
	if l := last(e); !strings.HasPrefix(l, "Write Get") {
		t.Errorf("in the tag, a message must not hide it: %q", l)
	}
}

// TestTutor works through exercises of the tutor on its own text, so
// that the tutor stays true to the editor.
func TestTutor(t *testing.T) {
	for _, tt := range []struct{ lesson, line, keys, want string }{
		{"Lesson 1.3", "---> Thhe", "0fhx", "---> The coww jumpedd ovverr thhe mooon."},
		{"Lesson 1.5", "---> There is some text missing fro", "A" + "m this line." + esc, "---> There is some text missing from this line."},
		{"Lesson 2.1", "---> There are a some", "0fafacw", "---> There are some words fun that don't belong paper in this line."},
		{"Lesson 2.2", "---> Somebody typed", "0f.lc$", "---> Somebody typed the end of this line twice."},
		{"Lesson 2.4", "--->  2)  Mud", "cc", "--->  3)  Violets are blue,"},
		{"Lesson 3.3", "---> This lubw", "0flceiline" + esc, "---> This line has a few wptfd that mrrf changing usf the cut operator."},
		{"Lesson 3.4", "---> This line has extra", "0/extra\rcw/extra\r.n.", "---> This line has words that do not belong."},
		{"Lesson 4.4", "---> thee best", ":e s/thee/the/g\r", "---> the best time to see the flowers is in the spring."},
		{"Lesson 5.3", "      :<date <RETURN> . The date", "$:<echo DATE\r", "      :<date <RETURN> . The date goes in at the cursorDATE."},
		{"Lesson 5.4", "---> pear", "V3j:|sort\r", "---> apple"},
		{"Lesson 6.2", "---> Please do not shout.", "$bs:|tr a-z A-Z\r", "---> Please do not SHOUT."},
		{"Lesson 6.3", "---> delta", "V3j:|sort\r", "---> alpha"},
		{"Lesson 6.4", "---> Remove XX", "0/XX\rscn.n.n.", "---> Remove the markers from this line."},
		{"Lesson 6.4", "---> The cat sat", "Vj:e s/\\<cat\\>/dog/g\r", "---> The dog sat on the mat. The dog was black."},
		{"Lesson 6.8", "---> x marks", "0xgwwwz", "---> x marks the spot"},
		{"Lesson 6.8", "---> delta", "V3jgA |sort" + esc + "z", "---> alpha"},
		{"Lesson 6.9", "---> item 1", "Vjj:e x/[0-9]+/c/N/\r", "---> item N costs N"},
	} {
		t.Run(tt.lesson, func(t *testing.T) {
			e := testEditor(tutorText)
			lines := strings.Split(tutorText, "\n")
			ln := -1
			for i, in := 0, false; i < len(lines); i++ {
				in = in || strings.Contains(lines[i], tt.lesson+":")
				if in && strings.HasPrefix(lines[i], tt.line) {
					ln = i
					break
				}
			}
			if ln < 0 {
				t.Fatalf("no line %q in %s", tt.line, tt.lesson)
			}
			e.feed(fmt.Sprintf(":%d\r", ln+1) + tt.keys)
			if got := strings.Split(string(e.f.t.r), "\n")[e.f.line(e.f.t.Bol(ln))]; got != tt.want {
				t.Errorf("keys %q:\ngot  %q\nwant %q (msg %q)", tt.keys, got, tt.want, e.msg)
			}
		})
	}
}

func TestCommandNames(t *testing.T) {
	// Every command by its name, its short form and as a word to execute.
	e := testEditor("one two\n")
	e.feed("x:undo\r")
	if got := string(e.f.t.r); got != "one two\n" {
		t.Errorf(":undo: %q", got)
	}
	e.feed(":redo\r:x Undo\r")
	if got := string(e.f.t.r); got != "one two\n" {
		t.Errorf("Undo: %q", got)
	}
	for _, bad := range []string{":se ts=4\r", ":und\r", ":Undo\r"} {
		e.feed(bad)
		if !strings.Contains(e.msg, "Not an editor command") {
			t.Errorf("%q should not be a command: %q", bad, e.msg)
		}
	}
	// Cut and Yank need a selection or an address.
	e.feed(":cut\r")
	if got := string(e.f.t.r); got != "one two\n" || !strings.Contains(e.msg, "no selection") {
		t.Errorf(":cut without dot: %q, %q", got, e.msg)
	}
	e.feed(":x Yank\r")
	if !strings.Contains(e.msg, "no selection") {
		t.Errorf("Yank without dot: %q", e.msg)
	}
	e.feed("s:cut\r") // the word, as Cut in the tag would
	if got := string(e.f.t.r); got != " two\n" {
		t.Errorf(":cut on dot: %q", got)
	}
	e.feed("0:paste\r")
	if got := string(e.f.t.r); got != "one two\n" {
		t.Errorf(":paste: %q", got)
	}
	// Del and Exit, by key, name, short form and word.
	dir := plumbDir(t)
	e = testEditor("")
	e.open(filepath.Join(dir, "a.txt"))
	e.open(filepath.Join(dir, "sub", "b.txt"))
	e.open(filepath.Join(dir, "notes"))
	e.feed(":x Del\r")
	if len(e.files) != 2 {
		t.Fatalf("Del: %d files", len(e.files))
	}
	e.feed("x:del\r")
	if len(e.files) != 2 {
		t.Fatalf(":del closed a modified buffer")
	}
	e.feed(":x Del!\r")
	if len(e.files) != 1 {
		t.Fatalf("Del!: %d files", len(e.files))
	}
	e.feed(":x Exit\r")
	if !e.quit {
		t.Fatalf("Exit did not leave")
	}
}

// TestKeyHelp checks that the key help (help.go) describes every key
// bound in the tables, and no key that is not.
func TestKeyHelp(t *testing.T) {
	check := func(name string, list []keyHelp, bound func(Key) bool, all []Key) {
		seen := map[Key]bool{}
		for _, h := range list {
			for _, k := range h.keys {
				if seen[k] {
					t.Errorf("%s: %s is described twice", name, keyName(k))
				}
				seen[k] = true
				if !bound(k) {
					t.Errorf("%s: %s is described but not bound", name, keyName(k))
				}
			}
		}
		for _, k := range all {
			if !seen[k] && k != KeyMouse {
				t.Errorf("%s: %s is bound but not described", name, keyName(k))
			}
		}
	}
	var nk, mk, vk []Key
	for k := range normal {
		nk = append(nk, k)
	}
	for k := range motions {
		mk = append(mk, k)
	}
	for k := range visualOps {
		vk = append(vk, k)
	}
	check("normal", normalHelp, func(k Key) bool { _, ok := normal[k]; return ok }, nk)
	check("motions", motionHelp, func(k Key) bool { _, ok := motions[k]; return ok }, mk)
	check("visual", visualHelp, func(k Key) bool {
		_, ok := visualOps[k]
		return ok || slices.Contains(visualLoopKeys, k)
	}, vk)
	for _, c := range exTab {
		if c.help == "" {
			t.Errorf(":%s has no help", c.name)
		}
	}
	// ? shows it, as a buffer.
	e := testEditor("")
	e.feed("?")
	if e.f.name != "[keys]" || !strings.Contains(string(e.f.t.r), "cut: c and a motion") ||
		!strings.Contains(string(e.f.t.r), ":write  :w") {
		t.Errorf("? opened %q:\n%s", e.f.name, string(e.f.t.r))
	}
	for i, l := range strings.Split(string(e.f.t.r), "\n") {
		if n := utf8.RuneCountInString(l); n > 79 {
			t.Errorf("line %d of the key help is %d wide: %q", i+1, n, l)
		}
	}
}

func TestUnnamedIsScratch(t *testing.T) {
	dir := plumbDir(t)
	e := testEditor("")
	e.open(filepath.Join(dir, "a.txt"))
	e.feed(":new\rihello\x1b")
	if e.f.name != "" || e.f.dirty() || strings.Contains(e.bufLine().s, "+ ") {
		t.Fatalf("unnamed buffer counts as modified: %q", e.bufLine().s)
	}
	e.feed("d")
	if len(e.files) != 1 {
		t.Fatalf("d did not close the unnamed buffer")
	}
	e.feed(":new\rihi\x1b:w " + filepath.Join(dir, "named") + "\rx")
	if !e.f.dirty() {
		t.Fatalf("a named buffer must count changes again")
	}
}

func TestErrorsName(t *testing.T) {
	dir := plumbDir(t)
	e := editorIn(dir, "sub/notes", "echo hi\n")
	e.feed("Vz")
	if got := e.shortName(e.f); got != "sub/+Errors" {
		t.Errorf("+Errors on the buffer line: %q", got)
	}
}

func TestDelTwice(t *testing.T) {
	dir := plumbDir(t)
	e := testEditor("")
	e.open(filepath.Join(dir, "a.txt"))
	e.open(filepath.Join(dir, "sub", "b.txt"))
	e.feed("xd")
	if len(e.files) != 2 || !strings.Contains(e.msg, "again closes it") {
		t.Fatalf("first d: %d files, msg %q", len(e.files), e.msg)
	}
	e.feed("d")
	if len(e.files) != 1 {
		t.Fatalf("second d did not close the modified buffer")
	}
	// Something in between: the warning starts over.
	e.feed("xdld")
	if len(e.files) != 1 {
		t.Fatalf("d after another command closed it")
	}
	e.feed(":x Del\r:x Del\r") // as with the word in the tag
	if !e.quit {
		t.Fatalf("Del twice on the last buffer did not leave")
	}
}
