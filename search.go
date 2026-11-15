package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var wordBoundary = strings.NewReplacer(`\<`, `\b`, `\>`, `\b`)

// compile turns a pattern into a regexp. Patterns are Go (RE2) regular
// expressions, which match as sam's do: the longest match at the
// leftmost place wins (a|ab matches all of ab), and a negated class
// never matches a newline. vi's \< and \> are word boundaries.
func (e *Editor) compile(pat string) (*regexp.Regexp, error) {
	p := samClasses(wordBoundary.Replace(pat))
	if e.opt.ic {
		p = "(?i)" + p
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, fmt.Errorf("Bad pattern: %s", strings.TrimPrefix(err.Error(), "error parsing regexp: "))
	}
	re.Longest()
	return re, nil
}

// samClasses adds a newline to every negated class, [^x] to [^x\n], so
// that it does not match across lines. A ] right after [ or [^ is part
// of the class, as are escapes and [:name:].
func samClasses(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		switch {
		case p[i] == '\\' && i+1 < len(p):
			b.WriteString(p[i : i+2])
			i++
		case p[i] == '[':
			j := i + 1
			neg := j < len(p) && p[j] == '^'
			if neg {
				j++
			}
			if j < len(p) && p[j] == ']' {
				j++
			}
			for j < len(p) && p[j] != ']' {
				switch {
				case p[j] == '\\' && j+1 < len(p):
					j += 2
				case strings.HasPrefix(p[j:], "[:") && strings.Contains(p[j+2:], ":]"):
					j += strings.Index(p[j+2:], ":]") + 4
				default:
					j++
				}
			}
			if j >= len(p) { // not closed: let regexp say so
				b.WriteString(p[i:])
				return b.String()
			}
			b.WriteString(p[i:j])
			if neg {
				b.WriteString(`\n`)
			}
			b.WriteByte(']')
			i = j
		default:
			b.WriteByte(p[i])
		}
	}
	return b.String()
}

// search finds the first match after q (before q if back), line by
// line as vi does. It reports whether the search wrapped around.
func (f *File) search(re *regexp.Regexp, q int, back bool) (Range, bool, bool) {
	nl := f.t.Nlines()
	l0 := f.line(q)
	for i := 0; i <= nl; i++ {
		ln := l0 + i
		if back {
			ln = l0 - i
		}
		wrapped := ln < 0 || ln >= nl
		ln = (ln%nl + nl) % nl
		bol := f.t.Bol(ln)
		s := f.t.String(bol, f.t.Eol(ln))
		locs := re.FindAllStringIndex(s, -1)
		for j := range locs {
			if back {
				j = len(locs) - 1 - j
			}
			m := locs[j]
			a := bol + utf8.RuneCountInString(s[:m[0]])
			b := a + utf8.RuneCountInString(s[m[0]:m[1]])
			switch {
			case i == 0 && !back && a <= q,
				i == 0 && back && a >= q,
				i == nl && !back && a > q,
				i == nl && back && a < q:
				continue
			}
			return Range{a, b}, wrapped || i == nl, true
		}
	}
	return Range{}, false, false
}

// viReplacement converts a vi replacement (& and \1) to Go's ($0, ${1}).
func viReplacement(rep string) string {
	var b strings.Builder
	for i := 0; i < len(rep); i++ {
		c := rep[i]
		switch {
		case c == '\\' && i+1 < len(rep):
			i++
			switch d := rep[i]; {
			case d >= '0' && d <= '9':
				b.WriteString("${" + string(d) + "}")
			case d == 'n' || d == 'r':
				b.WriteByte('\n')
			case d == 't':
				b.WriteByte('\t')
			case d == '$':
				b.WriteString("$$")
			default:
				b.WriteByte(d)
			}
		case c == '&':
			b.WriteString("${0}")
		case c == '$':
			b.WriteString("$$")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// subst replaces the first (or every) match of re in s.
func subst(re *regexp.Regexp, s, tmpl string, global bool) (string, int) {
	lim := 1
	if global {
		lim = -1
	}
	var b []byte
	last, n := 0, 0
	for _, m := range re.FindAllStringSubmatchIndex(s, lim) {
		b = append(b, s[last:m[0]]...)
		b = re.ExpandString(b, tmpl, s, m)
		last = m[1]
		n++
	}
	if n == 0 {
		return s, 0
	}
	return string(append(b, s[last:]...)), n
}

// splitDelim splits s at the first d not escaped by a backslash; \d
// becomes d. The rest after d is returned.
func splitDelim(s string, d byte) (string, string) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == d:
			b.WriteByte(d)
			i++
		case s[i] == '\\' && i+1 < len(s):
			b.WriteString(s[i : i+2])
			i++
		case s[i] == d:
			return b.String(), s[i+1:]
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), ""
}
