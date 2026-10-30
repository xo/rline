# rline

rline is a readline package for Go: it reads a line of text from a terminal,
with editing, history, completion and colored output. It is a pure Go port
of isocline, a line editor written in C by Daan Leijen under the MIT
license. It uses no cgo. usql is the program it exists for.

The module is `github.com/xo/rline`, and `go.mod` asks for Go 1.25.0. It
depends on `github.com/mattn/go-runewidth` and `golang.org/x/sys` (D7), and
D39 adds `github.com/xo/inputrc` for reading inputrc files. D55 adds
`github.com/xo/terminfo` for the capabilities of the terminal. Add no other
dependency without a decision.

## Standing rules

These hold in every `xo` repository, for every coding agent (dbmeta D110,
and D36 here).

1. Stage changes for review. Commit and push only when Ken says so.
2. Load the simple-english skill before you write any text that a person
   reads: project documentation, a code comment, an error message or a
   commit message.
3. In a Go project, load the go-pedantry skill before you write or review Go
   code. A rule in this file wins where the two conflict.

`CLAUDE.md` holds one line that imports this file, so that Claude Code and
every other agent read the same rules. Edit this file, not that one.

## Which document to read

Read [docs/PLAN.md](docs/PLAN.md) first. It holds the plan, every decision
with its status, and the questions that are open for Ken. A decision marked
`Proposed` is not settled. Do not decide an open question on your own. Ask
Ken.

| If you are | Read |
| --- | --- |
| starting any work | [docs/PLAN.md](docs/PLAN.md), then [docs/BACKLOG.md](docs/BACKLOG.md) |
| asking why something is the way it is | the decision index at the top of [docs/PLAN.md](docs/PLAN.md) |
| changing what the port does compared with the C | "Known departures" and "Bugs found in the C code" in [docs/PORT.md](docs/PORT.md) |
| writing a test, a corpus or a check | [docs/TESTING.md](docs/TESTING.md), then [docs/LESSONS.md](docs/LESSONS.md) |
| adding a file, a package or a build tag | [docs/LAYOUT.md](docs/LAYOUT.md) |
| working on Windows, a Unix system or CI | [docs/PLATFORMS.md](docs/PLATFORMS.md) |
| adding something that usql needs | [docs/USQL.md](docs/USQL.md), then D16 in [docs/PLAN.md](docs/PLAN.md) |
| testing in a real terminal emulator | [uitest/README.md](uitest/README.md) |
| setting up a machine to work here | [CONTRIBUTING.md](CONTRIBUTING.md) |

A bare decision number, such as D3, means a decision in this repository's
[docs/PLAN.md](docs/PLAN.md). A reference to a decision of another
repository names that repository, such as dbmeta D110.

## Layout

The root holds the package `rline`. Its files follow what a reader looks
for, not the C file they came from, and the header of each file names the C
files (D15):

- `rline.go`: the public API, the session log, and reading a password.
- `prompt.go`: reading one line, the redraw, hints, resize and the loop
  that hands each key to the line.
- `bind.go`: the keymaps, built from readline's defaults, the commands that
  rline runs itself, the commands that a program adds (D58), and the list of
  completions that readline shows.
- `comp.go`: completion and file name completion.
- `menu.go`: isocline's numbered completion menu, which
  `WithCompletionMenu` binds to Tab (D51).
- `history.go`: the history list and its file. Walking and searching it are
  readline's commands, in `internal/readline`.
- `bbcode.go`: markup and the highlighting built on it.
- `term.go`: writing to a terminal.
- `decoder.go`: decoding bytes and escape sequences into keys, and turning
  Windows key events into sequences. It has no build tag, so every system
  tests it.
- `tty_*.go`: the terminal device. `tty_posix.go` is `unix && !aix`.
  `tty_sysv.go` and `tty_bsd.go` hold the ioctl numbers of each family,
  and read the output speed as that family keeps it.
  `tty_sti.go` and `tty_nosti.go` split on the `TIOCSTI` ioctl.
  `tty_other.go` is the fallback that reads plain lines.
- `sys_*.go`: everything that shares one build tag.

The other folders:

- `key/`: the key codes, exported (D8).
- `ansi/`: colors, the palette, attributes and the color names. It is public
  and needs nothing else from rline (D23).
- `internal/text/`: the buffer a line is edited in, widths, the QUTF-8 codec
  and brace matching.
- `internal/editor/`: the line being edited, the operations on it and the
  undo stack. These are isocline's, and the corpus of the C tests them.
- `internal/readline/`: all of GNU readline's commands over one line, with
  the kill ring, the undo list, the numeric argument, the history, the
  keymaps, completion, incremental search, the prompt that reads a search or
  a command name, the commands that print, the variables of an inputrc file
  and vi's modes (D44 to D46). It is written from readline's manual and recordings of
  readline 8.3, and never from readline's C, which is under the GPL (D47).
- `internal/capture/`: records a program under a pseudo-terminal, and holds
  the sessions recorded from the C demo in `testdata/<goos>/`.
- `testdata/`: the corpora recorded from the C. `testdata/default/` and
  `testdata/darwin/` split on the mark at the end of a wrapped row.
  `testdata/readline-emacs.txt` and `testdata/readline-binds.txt` are
  recorded from GNU readline 8.3.
- `tools/`: the C probes (`probe-*.c`), the scripts that build them
  (`build-*.sh`), the linter module (`tools/lint`), `lint.sh`, `lint.ps1`,
  `mutate.sh`, and `systems.txt`, the systems that the checks vet (D60).
- `_examples/`: one folder for each example program (D37). The underscore
  keeps them out of `./...`, so a command names the folder.
  - `_examples/sql/`: a small SQL prompt that uses the package. The tests
    in `rline_test.go` and `uitest` build it with
    `go build ./_examples/sql`.
  - `_examples/bash/`: a prompt that highlights bash, prints each line
    back and keeps it in the history, and runs nothing. Its tests run
    through `TestBashExampleTests` in `rline_test.go`.
  - `_examples/transit/`: a prompt that highlights and completes code with
    transit. It is a module of its own, so that rline gets no new dependency
    (D7). Its `go.mod` requires a published transit, and replaces rline
    with this checkout, so that the example always builds against the code
    beside it. Ken chose to keep the replace on 2026-10-30. D38 removed it
    for v0.1.0 only. Run `go test ./...` inside it.
- `uitest/`: drives real terminal emulators. It does not run in CI.
- `contrib/`: installers for what `uitest` drives, and the script that makes
  the demonstration GIF.
- `isocline/`: the C source. It is a separate git repository, and git
  ignores it here. Never write inside it.
- `docs/`: every document except the four in the root.

## Commands

Run these from the repository root. CI runs all of them on Linux, macOS and
Windows (D20).

```sh
gofmt -l .                  # must print nothing
go build ./...
go vet ./...
go test ./...               # on Windows: go test -skip TestGoldenSetIsPresent ./...
go test -race . ./key       # on Windows this needs a C compiler
tools/lint.sh               # on Windows: pwsh -File tools/lint.ps1
```

`tools/lint.sh` builds the pinned golangci-lint from `tools/lint` and runs
it for linux, darwin and windows, with the settings in `.golangci.yml`.

`go vet` also runs for each GOOS/GOARCH pair in `tools/systems.txt`,
because a file tagged for another system is invisible to a build on this
one. The list holds one pair for each build tag (D60), and
`TestEveryFileBuildsSomewhere` makes sure that every file compiles in one of
them. The loop is in `.github/workflows/test.yml`. Run it after any change
that touches more than one file, and always after a rename (D31):

```sh
for pair in $(grep -v '^#' tools/systems.txt); do GOOS=${pair%/*} GOARCH=${pair#*/} go vet ./... || break; done
```

A corpus is regenerated from its C probe with `go test -update` in its
package. That needs a C compiler and `isocline/`. `go test ./internal/capture
-update` records the sessions, on Linux or macOS only.

The readline corpora come from `tools/probe-readline.c`, which links the GNU
readline 8.3 of the system. Record them on Linux, with readline's headers
installed:

```sh
tools/build-probe-readline.sh
go test ./internal/readline -args -update
```

The probe runs each case in a process of its own, with no inputrc, and
records what each key does to the line, the cursor and the mark. A new
behaviour of readline gets a case in the probe first, and the Go code is
written to match the recording.

`tools/mutate.sh <file> <find> <replace> [test pattern]` breaks the code on
purpose and says whether a test caught it. It tells "caught", "NOT CAUGHT"
and "DID NOT COMPILE" apart.

## Go conventions the code follows

Errors:

- A sentinel error is a constant of type `Error`, a string type. Its text is
  its name without `Err`, split into words: `ErrClosed` reads "closed".
  Context goes in the wrapping where the error is returned (D17).
  `TestErrorsAreConstants` holds this.
- An error is wrapped with `%w` and a phrase that says what was being done:
  `fmt.Errorf("switching the terminal to raw mode: %w", err)`. A caller
  compares with `errors.Is`, never with `==`.
- A deferred close of a file opened for reading is not checked. A close of a
  file opened for writing is checked (D29).

The API:

- `New` takes options. Each option is `With` and the name of what it sets.
  A switch is named for what it turns on, and its zero value is off (D26).
  The parameter of an option has the name of the field it sets (D27).
- The public API is shaped for Go and is not a translation of the C
  (D11, D14).

The code:

- The header comment of a file says what it holds, and which isocline file
  it was ported from: "Ported from isocline/src/completions.c".
- The port keeps the behavior of the C, even where it is wrong, because the
  C's recorded output is the corpus (D4). A departure carries a comment where
  the code makes it, and is listed in [docs/PORT.md](docs/PORT.md).
- A receiver is one or two letters from its type name, the same on every
  method. Two types break this, and [docs/BACKLOG.md](docs/BACKLOG.md) holds
  them.
- A file is named for the axis it splits on, not for one system. Logic that
  is specific to a system and makes no system call has no build tag, so that
  every system compiles and tests it. [docs/LAYOUT.md](docs/LAYOUT.md) gives
  the four patterns in order of preference.

Tests:

- Tests are in the package they test, not in a `_test` package, except in
  `key`.
- A corpus is never edited by hand. Each one has a probe in `tools/` that
  made it, and `-update` makes it again.
- A missing corpus or recorded session is a failure, never a skip. When a
  test does skip, its message says what went unchecked.
- An expected value comes from the C, from a specification or from the
  intent. It never comes from running the code and copying what came out.
- Before you trust a new test or corpus, break the code it covers with
  `tools/mutate.sh`, once for each thing it is meant to hold, and make sure
  that each break fails it.
- Read the exit status of a command, not the tail of its output. A pipe
  reports the status of its last command. [docs/LESSONS.md](docs/LESSONS.md)
  records each time this went wrong.

## The documents

Only `README.md`, `AGENTS.md`, `CLAUDE.md` and `CONTRIBUTING.md` are in the
root. Every other document goes in `docs/`, and
`TestTheRootHoldsFourDocuments` holds that.

A decision is append only. A new one takes the next number, a heading such
as "### Dn. Title. Proposed." at the end of the decisions in
[docs/PLAN.md](docs/PLAN.md), and a row in the index at the top. When it
changes an earlier decision, its heading says "Amends" and the number of the
earlier one, and the heading of the earlier one says "Amended by" and the
number of the new one. `TestTheDecisionIndexIsComplete` and `TestAnAmendmentPointsBothWays`
hold both rules. Write `Decided` only when Ken decided.

When you finish an item from [docs/BACKLOG.md](docs/BACKLOG.md), delete it
there. When you find work that you do not do, add it there and say where it
came from.
