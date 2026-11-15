package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// :edit (or :e) runs a command of sam's command language, as acme's
// Edit does:
//
//	:e [address] command
//
// The address is a tie address (ex.go); without one the command works
// on dot, or on the current line when dot is empty. Regular expressions
// span lines: ^ and $ match at every line. Commands:
//
//	s/re/text/[g]  substitute text for the first (every) match
//	x/re/ cmd      run cmd on every match of re; dot is the match
//	y/re/ cmd      run cmd on the text between the matches
//	g/re/ cmd      run cmd if the range contains a match
//	v/re/ cmd      run cmd if it does not
//	a/text/        append text after the range
//	i/text/        insert text before the range
//	c/text/        change the range to text
//	d              delete the range
//	|cmd <cmd >cmd pipes, as on the command line
//
// In text, \n is a newline; in s, & and \1 are as in vi. With no
// command, the address becomes dot. So
//
//	:e ,x/teh/c/the/     :e ,s/teh/the/g     :e ,x/.*\n/i/> /
type samCmd func(e *Editor, r Range, arg string) error

var samCmds map[byte]samCmd

func init() {
	samCmds = map[byte]samCmd{
		's': samSubst,
		'x': samLoop, 'y': samLoop, 'g': samLoop, 'v': samLoop,
		'a': samText, 'i': samText, 'c': samText,
		'd': func(e *Editor, r Range, arg string) error {
			e.f.delete(r.Q0, r.Q1)
			e.f.dot = Range{r.Q0, r.Q0}
			return nil
		},
		'|': samPipe, '<': samPipe, '>': samPipe,
	}
}

func exSam(e *Editor, x *exArgs) error {
	s := x.arg
	r, _, has, rest, err := e.parseRange(s)
	if err != nil {
		return err
	}
	if !has {
		r = x.r
	}
	if err := e.sam(r, rest); err != nil {
		return err
	}
	e.keepDot = true
	e.f.cur = e.f.dot.Q0
	return nil
}

// sam runs one command on r.
func (e *Editor) sam(r Range, s string) error {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		e.f.dot = r
		return nil
	}
	c, ok := samCmds[s[0]]
	if !ok {
		return fmt.Errorf("Edit: unknown command %q", s[0])
	}
	// The argument keeps the command letter for commands that need it.
	return c(e, r, s)
}

func (e *Editor) samRegexp(pat string) (*regexp.Regexp, error) {
	if pat == "" {
		pat = e.pat
	}
	if pat == "" {
		return nil, errors.New("Edit: no pattern")
	}
	e.setPat(pat, false)
	return e.compile("(?m)" + pat)
}

// delimited splits "/a/rest" at its delimiters.
func delimited(s string) (string, string, error) {
	if s == "" || s[0] == ' ' || s[0] == '\\' || s[0] == '\n' {
		return "", "", errors.New("Edit: missing delimiter")
	}
	field, rest := splitDelim(s[1:], s[0])
	return field, rest, nil
}

var textEscapes = strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\\`, `\`)

func samText(e *Editor, r Range, s string) error {
	text, _, err := delimited(s[1:])
	if err != nil {
		return err
	}
	t := []rune(textEscapes.Replace(text))
	f := e.f
	switch s[0] {
	case 'a':
		f.insert(r.Q1, t)
		f.dot = Range{r.Q1, r.Q1 + len(t)}
	case 'i':
		f.insert(r.Q0, t)
		f.dot = Range{r.Q0, r.Q0 + len(t)}
	case 'c':
		f.replace(r.Q0, r.Q1, t)
		f.dot = Range{r.Q0, r.Q0 + len(t)}
	}
	return nil
}

func samSubst(e *Editor, r Range, s string) error {
	pat, rest, err := delimited(s[1:])
	if err != nil {
		return err
	}
	d := s[1]
	rep, flags := splitDelim(rest, d)
	re, err := e.samRegexp(pat)
	if err != nil {
		return err
	}
	f := e.f
	out, n := subst(re, f.t.String(r.Q0, r.Q1), viReplacement(rep), strings.Contains(flags, "g"))
	if n == 0 {
		f.dot = r
		return nil
	}
	t := []rune(out)
	f.replace(r.Q0, r.Q1, t)
	f.dot = Range{r.Q0, r.Q0 + len(t)}
	return nil
}

// samLoop is x, y, g and v.
func samLoop(e *Editor, r Range, s string) error {
	pat, rest, err := delimited(s[1:])
	if err != nil {
		return err
	}
	re, err := e.samRegexp(pat)
	if err != nil {
		return err
	}
	f := e.f
	text := f.t.String(r.Q0, r.Q1)
	switch s[0] {
	case 'g', 'v':
		if re.MatchString(text) == (s[0] == 'g') {
			return e.sam(r, rest)
		}
		return nil
	}
	// The ranges of the matches (x) or of the text between them (y),
	// in runes. They are worked on from the last to the first, so that
	// changes do not move the ones still to come.
	var ranges []Range
	prev, q := 0, r.Q0
	for _, m := range re.FindAllStringIndex(text, -1) {
		a := q + utf8.RuneCountInString(text[prev:m[0]])
		b := a + utf8.RuneCountInString(text[m[0]:m[1]])
		ranges = append(ranges, Range{a, b})
		prev, q = m[1], b
	}
	if s[0] == 'y' {
		var between []Range
		start := r.Q0
		for _, m := range ranges {
			between = append(between, Range{start, m.Q0})
			start = m.Q1
		}
		ranges = append(between, Range{start, r.Q1})
	}
	if strings.TrimSpace(rest) == "" {
		return errors.New("Edit: x and y need a command")
	}
	for i := len(ranges) - 1; i >= 0; i-- {
		if err := e.sam(ranges[i], rest); err != nil {
			return err
		}
	}
	f.dot = Range{r.Q0, r.Q0}
	return nil
}

func samPipe(e *Editor, r Range, s string) error {
	return e.pipe(s[0], strings.TrimSpace(s[1:]), &exArgs{r: r, addr: true})
}
