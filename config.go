package main

import "runtime"

// This file is meant to be edited: it holds the choices a user is most
// likely to want to change. Rebuild tie after changing it.

// The three acme mouse buttons on the keyboard, side by side at the
// bottom left, in the order of the buttons: select, execute, look.
// They replace vi's ZZ (use :wq or :xit), X (use dh) and C (use c$).
const (
	keySelect  Key = 'Z' // button 1: select the word; again: grow to line, paragraph, file
	keyExecute Key = 'X' // button 2: execute dot, or the word under the cursor
	keyLook    Key = 'C' // button 3: look: open, plumb or search for dot or the word
)

// keyTag goes into the tag line of the buffer and back (tag.go).
// keyBuffers goes to the buffer line, where a number (or h, l and
// RETURN) chooses a buffer. defaultTag is what a new tag holds: command
// names, capitalized, to be executed with keyExecute or button 2 (every
// command of the command line works: Write is :write), and room for
// your own.
const (
	keyTag     Key = 'g'
	keyBuffers Key = '\t'
	defaultTag     = "Write Get Diff Undo Redo Yank Cut Paste New Del |fmt"
)

// mouseDefault says whether tie takes the mouse (:set mouse).
const mouseDefault = true

// A click of button 1 with Option (Alt) or Control held down counts as
// this button (0: no change), for mice and trackpads without three
// buttons. (acme on macOS uses Option for button 2 and Command for 3;
// terminals do not report Command.) Swap the numbers to swap the keys.
const (
	optionClick  = 3
	controlClick = 2
)

// opener is the program that opens a file or URL with the desktop's
// application for it: open on macOS, xdg-open elsewhere.
var opener = func() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}()

// lookRules are tried, in order, on text being looked at (C, button 3,
// :l) before tie tries it as a file name and then as text to search for.
// This is tie's plumber.
//
// A rule's pattern is a regular expression. When it matches, cmd is run
// by the shell, in the directory of the buffer, without waiting for it;
// in cmd, "$1" is the text looked at and "$2", "$3", ... are the
// pattern's parenthesized groups. Or fn, if set, is called with the
// matches instead.
var lookRules = []lookRule{
	{pattern: `^(https?|ftp)://\S+$`, cmd: opener + ` "$1"`},
	{pattern: `^mailto:\S+$`, cmd: opener + ` "$1"`},
	{pattern: `(?i)^\S+\.(pdf|ps|eps|png|jpe?g|gif|svg|webp|html?|docx?|xlsx?|pptx?|odt|mp3|mp4|mov)$`, cmd: opener + ` "$1"`},
	// ls(1) opens the manual page in a +Errors buffer.
	{pattern: `^([a-z0-9_.-]+)\(([0-9])\)$`, fn: func(e *Editor, m []string) error {
		e.runCaptured("man "+m[2]+" "+m[1]+" | col -b", nil)
		return nil
	}},
}
