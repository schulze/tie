// Tie is the interactive editor: a modal, vi-like editor in which, as
// in acme, the selection (dot) is something commands work on.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
)

//go:embed doc/tutor.txt
var tutorText string

//go:embed doc/intro.txt
var introText string

func main() {
	tutor := flag.Bool("tutor", false, "start the tutorial")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: tie [-tutor] [file ...]")
		flag.PrintDefaults()
	}
	flag.Parse()

	t, in, err := openTerm()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tie:", err)
		os.Exit(1)
	}
	e := newEditor(t, in)
	defer e.rescue()

	e.files = []*File{newFile("")}
	e.f = e.files[0]
	if *tutor {
		e.msgErr(e.openTutor())
	}
	for _, name := range flag.Args() {
		e.open(name)
	}
	if !*tutor && len(e.files) > 1 {
		e.f, e.alt = e.files[0], nil
	}
	e.run()
	t.close()
}

// rescue saves modified buffers if tie crashes.
func (e *Editor) rescue() {
	r := recover()
	if r == nil {
		return
	}
	e.t.close()
	fmt.Fprintf(os.Stderr, "tie: %v\n%s", r, debug.Stack())
	for i, f := range e.files {
		if !f.dirty() {
			continue
		}
		name := f.name
		if name == "" {
			name = fmt.Sprintf("tie%d", i)
		}
		name += ".swap"
		if _, err := writeFile(name, f.t.r); err == nil {
			fmt.Fprintf(os.Stderr, "file saved to %s\n", name)
		}
	}
	os.Exit(2)
}
