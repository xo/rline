# Port plan

This document records how `rline` becomes a pure Go port of isocline. isocline
is a readline replacement written in C by Daan Leijen, under the MIT license.
The C source sits in `isocline/`, which is a separate git repository. We do not
commit to it.

## Decisions

The port is one Go package named `rline`. Each C module becomes one or more
files in that package. The C headers contain a cycle, because `attr.c` includes
`term.h` and `term.h` includes `attr.h`. One Go package removes that cycle.

The port does not use cgo at any stage. cgo is the Go facility that calls C
code. An earlier plan built a cgo binding layer first, to get a reference to
compare against. We dropped that step for two reasons. The binding code gets
discarded at the end. The completion and highlight callbacks cost real work to
pass across the boundary.

A module is the unit of work, not a function. isocline passes its own allocator
into most structures, as the type `alloc_t`. A module keeps that ownership in
one place, so a module ports cleanly and a single function does not.

## Platforms

`rline` must run on Linux, macOS and Windows. On Windows it must work in both
`cmd.exe` and PowerShell. Linux is the only target for now, to keep the work
simple. macOS and Windows come later, on hosts that run those systems.

The capture harness follows the same rule. It records on Linux only. On every
other platform `Record` returns `ErrUnsupported`. macOS needs a pseudo-terminal
opened with `posix_openpt`. Windows has no pseudo-terminal device file, so it
needs a pseudo console, which it creates with `CreatePseudoConsole`.

## Port order

The C headers give an acyclic order. Port the modules from the leaves up:

1. `common.c` and `common.h`. Done. The allocator, the `memmove` wrappers and
   the `strlen` wrappers are gone, because Go collects garbage and carries the
   length of a slice. What survives is the QUTF-8 codec in `qutf8.go` and the
   ASCII case rules in `ascii.go`. Neither matches the standard library.
   `tools/build-probe.sh` builds a probe that prints what the C functions
   return, and `testdata/common.txt` records 18576 of those calls.
2. `wcwidth.c`. This is the wcwidth function of Markus Kuhn. It returns 0 for
   combining marks and for control characters.
3. `stringbuf.c`. This is a growable buffer that moves the cursor by character,
   not by byte.
4. `tty.c` and `tty_esc.c`. This reads the terminal and decodes escape
   sequences.
5. `attr.c`. This holds the text attributes.
6. `term.c` and `term_color.c`. This writes to the terminal and reduces colors
   to what the terminal accepts.
7. `bbcode.c` and `bbcode_colors.c`. This parses markup such as
   `[red]text[/red]`.
8. `history.c`, then `undo.c`.
9. `completions.c`, then `completers.c`.
10. `highlight.c`.
11. `editline.c`. This is the edit loop and the key dispatch. The files
    `editline_help.c`, `editline_history.c` and `editline_completion.c` are
    textual includes of `editline.c`, not separate units.
12. `isocline.c`. This is the public API. API means Application Programming
    Interface.

`wcwidth.c` is a textual include of `stringbuf.c`, so the two port together.

## Test method

The C code has no unit tests. The `test/` directory holds two demo programs.
The test corpus comes from recorded bytes instead.

`tools/build-demo.sh` builds the isocline demo into `.build/`. It never writes
inside `isocline/`, because `isocline/` is a separate git repository that this
project does not change. `isocline/src/isocline.c` includes every other source
file, so one `gcc` command builds the whole library.

`internal/capture` records a session. It opens a pseudo-terminal, starts the
demo on it, sends the input of the session, and returns every byte that the
demo wrote. It fixes the terminal size, the `TERM` variable, the home directory
and the working directory, so that the output does not change between runs.

The recordings live in `internal/capture/testdata` as golden files. A golden
file holds the expected output of a test. `go test ./internal/capture -update`
rewrites them. `go test ./internal/capture` compares against them.

The golden files are escaped text, not raw bytes, so that a difference is
readable. The escape byte becomes `\e`, other control bytes become `\xNN`, and
a real newline follows every `\r` and `\n`.

Two limits are known. The leader side of the pseudo-terminal must be opened
non-blocking, or the Go poller never sees it and a read deadline never fires.
The escape decoder uses a timeout to tell the Escape key from an escape
sequence, so a clock must be injected before the timing tests are written.

Add fuzz tests for the escape decoder and for the markup parser. A fuzz test
feeds random input to find errors. Run the C code and the Go code on the same
random input, and compare. Turn each difference into a test.

Pin the Unicode version that the width tables use. The golden files change when
that version changes.

## Known departures from the C code

The port keeps the behavior of the C code, even where that behavior is wrong,
because recorded output from the C build is the test corpus. Each departure
carries a comment where the code makes it. Three are known so far.

The QUTF-8 decoder refuses the byte pair `0xED 0x80`, which encodes U+D000 to
U+D03F. Those are ordinary characters. The test that refuses UTF-16 surrogate
halves is one value too wide.

The QUTF-8 encoder does encode a surrogate code point, and the decoder will not
read one back. The encoder and the decoder disagree.

Case-insensitive comparison compares C `char` values, and `char` is signed on
x86 and on x86-64. Any byte above 0x7f therefore sorts before every ASCII
character. `char` is unsigned by default on ARM, so the C code sorts completions
differently there. The port keeps the x86 order, because that is where the
corpus comes from. This needs a decision once the port runs on ARM.

## Checks

Three checks run on the Go code:

1. `gofmt -l .` names any file that is not formatted.
2. `go vet ./...` reports suspicious code.
3. `go tool golangci-lint run ./...` runs the linters that `.golangci.yml`
   names.

`go.mod` pins golangci-lint with a `tool` directive, so `go tool` builds it
with the toolchain of this module. A golangci-lint binary built elsewhere can
fail to read the export data of a newer Go release, and then it reports every
standard library import as an error.

## Open questions

Four questions have no answer yet.

First, does `rline` adopt `github.com/xo/terminfo`? isocline contains no
terminfo code. `term.c` reads the `TERM`, `COLORTERM`, `NO_COLOR`,
`WT_SESSION`, `ITERM_SESSION_ID` and `VSCODE_PID` variables to pick a color
palette. terminfo is therefore new work, not saved work.

Second, does `rline` adopt `github.com/xo/inputrc`? isocline never reads an
inputrc file, because it carries fixed key bindings. inputrc is a new feature.

Third, which package measures character width? `github.com/mattn/go-runewidth`
matches the C behavior. `golang.org/x/text/width` does not, because it gives
East Asian width only and returns no zero width for combining marks.

Fourth, how does `isocline/` reach another host? It is a separate git
repository, so this repository ignores it for now.

## License

isocline is MIT licensed, and the copyright belongs to Daan Leijen, 2021. Carry
that notice into the Go source files that derive from the C source.
