package main

import (
	"fmt"
	"strings"
)

// The key help (?): a buffer listing every key, made from the key
// tables, so it shows the keys as they are bound, including those set
// in config.go. The lists below say what the keys do, in the order they
// are shown; keys with one meaning share a line. TestKeyHelp checks that
// the lists and the tables agree.

type keyHelp struct {
	keys []Key
	help string
}

func keys(k ...Key) []Key { return k }

var normalHelp = []keyHelp{
	{keys('i'), "insert before the cursor"},
	{keys('a'), "append after the cursor"},
	{keys('I'), "insert at the first non-blank of the line"},
	{keys('A'), "append at the end of the line"},
	{keys('o', 'O'), "open a line below, above"},
	{keys('c'), "cut: c and a motion (cw), cc a line"},
	{keys('y'), "yank (copy): y and a motion (yw), yy a line"},
	{keys('>', '<'), "shift right, left: > and a motion, >> a line"},
	{keys('x', KeyDelete), "cut the character under the cursor"},
	{keys('p', 'P'), "paste after, before the cursor (lines: below, above)"},
	{keys('J'), "join the line with the next"},
	{keys('r'), "rx: replace the character with x"},
	{keys('~'), "swap the case of the character"},
	{keys('u'), "undo"},
	{keys(ctrl('r')), "redo"},
	{keys('.'), "period: repeat the last change"},
	{keys('v', 'V'), "visual mode: select characters, lines"},
	{keys(keySelect), "select (button 1): the word; again: line, paragraph, file"},
	{keys(keyExecute), "execute (button 2) dot, or the word under the cursor"},
	{keys(keyLook), "look (button 3): open, plumb or find dot or the name"},
	{keys(keyTag), "go into the tag (on the bottom line) and back"},
	{keys(keyBuffers), "choose a buffer on the buffer line: TAB 2 for the second"},
	{keys(ctrl('^')), "the buffer you were in before"},
	{keys('d'), "close the buffer (Del; if modified: d twice)"},
	{keys('Q'), "leave tie (Exit)"},
	{keys(':'), "a command line (below)"},
	{keys('?'), "this list of keys"},
	{keys('m'), "mx: set mark x"},
	{keys('@'), "@x: run register x as keys; @@ again"},
	{keys('z'), "zt zz zb: put the line at the top, middle, bottom"},
	{keys(ctrl('d'), ctrl('u')), "scroll half a screen down, up"},
	{keys(ctrl('f'), ctrl('b'), KeyPgDn, KeyPgUp), "page forward, back"},
	{keys(ctrl('e'), ctrl('y')), "scroll a line down, up"},
	{keys(ctrl('g')), "show the file name and where you are"},
	{keys(ctrl('l')), "redraw the screen"},
	{keys(ctrl('z')), "suspend tie"},
	{keys(KeyEsc), "cancel; drop dot; leave the tag"},
}

var motionHelp = []keyHelp{
	{keys('h', 'l', KeyLeft, KeyRight), "left, right"},
	{keys('j', 'k', KeyDown, KeyUp), "down, up (keeping the column)"},
	{keys('w', 'W'), "the next word, big word (blank to blank)"},
	{keys('b', 'B'), "back a word, big word"},
	{keys('e', 'E'), "the end of the word, big word"},
	{keys('0', KeyHome), "the start of the line"},
	{keys('^'), "the first non-blank of the line"},
	{keys('$', KeyEnd), "the end of the line"},
	{keys('f', 'F'), "fx: the next, previous x on the line"},
	{keys('t', 'T'), "tx: up to the next, previous x on the line"},
	{keys(';', ','), "the last f F t T again, the other way"},
	{keys('G'), "the last line; with a count, line N (1G the first)"},
	{keys('H', 'M', 'L'), "the top, middle, bottom of the screen"},
	{keys('{', '}'), "the previous, next empty line"},
	{keys('%'), "the matching bracket"},
	{keys('/'), "/re: search forward; the match is dot"},
	{keys('n', 'N'), "the next match, in the other direction"},
	{keys('*', '#'), "search for the word under the cursor, backward"},
	{keys('\''), "'x: the line of mark x; '' back"},
}

var visualHelp = []keyHelp{
	{keys('c', KeyDelete), "cut dot"},
	{keys('y'), "yank dot; dot stays"},
	{keys('p'), "paste over dot"},
	{keys('J'), "join the lines"},
	{keys('~', 'u', 'U'), "swap, lower, upper the case"},
	{keys('r'), "rx: replace every character with x"},
	{keys('>', '<'), "shift the lines right, left"},
	{keys(':'), "a command line on dot"},
	{keys(keyExecute), "execute dot"},
	{keys(keyLook), "look at dot"},
	{keys(keySelect), "grow dot: lines, paragraph, file"},
	{keys(keyTag), "go into the tag, keeping dot"},
	{keys('o'), "go to the other end of dot"},
	{keys('v', 'V'), "select characters, lines; again: leave"},
	{keys(KeyEsc), "leave visual mode"},
}

// The keys visual mode handles itself, rather than through visualOps.
var visualLoopKeys = keys(keySelect, keyTag, 'o', 'v', 'V', KeyEsc)

// Insert mode and the command line handle their keys in code.
var insertHelp = []keyHelp{
	{keys(KeyEsc), "end the insert"},
	{keys(KeyEnter), "a new line"},
	{keys(KeyBS), "erase the character before the cursor"},
	{keys(ctrl('w'), ctrl('u')), "erase the word, the line before the cursor"},
	{keys(ctrl('v')), "insert the next key as it is"},
	{keys('\t'), "a tab (spaces with expandtab)"},
	{keys(KeyLeft, KeyRight, KeyUp, KeyDown), "move"},
	{keys(KeyHome, KeyEnd), "the start, the end of the line"},
}

var promptHelp = []keyHelp{
	{keys(KeyEnter), "run the command"},
	{keys(KeyEsc), "give up"},
	{keys(KeyUp, KeyDown), "earlier commands"},
	{keys('\t'), "complete a file name; again: the next"},
	{keys(KeyBS, ctrl('w'), ctrl('u')), "erase a character, a word, the line"},
}

var keyNames = map[Key]string{
	KeyUp: "Up", KeyDown: "Down", KeyLeft: "Left", KeyRight: "Right",
	KeyHome: "Home", KeyEnd: "End", KeyPgUp: "PgUp", KeyPgDn: "PgDn",
	KeyDelete: "Delete", KeyEsc: "ESC", '\t': "TAB", KeyEnter: "RETURN",
	KeyBS: "BACKSPACE", ' ': "SPACE",
}

func keyName(k Key) string {
	if n, ok := keyNames[k]; ok {
		return n
	}
	if k >= 0 && k < ' ' {
		return "^" + string(rune(k+'@'))
	}
	return string(rune(k))
}

func writeHelp(b *strings.Builder, title string, list []keyHelp) {
	fmt.Fprintf(b, "\n%s\n\n", title)
	for _, h := range list {
		var names []string
		for _, k := range h.keys {
			names = append(names, keyName(k))
		}
		fmt.Fprintf(b, "  %-18s %s\n", strings.Join(names, " "), h.help)
	}
}

// keysText is the text of the key help.
func keysText() string {
	var b strings.Builder
	b.WriteString("tie: the keys, as bound now (the keys of config.go included).\n")
	b.WriteString("d closes this list, ^^ goes back; :help is the introduction.\n")
	writeHelp(&b, "Normal mode (a count before a command repeats it: 3x)", normalHelp)
	writeHelp(&b, "Motions: move the cursor, extend dot, or follow an operator (cw)", motionHelp)
	writeHelp(&b, "Visual mode: motions extend dot, these act on it", visualHelp)
	writeHelp(&b, "Insert mode", insertHelp)
	b.WriteString("\nMouse\n\n")
	fmt.Fprintf(&b, "  %-18s %s\n", "button 1", "click, sweep, double click: select")
	fmt.Fprintf(&b, "  %-18s %s\n", "button 2", "execute;  button 3: look")
	fmt.Fprintf(&b, "  %-18s %s\n", "1-2, 1-3 chords", "cut, paste")
	clicks := map[int]string{optionClick: "Option-click", controlClick: "Control-click"}
	for _, n := range []int{2, 3} {
		if c, ok := clicks[n]; ok {
			fmt.Fprintf(&b, "  %-18s button %d\n", c, n)
		}
	}
	b.WriteString("\nCommand line (:), after an optional address: N . $ 'x /re/ a,b ,\n\n")
	for _, c := range exTab {
		names := ":" + c.name
		if c.short != "" {
			names += "  :" + c.short
		}
		fmt.Fprintf(&b, "  %-18s %s\n", names, c.help)
	}
	fmt.Fprintf(&b, "  %-18s %s\n", ":|cmd :<cmd :>cmd", "pipes on dot: filter, replace, send")
	fmt.Fprintf(&b, "  %-18s %s\n", ":!cmd", "run cmd on the terminal")
	writeHelp(&b, "On the command line", promptHelp)
	b.WriteString("\nA command name with a capital letter is the same command, executed:\n")
	b.WriteString("Write Get Diff Undo Yank Cut Paste Del ... in the tag.\n")
	return b.String()
}

// helpCmd (?) shows the keys, made afresh each time.
func helpCmd(e *Editor, c *ctx) {
	const name = "[keys]"
	for _, f := range e.files {
		if f.name == name {
			f.t = Text{r: []rune(keysText())}
			f.cur, f.top = 0, 0
			e.switchTo(f)
			return
		}
	}
	e.openText(name, keysText())
}
