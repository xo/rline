# Port plan

This document records how `rline` becomes a pure Go port of isocline. isocline
is a readline replacement written in C by Daan Leijen, under the MIT license.
The C source sits in `isocline/`, which is a separate git repository. We do not
commit to it.

## Decisions

The port is one Go package named `rline`. Each C module becomes one or more
files in that package. The C headers contain a cycle, because `attr.c` includes
`term.h` and `term.h` includes `attr.h`. One Go package removes that cycle.

The one exception is `key`, which holds the key codes from `tty.h`. A program
that binds keys or reads them names those codes, so they are exported from
their own package and read as `key.Up`, `key.CtrlA` and `key.F(5)`. `key`
imports nothing from `rline`, so it adds no cycle. Everything else stays
unexported in `rline` until `isocline.c` is ported and the public API is
decided.

The port takes two dependencies. `github.com/mattn/go-runewidth` measures
character width, in place of the table in `wcwidth.c`. `golang.org/x/sys/unix`
reads and writes the terminal settings, because the standard `syscall` package
does not name `TCSETSF` on Linux, so `termios` cannot be done there with the
standard library alone.

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

The capture harness records on Linux and on macOS. On every other platform
`Record` returns `ErrUnsupported`. Windows has no pseudo-terminal device file,
so it needs a pseudo console, which it creates with `CreatePseudoConsole`.

`record_unix.go` holds everything the two systems share. Only `openPTY`
differs, and it sits in `record_linux.go` and `record_darwin.go`. Both open
`/dev/ptmx`. Linux then unlocks the pair, asks for its number, and builds the
name as `/dev/pts/N`. macOS grants the follower to the user, unlocks it, and
asks for the whole name, which looks like `/dev/ttys003`. The two also report
the end of the stream differently: Linux fails the read with `EIO`, and macOS
returns `io.EOF`.

The golden files stay Linux only, because isocline itself writes different
bytes on macOS. `term_update_ansi16` in `term.c` is guarded by
`#if __APPLE__`, so on macOS the demo asks the terminal for its color palette
with an OSC 4 sequence and waits for an answer. A bare pseudo-terminal never
answers, so the demo waits out its timeout, which both adds the query to the
output and pushes the rest of the startup text into the next exchange. On
Linux the query is not compiled in at all, because `GIO_CMAP` is.

`TestRecord` therefore records every session on macOS and checks that it
produced output, but compares the bytes only on Linux, and `-update` refuses
to rewrite a golden file from the wrong system. Recording is stable on macOS,
so `TestRecordIsStable` runs there as well. Per platform golden files are
possible later. They are not needed while Linux is the reference.

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
4. `tty.c` and `tty_esc.c`. The decoding half is done. `tty_esc.c` is ported
   whole, in `ttyesc.go`. From `tty.c` what is ported is the key codes, now
   the exported `key` package, and the reader in `tty.go`: the two pushback
   buffers, the UTF-8 assembly, the dispatch, and the rewriting of the keys
   that terminals disagree about. `tools/build-probe-tty.sh` builds a third
   probe, and `testdata/tty.txt` records 7157 decodes.

   The terminal itself is done too, in `ttydev_unix.go` with the per system
   requests in `ttydev_linux.go` and `ttydev_darwin.go`: raw mode through
   `termios`, the UTF-8 test, the resize event, interrupting a read, and
   `tty_read_esc_response`. Windows is still `errUnsupported`.

   That half needs a real terminal, which a corpus cannot give, so
   `TestTTYDeviceOnARealTerminal` opens a pseudo-terminal through
   `capture.OpenPTY`, goes into raw mode, checks that echo and line mode and
   the signal keys are off, reads keys through the decoder, and checks the
   terminal is put back. `tty_read_esc_response` is in the corpus, because it
   reads from the same byte source as the decoder.
5. `attr.c`. Done. The attributes are a Go struct of comparable fields in
   `attr.go`, rather than the 64 bit union of bit fields the C packs them into,
   because Go compares a struct with `==` and nothing outside `attr.c` depends
   on the packed value. The colors came with it, in `color.go`, because the SGR
   parser needs them: `term_color.c` holds `ic_rgb`, `ic_rgbx` and
   `color_from_ansi256`, and the 256 color table was extracted from the C
   source rather than typed out. `tools/build-probe-attr.sh` builds a fourth
   probe, and `testdata/attr.txt` records 1678 of those calls.
6. `term.c` and `term_color.c`. Done, apart from Windows. `termcolor.go` holds
   the color reduction, which finds the nearest color a terminal can show when
   it understands fewer than a style asks for. `term.go` holds the terminal
   itself: the writer, the buffering, cursor movement, the attribute state, and
   working out the size and how much color the terminal supports.
   `tools/build-probe-termcolor.sh` and `tools/build-probe-term.sh` build the
   sixth and seventh probes. `testdata/termcolor.txt` records 5522 calls and
   `testdata/term.txt` records a script of 256 steps, driven through a real
   pseudo-terminal because the C asks the terminal for the cursor position when
   it cannot get the size any other way.

7. `bbcode.c` and `bbcode_colors.c`. Done. `bbcode.go` turns markup such as
   `[red]text[/red]` into text plus one attribute for every byte of it, and
   `bbcodecolors.go` holds the 172 HTML color names, extracted from the C
   source rather than typed out. `tools/build-probe-bbcode.sh` builds the
   eighth probe, and `testdata/bbcode.txt` records 79 pieces of markup, each
   one parsed, measured and printed.
8. `history.c` and `undo.c`. Done. `history.go` holds the list of lines the
   user typed and the file it is kept in, and `undo.go` the stack of saved
   lines that stepping back through edits uses. The C keeps the list in a
   fixed array with its own count; a Go slice carries both.
   `tools/build-probe-history.sh` builds a fifth probe, and
   `testdata/history.txt` records 424 cases, including the escaping of every
   byte on its own.
9. `completions.c` and `completers.c`. `completions.c` is done, in
   `completions.go`: the list of what the user could type, how much of the
   line each one takes away on either side of the cursor, the order the menu
   shows them in, and filling in the longest start they all share. From
   `completers.c`, word and quoted word completion are done in
   `completers.go`, which is the part that works out which word the completer
   should see and puts the quoting back on what comes back.

   `tools/build-probe-completions.sh` builds a sixth probe, and
   `testdata/completions.txt` records 1635 cases, 302 of them word and quoted
   word completion over every combination of quote, escape and character
   class.

   Completing a file name is done as well, in `filenames.go`: reading a
   directory, matching an extension, working out the type of an entry, and
   colouring it from `LS_COLORS` or `LSCOLORS`.

   That is the first part of the port that reads the world rather than a
   string, so the shape of its test is new. `tools/probe-filenames.c` builds
   a fixed tree in a temporary directory, changes into it, and completes
   against it, so nothing it prints holds the temporary path. The Go test
   builds the same tree and does the same.
   `testdata/filenames.txt` records 540 cases. Two things cannot go in it:
   directory order, which the file system decides and which both sides sort
   away, and the type of a real entry, which the corpus passes in rather than
   works out. `TestTypeOfRealEntries` covers that against a real tree, and
   skips the file types the system will not let a test make.
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

The port does not catch `SIGSEGV`, `SIGTRAP` or `SIGBUS`. isocline catches
them so that it can put the terminal back before the program dies. The Go
runtime owns those signals, needs them to print a stack trace, and taking them
over would break more than a tidy terminal is worth. The signals that a
program can be expected to survive, from `SIGTERM` to `SIGTTOU`, are caught,
and the terminal is restored before the signal is passed on.

`tty_read_esc_response` returns nothing when it fails. The C code fills its
buffer as it goes and writes the terminating zero only once it succeeds, so a
failure leaves bytes there that are not a string. No caller may read them, and
`term.c` does not.

There is no `setlocale` in Go, so `localeIsUTF8` reads `LC_ALL`, `LC_CTYPE`
and `LANG` in the order that `setlocale` reads them, and applies the same
test. An environment that sets none of them leaves the locale at `C`, which
the C code counts as UTF-8.

File name completion reads the colour settings from the environment each
time rather than once. The C code keeps the first answer for the life of the
program, so a program that sets `CLICOLOR` after the first completion never
sees it. Reading each time is the same for any program that sets them before
it starts, and it is what makes the behaviour testable.

File name completion carries its settings in a closure. The C code puts them
in the field that holds the program's own argument, and leaves that field
pointing at a dead stack value once it returns.

A completion with an empty display shows its replacement. The C code keeps an
absent display apart from an empty one, and shows the empty one, which draws a
blank line in the menu. A Go caller cannot pass the absence of a string, so
the two are the same here. This is the only recorded case where the port
answers differently on purpose, and the test says so where it checks it.

The history file is created with the mode that keeps it to its owner, rather
than created and then tightened. The C code calls `fopen` and then `chmod`,
which leaves a moment where a file holding everything the user typed can be
read by anyone. Setting the mode at creation closes that, and the `chmod`
stays so that a file which already existed with a looser mode is tightened as
well, which the C code never does. A file mode cannot appear in terminal
output, so no recorded session can tell the difference.

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

These came out of writing the probes. The port does what each function means
to do, and says so in a comment. All but one are unreachable or invisible, so
they change no recorded session. The exception is the deletion fault in
`attr.c`, which has a live caller, and `testdata/attr-delta.txt` records what
the C does there.

### stringbuf.c

Three faults.

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

### attr.c

Two faults, and the first one is live.

`attrbuf_delete_at` hands `ic_memmove` a count of attributes where every other
call in the file hands it a count of bytes. An attribute is eight bytes, so the
deletion shifts one eighth of what it should and leaves stale attributes behind
everything it moved. `bbcode.c` calls it twice, at lines 670 and 682, so this
reaches real output.

The port does not reproduce it, and the reason is not only that it is wrong.
`attr_t` is a union of C bit fields, so which bytes the fault leaves behind
depends on how a compiler packs those fields. The wrong answer is therefore not
portable, which makes it useless as a reference. `testdata/attr-delta.txt`
records what the C build on this host returns, and `TestAttrPortMatchesC`
allows a difference only in those cases. A difference anywhere else fails.

`attrbuf_attr_at` tests `pos > count` where it means `pos >= count`, so at the
position just past the end it reads a slot it never wrote. That is undefined
behavior rather than a wrong answer, so there is nothing to reproduce at all.
The port returns the empty attribute there.

### bbcode.c

Three faults, and the first two both reach real output.

`attr_update_bool` uses each `strcmp` as a truth value instead of testing it
against zero, and `strcmp` answers zero when the two strings are equal. The
first branch is therefore taken for every value, so `[bold=off]` turns bold on,
and so does `[bold=false]` and `[bold=0]`. The port keeps this, because the
banner in the demo is written in this markup and the recorded sessions hold
what it produces.

Every tag name is thrown away. `attr_update_with_styles` means to record the
name on the tag, but it guards the assignment with a test on the field it is
about to write rather than on the value it is writing, and the field starts as
a null pointer. So the name is never stored, `bbcode_close` matches the first
tag it pops whatever it is called, and the whole unbalanced-tag branch below it
is unreachable. `[b][i]x[/b][/i]` therefore behaves the same as
`[b][i]x[/i][/b]`.

The character classes for a name and for a value disagree about upper case. A
name takes A to Z, and an unquoted value stops at F, so `[color=RED]` reads as
an empty value while `[RED]` reads as the color. Quoting the value avoids it.

### term.c

Two faults and one thing that only looks like one.

`term_is_interactive` passes its two arguments to `strstr` the wrong way round,
so it asks whether `TERM` is part of the list rather than whether the list
holds `TERM`. The names it means to catch do answer yes, but so does any piece
of one, such as `umb` or `25|CONS`, and so does an empty `TERM`, because an
empty string is part of every string. The port keeps this, because changing it
would change which terminals the editor refuses to run on. `gocritic` finds it
on the Go side too, so the line carries a `nolint` that says why.

`term_update_ansi16` reads the sixteen palette colors from the Linux console
with `GIO_CMAP` and then writes them to `ansi256[i]` where `i` steps by three,
so it scatters them across indices 0, 3, 6 and so on up to 45, which overwrites
part of the color cube and fills in only six of the sixteen it meant to. It
fires only on a Linux virtual console, because the request fails on a
pseudo-terminal, so no recorded session reaches it.

`term_set_attr` looks as though it forgets to store what it just set, and it
does not. The state is kept by the write path: setting an attribute means
writing an escape sequence, and `term_append_esc` reads every such sequence
that goes past and folds it into the stored attributes. The two assignments
inside `term_set_attr` cover the one case that defeats this, which is a color
the palette cannot show, where the sequence names the nearest color instead and
reading it back would record the approximation.

### completers.c

`ls_valid_esc` is defined and never called. It looks like it was meant to
check that a colour setting holds only the escape codes that are safe to send
on, which nothing does, so a setting from the environment reaches the terminal
unchecked.

### tty.c

`tty_cpush` guards the byte pushback buffer with the length of the *code*
pushback buffer rather than its own, so the byte buffer can overrun. Nothing
reaches it, because the decoder never pushes back more than three bytes at
once. The port checks the buffer it is about to write to.

`tty_readc_noblock` asks `FIONREAD` about file descriptor 0 rather than about
its own. That is right only because isocline reads from standard input.

`tty_read_timeout` returns its `code_t*` argument where its result type is
`bool`. The pointer is never null, so it always means true, which is the
answer it wants. The port returns a second value instead.

### history.c

`history_search` hands the entry at each index straight to `strstr` without
checking that the index names one. `history_get` answers with a null pointer
outside the list, so a starting index at or past the count reads through it
when the search runs forwards. The port stops at the end of the list and
answers that it found nothing.

The walk that removes an older duplicate does not step back after it removes
one, so when two neighbours both match the new entry only the first goes. The
port keeps the same walk, so that the two agree. Reaching it needs duplicates
already in the list, which happens only when they were allowed earlier or came
from the file.

Two things about the file format are worth knowing rather than fixing. An
entry that escapes to nothing writes no line at all, so an empty entry is not
kept. And `\x00` reads as nothing, because the buffer it is appended to stops
at a zero byte, so a line holding only that counts as empty and is skipped.

`tty_readc_noblock` promises not to change the byte when nothing arrives, and
usually does not, because a quiet terminal is not readable and the read never
runs. But `tty_readc_blocking` clears the byte before it reads, so if the read
does run and fails, the byte comes back as zero. That decides what `ESC [`
alone decodes to: alt and `[` on a terminal, alt and NUL through a source that
is always readable. `tools/probe-tty.c` uses an idle pipe rather than
`/dev/null` for that reason, and the comment there explains it.

## Windows

Windows support for `tty.c` is deliberately not written yet, and it waits for a
Windows host. The console API reads key events and needs its own pushback,
which is a rewrite rather than a port, and the capture harness cannot record on
Windows either. Code that nobody can run and no corpus can check is worse than
a gap that is written down. `ttydev_other.go` returns an error there. This
belongs to whoever owns `tty.c` once a host exists.

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
