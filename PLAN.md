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
2. `wcwidth.c`. Not ported. `width.go` calls `github.com/mattn/go-runewidth`
   instead. `testdata/wcwidth.txt` records the width that the C code gives
   every code point, and `testdata/wcwidth-delta.txt` records the 150 ranges
   where go-runewidth answers differently. `TestWidthDelta` keeps that record
   current, so an upgrade of go-runewidth shows up as a change to a committed
   file.
3. `stringbuf.c`. Done. This is a growable buffer that moves the cursor by
   character, not by byte, together with the width, navigation and row and
   column code that the edit loop draws from. The allocator and the growth
   policy are gone, because a Go slice grows on demand. What survives is in
   `strwidth.go`, `strfind.go`, `charclass.go`, `rowcol.go`, `parse.go` and
   `stringbuf.go`. `tools/build-probe-stringbuf.sh` builds a second probe, and
   `testdata/stringbuf.txt` records 76164 of those calls.
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

`wcwidth.c` is a textual include of `stringbuf.c`. Since it is not ported,
`stringbuf.c` ports on its own and calls `runeWidth`.

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

`testdata/stringbuf-delta.txt` holds the recorded calls whose answer differs
because the port measures width with go-runewidth. There are 86 of them, all
for the two byte form of U+0080, which the C table calls width -1 and
go-runewidth calls width 0. `TestStringbufPort` fails a width difference in any
string that holds no disputed character, so the file cannot grow to cover a
port bug.

## Known departures from the C code

The port keeps the behavior of the C code, even where that behavior is wrong,
because recorded output from the C build is the test corpus. Each departure
carries a comment where the code makes it.

The QUTF-8 decoder refuses the byte pair `0xED 0x80`, which encodes U+D000 to
U+D03F. Those are ordinary characters. The test that refuses UTF-16 surrogate
halves is one value too wide.

The QUTF-8 encoder does encode a surrogate code point, and the decoder will not
read one back. The encoder and the decoder disagree.

Case-insensitive comparison compares C `char` values, and the result depends on
whether `char` is signed. Any byte above 0x7f sorts before every ASCII
character when it is signed, and after it when it is unsigned.

This is settled. `char` is signed on x86-64, and signed on Apple arm64, which
the macOS host confirmed by compiling and running a test program. The whole
corpus was regenerated on that host and matched all 18576 lines with no
difference. The port keeps the signed order.

The cause is the ABI, not the architecture. Apple's arm64 ABI specifies signed
`char`. The Linux AArch64 ABI makes it unsigned. So a Linux arm64 host would
produce a different corpus, and that case is untested.

## The width table

go-runewidth is newer than the table in `wcwidth.c`, and the two disagree for
3877 code points out of 1114112. Most of the disagreement is go-runewidth
being right about characters that Unicode added after the C table was written.
Three parts need a decision, and none is settled.

U+1160 to U+11FF are Hangul Jamo vowels and final consonants. The C table
gives them width 0, because they combine with the syllable in front of them.
go-runewidth gives them width 1. Korean text composed from Jamo will therefore
place the cursor differently.

U+E0020 to U+E007F are the tag characters, which carry the region letters
inside a flag emoji. The C table gives them width 0 and go-runewidth gives
them width 1.

U+D800 to U+DFFF are the UTF-16 surrogate halves, and they account for 2048 of
the 3877. They cannot appear in text that the decoder accepts, so this part
does not matter in practice.

## Bugs found in the C code

Three faults in `stringbuf.c` came out of writing the probe. None of them
changes a recorded session, so none of them is a departure. The port does what
each function means to do, and says so in a comment.

`sbuf_split_at` sets the new length of the left buffer and never writes the
terminating zero. The left buffer then stops being a valid C string, and
`sbuf_string` asserts on it in a build that keeps assertions. Nothing in
isocline calls the function.

`sbuf_strdup_from_utf8` allocates one byte for each byte of the buffer, and
then writes a terminating zero at the index it stopped at. A buffer that holds
only single byte characters stops at the length, so the write lands one byte
past the end of the allocation. The Go port has no terminator to write.

`skip_esc` says yes to every byte that follows an escape, because the branch
for the sequences that run to a terminator falls through when it never reaches
one, and the two branches below it do the same thing as each other. The test
on the set `" #%()*+"` therefore changes nothing, and an unterminated `ESC [`
counts as two bytes.

Two more are quirks rather than faults, and the port keeps both. A zero byte
passed to `strchr` finds the terminating zero of the set, so a zero byte is a
separator and enters the escape branch. `str_prev_ofs` tests its pointer
against null, so an empty buffer, which holds a null pointer, answers
differently from an empty string, which does not.

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

Three questions have no answer yet.

First, does `rline` adopt `github.com/xo/terminfo`? isocline contains no
terminfo code. `term.c` reads the `TERM`, `COLORTERM`, `NO_COLOR`,
`WT_SESSION`, `ITERM_SESSION_ID` and `VSCODE_PID` variables to pick a color
palette. terminfo is therefore new work, not saved work.

Second, does `rline` adopt `github.com/xo/inputrc`? isocline never reads an
inputrc file, because it carries fixed key bindings. inputrc is a new feature.

Third, how does `isocline/` reach another host? It is a separate git
repository, so this repository ignores it for now.

## License

isocline is MIT licensed, and the copyright belongs to Daan Leijen, 2021. Carry
that notice into the Go source files that derive from the C source.
