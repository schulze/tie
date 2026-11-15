# AGENTS.md — working on tie

tie (the interactive editor) is a modal, vi-like terminal editor in Go
that borrows acme's model: a selection called **dot** that commands work
on, and three "buttons" — select, execute, look — on the keyboard, the
command line and the mouse.

## Where behaviour is specified

The documentation is the specification. Read it before changing
behaviour, and keep it true afterwards.

| file | what it is |
|---|---|
| `doc/intro.txt` | **The reference.** "An Introduction to Display Editing with Tie", after Bill Joy's vi paper. Describes every command; the *Appendix* lists every normal-mode key and every status-line command. Shown by `:help`. |
| `doc/tutor.txt` | The interactive tutor (vi-tutor style, `--->` practice lines). Teaches the same commands by doing. Opened by `tie -tutor` / `:tutor` on a temp copy. |
| `README.md` | Overview for people browsing the repository; file layout; how to extend. |
| `help.go` | The key help (`?`): what each key does, in lists next to the key tables; the `[keys]` buffer is made from them. `TestKeyHelp` fails when a key is bound without help, or described without being bound. |
| `config.go` | The user-editable choices, and only those: button keys, tag key and default tag, mouse default, modifier clicks, look rules (the plumber). Everything else is plain code without override machinery. |

Both `doc/` files are embedded in the binary (`main.go`, `go:embed`), so
a change to them ships with the next build.

**Rule:** a change in behaviour updates, in the same change,
1. the code and its tests,
2. `doc/intro.txt` (the section, and the Appendix if a key or command changed),
3. `doc/tutor.txt` if the command is taught there (`TestTutor` works through
   exercises on the tutor's own text; add a row for a new exercise. They must still work:
   an exercise line is typed on literally, so `--->` markers end up in
   selected text — put text to be *executed* on unmarked, indented lines),
4. `README.md` if the overview or layout changed,
5. the help lists in `help.go` when a key is added, removed or changes
   meaning (`TestKeyHelp` insists), and the `help` of an `exTab` entry.

## Build and test

    go build            # produces ./tie
    go vet ./...
    go test ./...
    gofmt -l .          # must print nothing

Dependencies: the standard library, `golang.org/x/term`, `golang.org/x/sys`.
Do not add others. cgo only if something cannot be done without it
(nothing needs it today).

Tests run the editor headless (`headlessTerm`), feeding keys:
`editor_test.go` has tables of *text, keys, expected text, cursor/dot*.
Add rows rather than new test functions where possible. Mouse events
are tested by feeding raw SGR reports (`mouse(b, x, y, up)`) through
the real decoder with `feedBytes`. Tests that touch files use
`t.TempDir()` (`plumbDir`, `editorIn`).

Real terminal behaviour (raw mode, the tab-width and size queries,
resizing, mouse reporting) cannot be tested headless. Run

    go build && python3 testdata/ptysmoke.py ./tie

which drives the binary on a pseudo-terminal. For anything visual, ask
the user to run tie or a small probe program in their terminal emulator
and report back.

## Concepts and names

- **dot** is the selection (`File.dot`, a `Range` of rune offsets),
  as in sam/acme. Never use "dot" for vi's `.`.
- **period** is vi's `.` (repeat last change), `period()` in `editor.go`.
  It works by replaying recorded keys (`Input.record/stop/push`), so any
  command that sets `changed` (table flag or `c.changed`) is repeatable.
- A command that sets dot itself sets `e.keepDot`; otherwise the main loop
  collapses dot to the cursor after each command.
- Positions are rune offsets into `Text` (`text.go`); lines are derived.
- Every non-empty buffer ends in a newline (enforced in `File.commit`).
- One command = one undo group (`File.pend`, committed by the main loop).
- Scratch buffers (`File.scratch`: `+Errors`, directory listings, help,
  tags) and buffers without a name never count as modified (`File.dirty`),
  as in acme. `+Errors` shows as `dir/+Errors` on the buffer line.
- While the cursor is in a tag, `e.f` is the tag's `File` (so all editing
  works on it) and `e.buffer()` is the buffer it belongs to. `e.ex`,
  `execute`, `look` and `switchTo` call `leaveTag` first: commands act on
  the buffer. Display code takes the `File` to draw as a parameter.

## Layout

| file | contents |
|---|---|
| `main.go` | flags, startup, crash rescue (`.swap` files) |
| `config.go` | user configuration (see above) |
| `term.go` | raw mode, control sequences, size, tab-width query, mouse on/off |
| `input.go` | key decoding: escape sequences, UTF-8, SGR mouse; replay queue; recording |
| `text.go` | rune buffer and line index |
| `file.go` | buffer: undo/redo, marks, load/save, directory listings |
| `disk.go` | files changed on disk: `checkDisk` (after each command and on `Input.onIdle`), stale buffers, `:diff` |
| `editor.go` | `Editor`, main loop, count/register prefix, `period`, registers |
| `normal.go` | table `normal`: key → command |
| `motion.go` | table `motions`: key → motion (kind, flags) |
| `op.go` | table `operators`, insert mode |
| `visual.go` | visual mode, table `visualOps` |
| `buttons.go` | select / execute / look; `unitAt`, `grow`, look rules; `:s :x :l`; capitalized words run commands |
| `tag.go` | the tag line: a scratch `File` per buffer (`File.tag`, `File.owner`); the buffer chooser (`Tab`) |
| `help.go` | the key help (`?`): help lists beside the key tables, the `[keys]` buffer |
| `mouse.go` | acme mouse: buttons 1–3, sweeps, double click, chords, wheel |
| `plumb.go` | file names in text, `file:addr`, `+Errors` |
| `ex.go` | status-line commands: table `exTab`, addresses, pipes, options |
| `edit.go` | `:edit` / `:e` — sam's command language, table `samCmds` |
| `search.go` | regexps: Go RE2 made to match as sam (`Longest`, `samClasses`), `\<` `\>`; substitution helpers |
| `display.go` | buffer line (top), bottom line (tag or message, position; the prompt), layout, wrapping, scrolling, diffed redraw, hit map, completion |

## Extending

Everything is table driven; prefer a table entry over a special case.

- Normal-mode key: `normal` (`normal.go`). A motion goes in `motions` and
  then works after every operator and in visual mode too.
- Operator: `operators` (`op.go`); doubling (`cc`) and visual mode follow.
- Visual-mode command: `visualOps` (`visual.go`).
- Status-line command: `exTab` (`ex.go`); order matters for abbreviations
  (`{"set", 2}` before `{"select", 1}` so `:se` is set and `:s` select).
- sam command for `:edit`: `samCmds` (`edit.go`).
- A word to execute (Write, Get, …) is a command in `exTab` (`ex.go`): every
  command is executed by its name with a capital letter; there is no
  second table.
- Plumbing: `lookRules` (`config.go`): pattern + shell command (`"$1"` is
  the text, `"$2"…` the groups) or a Go function.
- Option: `Editor.options` (`ex.go`) and `Options` (`editor.go`).

## Deliberate departures from vi

Keep these unless the user decides otherwise (they are documented in
`doc/intro.txt`):

- `s` `z` `t` are the select / execute / look buttons (`config.go`):
  `s` was free, and vi's `zt zz zb` and `tx`/`Tx` were dropped for `z`
  and `t` (`fx`/`Fx` find a character). Earlier choices were `A S D` and
  `Z X C`; the user prefers unshifted keys.
- `c` is cut (vi's `d`); vi's change operator is gone (cut, then insert).
  `d` is acme's Del: it closes the buffer (`:del`, short `:q`; the last
  one leaves the editor). On a modified buffer the first only warns, and
  a second right after closes it, as in acme (`Editor.warned`, `ncmd`). `Q` / `:exit` / `:qa` leave. `:wq` writes and
  closes. `y` on dot leaves visual mode but keeps dot (as acme's Snarf),
  so more can be done with it; `c` leaves no dot.
- Every command in `exTab` has one name and at most one short form
  (`write`/`w`, `del`/`q`, `exit`/`qa`, `edit`/`e`, `look`/`l`,
  `select`/`s`, `execute`/`x`); no other abbreviations.
- `:s` select, `:x` execute, `:l` look. `:edit` / `:e` is sam's command
  language (acme's Edit); substitute is `:e s/…/…/`, on the range as one
  text. vi's `:e file` is gone: look opens files (`:l name`, `t`), and
  `:new name` (`New` in the tag) starts a new one. `Get` re-reads from disk.
  vi's `:s`/`:su` and `&` are gone.
- Visual mode keeps vi's meaning for `<` `>` (shift). The only filter is
  acme's `|` (`:|cmd`, `|` in visual mode): vi's `!` operator, visual `!`
  and `:a,b!cmd` were removed; `:!cmd` only runs a command on the terminal.
- The whole file is `,` (sam), not vi's `%`; `:%…` says so. There is no
  `gf`: look (`t`) opens files.
- Executed text (`z`, button 2, `:x`) and `>cmd` send output to
  `+Errors`; `:!cmd` runs on the terminal as in vi.
- `g` in normal and visual mode goes to the tag and back; vim's `gg` is
  gone (use `1G`, as in vi). `Tab` puts the cursor on the buffer line:
  `Tab 2` goes to buffer 2. The user prefers normal-mode keys to colon
  commands, so `:b`, `:n`, `:N`, `:bd` and Alt-digit were removed for them.
- Regular expressions are Go's RE2 (no engine of our own), matching as
  sam's: leftmost-longest (`re.Longest()`) and negated classes that never
  match a newline (`samClasses` rewrites `[^x]` to `[^x\n]`).

Avoid adding second ways to do the same thing; when in doubt, ask the
user. Earlier rounds removed duplicates for that reason: visual `!` as
execute, visual `< >` as pipes, `gf`, `:e #` and `N^^` (`^^` remains),
`:b N`, `:n`, `:N`, `:bd` and Alt-N (`Tab N`), `:ls` (the buffer line shows
the buffers), `:previous`, `:=`, and the file name on the status line.
Later: the vi aliases `s S D Y` and `R` (replace mode), the motions
`^H ^J ^N ^P`, Space, Backspace, `+ -` Enter, `|` and `` ` ``, the `z`
spellings other than `zt zz zb` (and later those too, for `z`), the
`tx`/`Tx` motions (for `t`), visual `x s P |`, `:put :undo :redo`
(the builtins call the code), `:f`, `:r` (`:<cat file`), `:xit` (`:wq`),
a count before `:`, and the `wrapscan` option (searches always wrap).
Then: vi's change operator (`c` became cut, vi's `d`), `d` as delete (it
is Del now), the `q` key, prefix abbreviations of commands, and the
separate builtins table (commands are executed by their names).

Names: one word per thing, the common one. Keys keep their vi letters,
and the words follow: yank (`y`, the tag's Yank; acme says Snarf),
paste (`p`, Paste; vi says put), write (`:w`, Write; acme says Put).

vi compatibility is not a goal in itself: keep a vi key where it makes
tie easy to use. The keyboard's own keys (arrows, Home, End, PgUp, PgDn,
Delete) also work, as everyone expects them to.

## Possible future work

Recorded so it is not lost; none of it is wanted yet.

- **Several windows on screen** (splits, as in vim or tmux, or acme's
  columns). Not small: the view state (`cur`, `top`, `dot`, `want`) lives
  in `File` today and would move to a `Window` that points at a `File`, so
  two windows can show one file; `draw`, `scroll`, `visible`, `posAt` and
  the hit map would work per window inside a rectangle; the mouse and the
  keyboard need a focus and commands to split, close and move between
  windows; each window would show its buffer's tag. Roughly 500–800 lines.
- Tags that **persist** across sessions (today a tag lives while tie
  runs), e.g. saved next to the file or in one file of tags.
- **Asynchronous commands**: executed commands block the editor until
  they finish; acme runs them in the background and streams output into
  `+Errors`.
- The rest of **sam's language** in `:edit`: `{ }` grouping and command
  sequences, `m`, `t`, `p`, `=`, and the multi-file `X` / `Y` / `B` / `D`.
- vi pieces not done yet: sentence motions `( )`, `q` macro recording
  (`q` is free), `U`,
  word abbreviations, `wrapmargin`, a startup file.
