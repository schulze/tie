# tie — the interactive editor

A modal, vi-like editor in Go with acme's model: the selection, **dot**,
is something commands work on, and three buttons — **select, execute,
look** — do most of the work, from the keyboard, the command line or
the mouse. It depends only on the standard library and
`golang.org/x/term` / `golang.org/x/sys`.

    go build
    ./tie -tutor          # the interactive tutorial
    ./tie file...

`?` inside tie lists every key and command as bound (made from the key
tables, so it follows `config.go`). `:help` opens the full description,
`doc/intro.txt`, written
after Bill Joy's *An Introduction to Display Editing with Vi*; its
appendix lists every key and command. `doc/tutor.txt` is the tutor.
Both are embedded in the binary. Working on the code: see `AGENTS.md`.

## The three buttons

| acme       | key | command | mouse    | with nothing selected, takes |
|------------|-----|---------|----------|------------------------------|
| select     | `Z` | `:s`    | button 1 | the word (`Z` again: line, paragraph, file) |
| execute    | `X` | `:x`    | button 2 | the non-blank word under the cursor |
| look       | `C` | `:l`    | button 3 | the file name / word under the cursor |

* **Execute**: `:cmd` → tie command; `|cmd <cmd >cmd` → pipe on dot;
  a capitalized command name (`Write`, `Get`, `Diff`, `Undo`, `Yank`,
  `Cut`, `Paste`, `New`, `Del`, `Exit`, `Edit …`, `Look …`) → that command;
  else shell, output to the scratch buffer `dir/+Errors`.
* **Look**: the look rules in `config.go` (URLs, PDFs and images go to
  `open`/`xdg-open`, `ls(1)` to man), then a file or directory, with acme
  addresses `file:12`, `file:12:5`, `file:/re/`; else search for the text.
* **Tag**: every buffer has a tag line (on the bottom line, left of the
  cursor position), as in acme, holding
  `Write Get Diff Undo Redo Yank Cut Paste New Del |fmt` and whatever you add.
  `g` goes there and back; execute, look and `:` commands in the tag
  work on the buffer and its dot.
* **Mouse**: button 1 click, sweep, double click (word / line / brackets);
  button 2 and 3 click or sweep; chords 1-2 cut and 1-3 paste; wheel.
  Control-click is button 2 and Option-click button 3, set in
  `config.go` (acme on macOS uses Option and Command; terminals do not
  report Command).
* The keys take the place of vi's `ZZ`, `X`, `C`; `:s` and `:x` of vi's
  substitute and xit (use `:e s/…/…/` and `:wq`). The bindings are in
  `config.go`.
* `c` cuts (vi's `d`): `cw`, `cc`, `c$`, `c` on dot; to change text, cut
  and insert. `d` closes the buffer, as acme's Del.
* Each command has one name and at most one short form: `:write` `:w`,
  `:del` `:q`, `:exit` `:qa`, `:edit` `:e`, `:look` `:l`, `:get`, `:cut`,
  `:yank`, `:paste`, `:undo`, `:redo`, `:diff`, `:new`, …

## Dot

* `Z`, `v`, `V` and any motion make dot; a search (`/re`, `n`, `*`) makes
  the match dot, as does `:/re/` and `:5,9s`. Other motions drop it.
* On dot: `c y p J ~ u U r`, and `:` commands, which default to dot.
* Pipes from acme: `:|cmd`, `:<cmd`, `:>cmd`. Visual `< >` shift, as in vi. There is no `!`
  filter and no `%`: filter with `|`, and `,` is the whole file (sam).
* `:edit` (`:e`) runs sam's command language: `,s/a/b/g`,
  `,x/re/c/text/`, `x/.*\n/g/re/d`, `y`, `g`, `v`, `a`, `i`, `c`, `d`.
* `Z` or `v` with a non-empty dot starts from it, so `/re` `Zc` `n` `.`
  cuts match after match.
* Files changed on disk are read again if the buffer is clean; otherwise
  `!` on the buffer line, `:w` refuses, and `:diff` (`Diff`) shows the
  differences: then `Get` takes the disk, `:w!` keeps yours.
* Buffers: the buffer line on top lists them (click to switch); `d`
  closes one (the last one quits), `Q` quits; `Tab 2` goes to buffer 2
  (Tab puts the cursor on the buffer line), `^^` to the previous one;
  Tab completes file names on the command line; `:e` with dot opens the
  file it names.

vi's `.` is called **period** (`period()` in `editor.go`), because dot
is the selection.

## Layout

| file         | contents                                                |
|--------------|---------------------------------------------------------|
| `config.go`  | **edit me**: button keys, tag, builtins, look rules     |
| `term.go`    | raw mode, VT100 sequences, size and tab-width queries, mouse |
| `input.go`   | key decoding (escapes, UTF-8, SGR mouse), replay queue  |
| `text.go`    | the rune buffer and line index                          |
| `file.go`    | a buffer: undo groups, marks, load/save, directories    |
| `disk.go`    | files that change on disk; `:diff`                      |
| `editor.go`  | main loop, counts/registers, `period`                   |
| `normal.go`  | **table** `normal`: key → command                       |
| `motion.go`  | **table** `motions`: key → motion (kind, flags)         |
| `op.go`      | **table** `operators`, insert mode                      |
| `visual.go`  | **table** `visualOps`                                   |
| `buttons.go` | select, execute, look                                   |
| `tag.go`     | the tag line of a buffer                                |
| `mouse.go`   | the acme mouse                                          |
| `plumb.go`   | `file:address`, `+Errors`                               |
| `ex.go`      | **table** `exTab`, addresses, pipes, options            |
| `edit.go`    | **table** `samCmds`: `:edit`                            |
| `search.go`  | regexps, substitution                                   |
| `display.go` | layout, wrapping, scrolling, diffed redraw, hit map, prompt |

## Extending

* A key: `normal` (or `motions`, which then also work after every
  operator and in visual mode). An operator: `operators`.
* A `:` command: `exTab`; it receives the address range (dot by default).
* A sam command: `samCmds`. A builtin word or a plumbing rule: `config.go`.

Period replays recorded keys, so new change commands are repeatable
without extra work: set `change: true` in the table (or `c.changed`).

## Tests

    go test

`editor_test.go` runs the editor headless on tables of
*text, keystrokes, expected text*; mouse tests feed SGR reports.
