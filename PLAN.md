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

## Which systems are supported

Three are tested: Linux, macOS and Windows. Each has a session running the
full suite, and a person has driven the editor by hand on each.

Seven more compile and are not tested: FreeBSD, NetBSD, OpenBSD, Dragonfly,
Solaris, illumos and the mobile variants that Go folds into darwin and linux.
The Unix file is tagged `unix && !aix` and the ioctl numbers they need sit in
`tty_bsd.go` and `tty_sysv.go`. Nothing else in the terminal layer
asks anything of the system that `golang.org/x/sys/unix` does not answer per
system.

Compiling is not support, and this list says which is which rather than
leaving a user of FreeBSD to guess. DeepSeek argued for keeping the narrow
tag on exactly that ground. It also named the risk that would justify it,
which is code that hardcodes the index of VMIN or VTIME in the control
character array, since Linux puts them at 6 and 5 while the BSDs put them at
16 and 17. That risk was measured rather than accepted: the code reads
`unix.VMIN`, and `x/sys/unix` defines it as 0x6 on Linux, 0x10 on the BSDs
and 0x4 on Solaris. So the named risk does not apply here.

FreeBSD, NetBSD and illumos are measured rather than compiled, as of
2026-09-20. Ken ran all three as incus virtual machines on the Linux host.

FreeBSD 15.1 with its own Go 1.25.14: 327 tests, no failures. NetBSD 11.0
with Go 1.26.5 from pkgsrc: 325 passed, 14 skipped, no failures. OmniOS
r151058 with Go 1.25.12 from its own repository: 326 passed, 13 skipped, no
failures. All three built from source on the machine rather than from a
binary cross-compiled for them.

That settles the risk DeepSeek named when it argued against widening the tag.
`unix.VMIN` is 0x6 on Linux, 0x10 on the BSDs and 0x4 on illumos, three
different values, and the suite passes on all of them.

illumos is the one that earned its console time. It is the only one that
takes `tty_nosti.go`,
since FreeBSD and NetBSD both have `TIOCSTI`. Those two files had nothing
behind them before it.

Neither can record sessions, so `TestGoldenSetIsPresent` fails on both as it
does on Windows. That is the expected gap and not a regression.

Each of the two found a fault in a test rather than in the port, and both
were the same mistake.

illumos failed `TestTypeOfRealEntries` on a case whose comment read "a
character device that every system has", naming `/dev/null`. On illumos that
is a symlink to `../devices/pseudo/mm@0:null`, and `typeOf` uses `Lstat` as
the C does, so it correctly answered symlink. The test follows the path to
whatever it really is now, checks the symlink case where the system has one,
and says what it skipped where it does not.

That was the third such claim, after a path through a file on Windows and a
directory that reads as data on NetBSD. Three systems, three tests, one
mistake: a universal claim about filesystems, written in a comment, which is
the sentence nobody re-reads.

NetBSD's was the second. It
opened a directory to reach the reading half of `history.load`, on the
premise that opening one succeeds and reading it fails. Linux and macOS do
that. NetBSD reads a directory as data, so both halves succeed and the test
failed. It measures the behavior now and says what went unchecked when the
behavior is not there. The first instance was a path through a file, which
reads as a missing path on Windows.

Neither machine reaches `incus exec`. FreeBSD and NetBSD have no
virtio-vsock driver, so the incus agent runs in the guest with no transport
and the host times out rather than being refused. SSH on port 22 is the way
in, with a key added at the console.

What a virtual machine would still add: the twelfth device test. Thirteen of
the fourteen test files build for these systems, and the terminal layer has
eleven of its twelve tests there.

The twelfth is `TestTTYOnARealTerminal`, which needs a pseudo-terminal.
`capture.OpenPTY` is written for Linux and macOS, and FreeBSD opens one
differently again. It lives in `tty_pty_test.go` under `linux || darwin`
for that reason, so the eleven that use pipes are not held back by the one
that does not.

Widening the device tests is what found this. The tag on `tty_test.go` was
still `linux || darwin` after its source widened, so the layer the widening
enabled had no tests at all on the systems it enabled it for. That is the
same shape as the check that left the build, and it was one line of my own
work old when it was found.

Recording sessions does not reach FreeBSD either. `internal/capture` is
`linux || darwin` and FreeBSD takes the stub, so there is no fourth golden
set and no need for one: the corpus is recorded from the C, and the C build
would have to run there too.

Anything else, including aix, falls to `tty_other.go`. It reads plain
lines with no editing and says the terminal is unsupported, which is a mode
rather than a failure.

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

The golden files are kept per system, in `internal/capture/testdata/<goos>/`,
because isocline itself writes different bytes on each. `term_update_ansi16`
in `term.c` is guarded by `#if __APPLE__`, so on macOS the demo asks the
terminal for its color palette with an OSC 4 sequence and waits for an answer.
A bare pseudo-terminal never answers, so the demo waits out its timeout, which
both adds the query to the output and pushes the rest of the startup text into
the next exchange. On Linux the query is not compiled in at all, because
`GIO_CMAP` is.

Every system compares bytes, and a set that is missing for the system the test
runs on is a failure rather than a skip. A skip would put back the hole this
layout exists to close. `go test ./internal/capture -update` records the set
for whichever system it runs on.

Recording is timing sensitive in one place, and the fix belongs with the
session rather than with the harness. The demo cannot tell the Escape key from
the start of an escape sequence without waiting, and it waits 200ms on macOS
against 100ms on Linux. The quiet period that ends a step is 200ms, so on
macOS the two are equal and the wait can end in the middle of what the demo is
writing: the same bytes then land in two exchanges on one run and one on the
next. The `completion-menu` session, which is the only one that sends Escape,
waits 600ms on that step. Six runs in a row agree with that, where one in
three failed without it.

## Port order

The C headers give an acyclic order. Port the modules from the leaves up:

1. `common.c` and `common.h`. Done. The allocator, the `memmove` wrappers and
   the `strlen` wrappers are gone, because Go collects garbage and carries the
   length of a slice. What survives is the QUTF-8 codec and the ASCII case
   rules, both in `internal/text`. Neither matches the standard library.
   `tools/build-probe.sh` builds a probe that prints what the C functions
   return, and `internal/text/testdata/common.txt` records 18576 of those calls.
2. `wcwidth.c`. Not ported. `internal/text/text.go` calls `github.com/mattn/go-runewidth`
   instead. `internal/text/testdata/wcwidth.txt` records the width that the C code gives
   every code point, and `internal/text/testdata/wcwidth-delta.txt` records the 150 ranges
   where go-runewidth answers differently. `TestWidthDelta` keeps that record
   current, so an upgrade of go-runewidth shows up as a change to a committed
   file.
3. `stringbuf.c`. Done. This is a growable buffer that moves the cursor by
   character, not by byte, together with the width, navigation and row and
   column code that the edit loop draws from. The allocator and the growth
   policy are gone, because a Go slice grows on demand. What survives is in
   `internal/text`, which is where all six of the C files it came from
   landed. `tools/build-probe-stringbuf.sh` builds a second probe, and
   `internal/text/testdata/stringbuf.txt` records 76164 of those calls.
4. `tty.c` and `tty_esc.c`. The decoding half is done. `tty_esc.c` is ported
   whole, in `decoder.go`. From `tty.c` what is ported is the key codes, now
   the exported `key` package, and the reader in `decoder.go`: the two pushback
   buffers, the UTF-8 assembly, the dispatch, and the rewriting of the keys
   that terminals disagree about. `tools/build-probe-tty.sh` builds a third
   probe, and `testdata/tty.txt` records 7157 decodes.

   The terminal itself is done too, in `tty_posix.go` with the per system
   requests in `tty_sysv.go` and `tty_bsd.go`: raw mode through
   `termios`, the UTF-8 test, the resize event, interrupting a read, and
   `tty_read_esc_response`. Windows is still `errUnsupported`.

   That half needs a real terminal, which a corpus cannot give, so
   `TestTTYOnARealTerminal` opens a pseudo-terminal through
   `capture.OpenPTY`, goes into raw mode, checks that echo and line mode and
   the signal keys are off, reads keys through the decoder, and checks the
   terminal is put back. `tty_read_esc_response` is in the corpus, because it
   reads from the same byte source as the decoder.
5. `attr.c`. Done, and split between the two packages. `ansi.Attr` is a Go
   struct of comparable fields, rather than the 64 bit union of bit fields the
   C packs them into, because Go compares a struct with `==` and nothing
   outside `attr.c` depends on the packed value. Reading an attribute out of an
   SGR escape sequence went with it, as `ansi.ParseSGR` and
   `ansi.ParseEscapeSGR`, because that is reading ANSI and needs nothing else.
   `ansi.AttrBuf`, the buffer of one attribute per byte, went with it. What
   stayed in `bbcode.go` is `appendMarked`, the one place that buffer meets
   rline's own text buffer, which is a join and so belongs where both are
   known. `tools/build-probe-attr.sh` builds a fourth probe. Its 1678 recorded
   calls are split the same way: 1669 in `ansi/testdata/attr.txt` and 9 in
   `testdata/attrbuf.txt`. Each package's `-update` runs the one probe and
   keeps the kinds it checks, and each fails if its corpus holds a kind
   belonging to the other. Measured rather than assumed: moving one line
   across the boundary fails both tests, and deleting one from either fails
   that one. What the guard does not catch is a line going missing from the
   middle of a kind that has a thousand of them, because it counts kinds
   rather than lines.
6. `term.c` and `term_color.c`. Done, apart from Windows. `ansi/ansi.go` holds
   the color reduction, which finds the nearest color a terminal can show when
   it understands fewer than a style asks for. `term.go` holds the terminal
   itself: the writer, the buffering, cursor movement, the attribute state, and
   working out the size and how much color the terminal supports.
   `tools/build-probe-termcolor.sh` and `tools/build-probe-term.sh` build the
   sixth and seventh probes. `ansi/testdata/termcolor.txt` records 5522 calls and
   `testdata/term.txt` records a script of 256 steps, driven through a real
   pseudo-terminal because the C asks the terminal for the cursor position when
   it cannot get the size any other way.

7. `bbcode.c` and `bbcode_colors.c`. Done. `bbcode.go` turns markup such as
   `[red]text[/red]` into text plus one attribute for every byte of it, and
   `ansi/names.go` holds the 172 HTML color names, extracted from the C source
   rather than typed out, because a color name is a color rather than markup. `tools/build-probe-bbcode.sh` builds the
   eighth probe, and `testdata/bbcode.txt` records 79 pieces of markup, each
   one parsed, measured and printed.
8. `history.c` and `undo.c`. Done. `history.go` holds the list of lines the
   user typed and the file it is kept in, and `internal/editor/editor.go` the stack of saved
   lines that stepping back through edits uses. The C keeps the list in a
   fixed array with its own count; a Go slice carries both.
   `tools/build-probe-history.sh` builds a fifth probe, and
   `testdata/history.txt` records 424 cases, including the escaping of every
   byte on its own.
9. `completions.c` and `completers.c`. `completions.c` is done, in
   `comp.go`: the list of what the user could type, how much of the
   line each one takes away on either side of the cursor, the order the menu
   shows them in, and filling in the longest start they all share. From
   `completers.c`, word and quoted word completion are done in
   `comp.go`, which is the part that works out which word the completer
   should see and puts the quoting back on what comes back.

   `tools/build-probe-completions.sh` builds a sixth probe, and
   `testdata/completions.txt` records 1635 cases, 302 of them word and quoted
   word completion over every combination of quote, escape and character
   class.

   Completing a file name is done as well, in `comp.go`: reading a
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
10. `highlight.c`. Done. `bbcode.go` holds the environment a highlighter
   marks a line through, and the brace matching that colors the brace under
   the cursor and its partner. `tools/build-probe-highlight.sh` builds the
   ninth probe, and `testdata/highlight.txt` records 2346 cases: every line in
   its corpus, against five sets of brace pairs, at every cursor position.
11. `editline.c`. Started. `internal/editor/editor.go` holds the state of a line being
    edited and every operation that changes it: moving the cursor, the eleven
    kinds of delete, swapping, inserting with the brace that closes itself,
    and the undo and redo stacks. `tools/build-probe-editline.sh` builds the
    tenth probe, and `internal/editor/testdata/editline.txt` records 4177 operations, each one
    run against thirteen lines at every cursor position.

    Each operation here changes the text and the cursor and nothing else. The
    C redraws at the end of every one, and the port leaves that to the caller,
    which is what lets an operation be checked without a terminal.

    The redraw is done too, in `prompt.go`, together with the environment
    type that carries everything the editor needs besides the line itself.
    `tools/build-probe-refresh.sh` builds the eleventh probe, and
    `testdata/refresh.txt` records 1584 redraws across ten lines, two prompts,
    a hint or none, three kinds of content shown below the line, and both
    settings of the indent and brace matching.

    The key dispatch and the main loop are done as well, in
    `prompt.go`, together with the hint, the resize and the reading of
    one line from start to finish.

    `editline_help.c`, `editline_history.c` and `editline_completion.c`,
    which are textual includes of `editline.c` rather than separate units,
    are done as well. `history.go` holds walking through the history
    and the incremental search that Ctrl-R opens, which draws its own prompt
    below the line and reads its own keys. `comp.go` holds offering
    completions, and `menu.go` the menu, which draws itself the same way.

    The completion menu cannot be driven from outside, because it reads its
    own keys: each recorded case loads the keys it types into the tty, calls
    the menu once, and compares what was drawn against what the C drew for
    the same keys. `tools/build-probe-completion-menu.sh` builds the probe
    and `testdata/<variant>/completion-menu.txt` records 3360 cases over
    fifteen sets of completions, fourteen key sequences, and both settings of
    the preview, the auto tab and whether the completer had more to offer.
    The recording is split by branch for the reason the redraw is: the menu
    draws through the same redraw, and 152 of these carry the wrapped row
    mark.

    That is the whole of `editline.c` and its includes.
12. `isocline.c`. Done in shape, and it is the one part that is not a
    translation. The C keeps a single environment in a process global and
    every public function reaches for it, so a program can have only one line
    reader and cannot say which terminal it is on. `rline.go` puts that state in
    a `Reader` that the caller makes, with the settings passed as options when
    it is made rather than as global switches flipped afterwards.

    `ReadLine` answers `io.EOF` where the C answers a null pointer, and reads
    a plain line with no editing when there is no terminal to edit on, which
    is what a program reading a pipe or a script gets.

    `example/` is the smallest program that uses the package, and
    `TestExampleRunsUnderATerminal` drives it through a pseudo-terminal. That
    is the only test that runs the package as a program rather than checking
    one piece against a recording, and it is what caught the missing raw mode
    described below.

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

The escape decoder is fuzzed against the C one. A fuzz test feeds generated
input to find errors, and this one asks both decoders the same question and
fails on any difference, because unlike the width tables there is no reason
for these two to disagree.

`tools/build-probe-ttyfuzz.sh` builds the C side as a server that takes one
case per line, rather than a process per case, which would be too slow to be
worth running. The channels need care. `tty_readc_noblock` asks `FIONREAD`
about file descriptor 0 rather than about its own, so the terminal is a pipe
put on file descriptor 0, and the commands arrive on file descriptor 3, which
the Go side passes in. Commands on standard input would make the decoder
believe terminal input was waiting and then block on an empty pipe.

The seeds are every sequence in `testdata/tty.txt`, so the fuzzer starts from
input that already reaches the interesting parts and spends its time on what
nobody wrote down. Running `go test` without `-fuzz` replays the seeds, which
is what a normal run does.

It found a real difference within a second, which is in the `tty.c` list
below. Since that was fixed, 6.3 million runs have found nothing else.

The corpora and the recorded sessions leave a gap between them: a corpus
checks that a function answers correctly, and a session checks that the whole
program works, but neither checks which key reaches which operation. Three
faults were found only by running the program, which is what that gap looks
like from the other side.

`driven_test.go` covers it. It feeds a string of keystrokes to the editor and
asserts on the line and the cursor that come out, which is the shape
python-prompt-toolkit uses in `tests/test_cli.py`. It is the only comparable
project with a test suite worth copying: GNU readline ships example programs
rather than tests, and linenoise has none. jline3 has a large one, and its
list of what it tests is where the rest of that file comes from: how a line
ends, input that is not UTF-8, characters of more than one byte, and a
completion list too long to show.

It also reaches one layer up, calling `editLine` rather than
the key loop, because what the caller is told apart from the text — the line
ended, the input ended — is a mapping of its own and was not covered.

The markup parser has no fuzz test yet.

Pin the Unicode version that the width tables use. The golden files change when
that version changes.

`internal/text/testdata/stringbuf-delta.txt` holds the recorded calls whose answer differs
because the port measures width with go-runewidth. There are 86 of them, all
for the two byte form of U+0080, which the C table calls width -1 and
go-runewidth calls width 0. `TestStringbufPort` fails a width difference in any
string that holds no disputed character, so the file cannot grow to cover a
port bug.

## Error values are constants

The errors this package returns are constants of a string type, following
`github.com/xo/tblfmt`, which Ken prefers and which this project now shares:

    type Error string
    func (err Error) Error() string { return string(err) }
    const ErrClosed Error = "closed"

The text of each one is its own name with the `Err` prefix taken off, split
where a word starts and lowercased: `ErrClosed` reads "closed", `ErrNoSteps`
reads "no steps". So the words a caller prints and the identifier they looked
it up by are the same words, and neither can drift from the other. Context
belongs in the wrapping at the place the error is returned, which is where it
knows what was being attempted — `record_unix.go` names the session, and
`tty_other.go` says that it was reading keys.

That rule is a test rather than a convention. `TestErrorsAreConstants` derives
the text from the name rather than writing it out, so the check is the rule
and not a copy of the answer, and the capture package has the same check.

A `var` holding an error is writable by anything that can see it, including
another package. An error value that changes underneath a caller comparing
against it is a fault nobody looks for and nothing reports. A constant cannot
be reassigned, and the compiler says so rather than the program going wrong
at run time.

`errors.Is` still finds one through a wrap, because a string type is
comparable and `%w` keeps the chain. What a constant does not buy is safety
in comparing with `==`: a wrapped error is not equal to what it wraps
whatever its type, so `errors.Is` is still the way to ask, and
`TestErrorsAreConstants` says so where a reader will meet it.

Two things follow from the type comparing by value. Two errors with the same
text are the same error, so the test checks that none of them share text.
And a constant declared where the building platform does not read it is one
the linter reports, which is why `errUnsupported` lives in `tty_other.go`
under its own build tag rather than beside the rest.

The Windows terminal layer used to have its own `errNotATerminal` reading
"not a console". There is now one value for the one idea, and it reads "not a
terminal" everywhere. Nothing asserts the text.

## Known departures from the C code

The port keeps the behavior of the C code, even where that behavior is wrong,
because recorded output from the C build is the test corpus. Each departure
carries a comment where the code makes it.

One departure is over a behaviour rather than a fault, and is the only one.
`ReadLine` answers `ErrInterrupted` when the user presses Ctrl-C or Ctrl-G,
where the C clears the line and hands back an empty string that no caller can
tell from Enter on an empty line. usql cannot work without that distinction.
Ken decided for the program over the C. See "What rline offers usql" below.

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

The Windows function keys F11 and F12 are sent as vt codes 23 and 24, where
the C sends 13 and 14. This is the one place the port cannot be faithful,
because the C contradicts itself: `tty_waitc_console` encodes F11 as 13, and
`esc_decode_vt` in the same program reads 13 as F4 and reads F11 from 23. So
pressing F11 on Windows under isocline gives F4, and F12 gives F5.
windows-vm confirmed that end to end against a real console before the port
was changed, by pushing key records into the console input buffer and reading
them back out through the decoder.

Being faithful to the encoder would mean being unfaithful to the decoder, so
there is no faithful answer to give. The decoder is the half with recorded
cases behind it on two systems, 23 and 24 are the numbers every other
terminal uses for those keys, and no recorded session anywhere carries the C
answer, because nothing records on Windows. The port therefore sends what the
decoder reads.

The history file is written with no restriction on who can read it. The C
code creates it with `fopen` and then calls `chmod` to make it owner only,
and the port leaves both out. That is a decision rather than an oversight:
the mode cannot be expressed on Windows, where `os.Chmod` maps only the owner
write bit onto the read-only attribute and drops the rest, and the history is
going to be rewritten, so guarding it now would be work thrown away twice. A
file mode cannot appear in terminal output, so no recorded session can tell
the difference either way.

What that leaves open, for whoever rewrites the history. The file holds every
line the user typed, which is the kind of thing that holds a password typed
into the wrong prompt. On Unix the C code made it owner only and the port no
longer does, so it is now whatever the umask gives, usually readable by the
group and by everyone. On Windows a file written under a user profile came
back with an access control list holding only SYSTEM, Administrators and the
user, measured on a real host, but that list is inherited from the directory
rather than set by the code. Write the history somewhere with a looser
inherited list and the protection is gone with nothing to show it. Making it
a guarantee on Windows needs `SetNamedSecurityInfo` with an explicit list,
which is a change in behaviour rather than a port.

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

That one does fire in practice. `ic_highlight_formatted` walks the whole input
and reads an attribute for each byte of it, so markup that spells out less text
than the input reaches past the end. With an empty format it reads the slot
straight away and the answer is a heap pointer that changes between runs, which
is why `tools/probe-highlight.c` leaves that case out and
`TestHighlightFormattedEmpty` covers it on the Go side instead.

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

### editline.c

`edit_refresh` asserts that the first row of the content shown below the line
is at or after zero. It is not, whenever the line is long enough to be clipped
to the height of the terminal while there is something to show below it, which
a completion menu on a short terminal reaches. A release build, which is what
people run, compiles the assertion out and then draws no rows of that content
at all, which is defined and harmless. `tools/build-probe-refresh.sh` therefore
builds with `-DNDEBUG`, so that the corpus records what a release build does,
and the port draws nothing there for the same reason.

The mark at the end of a wrapped row is chosen at compile time: a return symbol
on macOS and a left arrow everywhere else. That is a guess about which glyph a
terminal is likely to have rather than anything about the system, but the port
keeps the branch in `sys_darwin.go` and `sys_nondarwin.go`, because the
recorded sessions are kept per system and each holds the glyph its own C build
produced.

### editline_completion.c

`edit_completion_menu` tests the selected entry with
`selected <= count_displayed`, one past the last entry it drew. Every path
that moves the selection keeps it below that count, so the extra entry is
unreachable within one drawing of the menu; the only way to reach it is a
resize between two reads that makes the menu narrower than the selection it
already holds, and then the menu previews an entry it is not showing. The
port keeps the comparison as the C has it.

The same function clears the completions in the Escape branch and again at
the end, and nothing between the two reads them, so the first call does
nothing. The port keeps it, because removing it would be a change rather
than a port, and the recording shows it makes no difference.

The six `IC_DISPLAY2_*` and `IC_DISPLAY3_*` constants, which name how wide a
column may be in a two or three column menu, are read by nothing. The layout
measures the entries with `edit_completions_max_width` instead, so the widths
those constants describe are not the widths the menu uses. They are left out
of the port.

`count_displayed` is set to `count` when the menu starts and then set again
by whichever layout branch runs, before anything reads it. That initial value
is what the unreachable `selected <= count_displayed` above would have read
if it were reachable, so the two are the same oversight seen from two sides.
The port declares the count without a starting value, so that a branch which
forgot to set it would not quietly read a plausible one.

`edit_completions_max_width` measures a help of "" as two columns wider than
no help at all, because C can tell an absent string from an empty one. Go
cannot, so both read as no help, the same conflation `completions_get_display`
already forced on the display. It is only reachable by a completer that offers
an empty help on purpose.

### Found by running it

`ic_editline` wraps the edit loop in raw mode on both the terminal and the
keyboard, and writes a closing newline afterwards. The port had the loop and
not the wrapper, which no corpus could catch, because every corpus drives a
function rather than a program. The first end to end run found it in one
reading: without raw mode the line discipline rewrites the Enter key into a
line feed on its way in, so the editor saw the key that inserts a line break
rather than the key that ends a line, and no line could ever be finished.

### Also found by running it

Two faults in the public API, both found by running the example program with
its input coming from a pipe, and both of them mine rather than the C's.

A program whose input is a pipe lost all of its own output. The keyboard
cannot be opened when standard input is not a terminal, and the reader was
built with no terminal at all in that case, so every print silently did
nothing while the lines were read correctly. The C builds its terminal either
way and only marks itself as unable to edit. The port now does the same, and
the prompt is the one thing left out, because with a pipe there is nobody to
prompt and a prompt would only dirty the output.

Color was written into a redirected output. The C turns color off when the
output is not a terminal, and the port did not check, so a program whose
output went to a file wrote escape sequences into it.

Neither could be caught by a corpus, for the same reason the missing raw mode
could not: a corpus drives a function, and these are about how the program is
started and what it is attached to.

### And found by running it on Windows

The colour fix above was only half a fix. `writesToTerminal` asked
`isTerminal`, which this passage called `isATTY` until it was renamed,
which on Windows answers about the standard input whatever descriptor it is
handed, because a console there is reached by handle rather than by
descriptor. So a Windows program with its output redirected still wrote
escape sequences into the file. The Unix `isTerminal` does honour its
argument,
so the fault was there only on Windows, and only while a console was on the
standard input at the same time to make the wrong answer a plausible one.
`fileIsTerminal` now asks about the file it is given, and `isTerminal` keeps
the standard input it was written for. Found by windows-vm, by measuring both
answers with the output redirected rather than by reading the code.

This is the wrap mark again in a different costume: one function standing for
two questions that are the same on one system and not on another, so the
system where they differ is the one that finds out.

And a third instance of the same shape, found by windows-vm while regression
testing the API reshape. What was then `openTTYDevice` on Windows and is
now `openTTY` took a descriptor and
ignored it, always opening the standard input, so `WithStdin` and the
`WithInputFd` that then existed silently did nothing there: the caller's stream was accepted and
discarded, the console was read instead, and because opening it succeeded the
reader stayed in editing mode, so it looked as though it had worked. On Unix
the same call fails to open a plain file as a terminal, falls back to reading
plainly, and returns the line. Measured rather than read: a file already
holding a whole line, and `ReadLine` waiting three seconds for the keyboard.

It is fixed rather than documented, because the option meaning different
things on different systems is worse than either meaning. A negative
descriptor means the standard input, as on Unix, and anything else is a handle
the caller gave. `TestConsoleOpenTTYDeviceHonoursItsArgument` pins it, and
needs a real console for the reason the others do: a plain file being refused
only proves something while a console is there to be taken by mistake.

Three instances is enough to name the rule rather than the incidents. A
function that takes an argument it ignores is a trap on this platform, because
the value that would have been wrong is the one the caller believes was used.
`isTerminal` is the one remaining, and its comment now says outright not to
give it a second meaning.

### completers.c

`ls_valid_esc` is defined and never called. It looks like it was meant to
check that a colour setting holds only the escape codes that are safe to send
on, which nothing does, so a setting from the environment reaches the terminal
unchecked.

### editline_history.c

The search ends by pushing a key back for the edit loop to read, and it cannot
happen. Every path out of the loop sets the key to zero first, and every other
path goes round again, so `tty_code_pushback` is unreachable. The port leaves
it out rather than writing a line that cannot run.

### history.c

`history_save` truncates the file and writes the list it holds. `history_load`
stops at a line it cannot read and keeps what it read before it. Together
those destroy history: one malformed line, and every line after it is gone on
the next save. Measured before it was believed — a file of fifty bytes became
nineteen, and it happens on the next line the user types, because the edit
loop saves after every read.

The port did the same until Ken asked whether the file was being truncated.
Two departures now, both stated here because they are behaviour and not a
fault of the C's own logic.

Saving is refused when the file was not read in full, because the list held
is then shorter than the file and writing it would destroy the rest. The
caller is told, which is what `SaveHistory` returning an error is for.

Saving writes a temporary file beside the history file and renames it over.
A rename is atomic on every system this builds for, so the file is either the
old one or the new one and never a half-written one. Truncating first means a
failure part way through leaves the file shorter than it was, with nothing to
say so.

`O_APPEND` instead of `O_TRUNC` was the first thing suggested, and it does
not work with a save that writes the whole list each time: measured, three
saves of one, two and three entries left a file of six lines rather than
three. Appending would need the save to write only what is new, which is a
different design — a log that is compacted, as a shell does, rather than a
file that mirrors the list.

The C also calls `chmod` after `fopen` to force 0600, except on Windows. The
port dropped that call and created the file 0666, so the mode was whatever
the umask left. `DefaultHistoryFileMode` is 0600 now, set on the temporary
file before anything is written into it, so the contents are never readable
under a wider mode even for an instant. `WithHistoryFileMode` changes it.

### What the C exposes and the port does not

Found by Ken using the example and noticing that the up arrow brought back
`\q` rather than the statement before it. Leaving is a line the user typed,
so it is remembered like any other, and the newest entry in every session was
the command that ended the one before it.

The C has an answer: `ic_history_remove_last`, public, documented as removing
the line `ic_readline` just added. The port had `removeLast` inside and never
exposed it. `RemoveLastHistory` does now, and the example calls it before
leaving, which is what the C's own demo does.

One thing about it is worth stating because it caught this test twice.
Removing changes the list and not the file, since `ReadLine` writes the file
after every line it returns. By the time a caller decides a line was not
worth keeping, the file already holds it, so `SaveHistory` has to follow.

Four more of the C's public functions have no option behind them here, each
with the state it would set already in place: `ic_enable_history_duplicates`,
`ic_enable_completion_preview`, `ic_set_tty_esc_delay`, and the two prompt
marker getters. None has been asked for, so none is built. They are listed
so that the next person does not have to find out the same way Ken did.

### common.c

A byte the terminal sends that UTF-8 cannot read is read as a code point in
the raw plane, and the encoder turns that code point back into the one byte,
so the line holds the byte itself. Two such bytes that happen to form a valid
UTF-8 sequence therefore become one character in the line: nothing afterwards
can tell them from a character that was typed, and `decodeFromLocale` drops
them on the way back to a terminal that does not read UTF-8. Latin-1 "Ã©" is
an example, being the UTF-8 for "é". The port keeps the hole, and
`TestBytesThatLookLikeUTF8AreMergedIntoOne` records it so that fixing it is a
deliberate change rather than a surprise.

### tty.c

`tty_cpush` guards the byte pushback buffer with the length of the *code*
pushback buffer rather than its own, so the byte buffer can overrun. Nothing
reaches it, because the decoder never pushes back more than three bytes at
once. The port checks the buffer it is about to write to.

`tty_readc_noblock` asks `FIONREAD` about file descriptor 0 rather than about
its own. That is right only because isocline reads from standard input.

`tty_cpush_char` cannot push a zero byte. It builds a string of one character
and hands it to `tty_cpush`, which measures it with `strlen`, so a zero byte
measures as nothing and no byte is pushed at all. This is reachable: the UTF-8
assembly pushes back whatever it did not use, so a zero byte in the middle of
an invalid sequence is dropped rather than read as a key. The port drops it
too, because the recorded sessions come from a build that does. The
differential fuzzer found it on the input `f5 00 30`, which is kept as a
regression seed in `testdata/fuzz`.

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

The port does not carry the console emulation in `term.c`. About 400 lines
there translate escape sequences into console API calls, for a console that
cannot read escape sequences itself. The port writes escape sequences on every
system and asks the console to read them, which two `SetConsoleMode` calls in
`sys_windows.go` arrange as raw mode is entered and left.

That is now known to be necessary rather than precautionary, and the evidence
took two readings to get right.

A console on Windows 11 does not read escape sequences by default. Measured on
the plain console host: with standard output attached to a console the mode is
`0x000003` and the flag is off, and writing `ESC [ 5 ; 10 H` moves the cursor
to column 7 of row 0, which is seven characters printed on the screen rather
than a cursor move. So the emulation in the C addresses a real failure on an
ordinary machine, not a historical one.

An earlier reading said the flag was on by default, and it was taken in a
process whose standard output was redirected to a file, which is the one
arrangement where the flag cannot matter because nothing reaches a console at
all. A line editor never runs that way. The corrected reading points the same
way as the first conclusion but much harder: without the two calls the port
would not work on Windows at all, rather than merely on old machines.

Turning the flag on is enough. On a console with the flag off,
`SetConsoleMode` accepts the change, the same sequence then moves the cursor
properly, and leaving raw mode puts the mode back to exactly the `0x000003` it
found. That branch had never been exercised until a real console ran it. So
two calls replace the 400 lines.

What remains open is which consoles are supported. Turning the flag on works
on Windows 10 and later. Anything older cannot read escape sequences at all
and would need the emulation. Dropping them is a narrowing of where rline runs
and is a decision about supported platforms rather than a detail of the port.

The flag is a property of the console rather than of the process, which
matters for any test that reads it: a test asserting the ambient default is
asserting what the machine happens to be set to, not what the port guarantees.

The function keys are verified from a real keyboard. Ken pressed them and
windows-vm read what arrived: `VK_F11` is `0x7A` and `VK_F12` is `0x7B`, the
encoder sends VT codes 23 and 24, and they decode to `key.F11` and `key.F12`.
That settles the departure from the C, whose encoder sends 13 and 14 there,
which its own decoder reads as F4 and F5.

The example has been driven by hand on a real console. Ken typed
`select * where a = true`, Enter, Enter, `zzz ;`, Enter and `\q` into
`cmd.exe`, 36 keys with one backspace, and windows-vm kept the log. It is the
first evidence on Windows that is neither a unit test nor a banner nobody
typed at, and it covers the whole chain at once: a key pressed on a keyboard,
through the console input buffer, `ReadConsoleInput`, the virtual key
translation, the sequence builder, the escape decoder, the editor, the
highlighter, the redraw, and out through a console output handle that reads
escape sequences only because `startOutputEscapes` asked it to. Every one of
those had been verified on its own first.

What the log shows: highlighting applied as a word completes rather than on
submit, so the final letter of `select` turns the whole word bright red in the
same redraw that echoes the keystroke; backspace removing a character,
redrawing and putting the cursor back a column; Enter opening a continuation
row with the `...>` prompt rather than submitting; and each later redraw
walking up the right number of rows to repaint a three row statement. The
newlines survive into the line the caller is given, and the example's own
summary flattens them to spaces, which is what `example/main.go` asks for.

A second run added the password read. `\pass`, sixteen characters and Enter:
between the prompt being drawn and the newline, seventeen keys were read and
nothing at all was written — no redraw, no masking character, nothing — so the
password never reached the screen, and this is the read itself rather than a
test standing in for it.

A third run closed what was left. Tab opened the completion menu, which drew
three columns of nine numbered entries below the line without writing over it,
with the paging hint for the other six, and put the cursor back with
`\e[4A\e[6C`. The down arrow moved the selection, which redrew with the chosen
entry marked and brightened, and picking it replaced the typed `c` rather than
appending to it. Then a four row statement was edited in the middle: up into
row 2, left, two characters typed, and the whole block repainted with the
other rows' contents and highlighting intact. The arrow keys arrived as
`\e[1;1A` and `\e[1;1B` and `\e[1;1D`, byte by byte, which is
`sys_windows.go`'s encoder and the escape decoder agreeing under a human's
fingers rather than against records the tests wrote.

So across three runs, everything the editor does on Windows has now been done
by a person: typing, highlighting, backspace, three and four row statements,
the completion menu and picking from it, the arrow keys within and between
rows, `\pass`, and `\q`. No defect was found in any of them.

The keystroke counts behind that took three attempts to get right, and both
wrong answers were the flattering one. A search for an escaped tab matched the
`t` in `select` and reported Tab as pressed when it was not; an anchored
pattern defeated by CRLF line endings reported backspace as unpressed when it
was. The counts are only trusted because the distinct escaped values were
extracted and counted rather than pattern-matched. It is the same shape as the
section below: a count that is confidently wrong looks exactly like a count
that is right.

Testing F11 needs Ctrl+F11. The console host claims a bare F11 for fullscreen,
so it never reaches the program at all. That is the terminal rather than the
shell or the virtual machine, and the modifier does not change which virtual
key is reported, so a modifier is the way to reach any key the terminal has
taken.

The escape wait is reachable on Windows, which an old comment denied. When the
Windows opener stopped setting `escInitialTimeout` — what was then `newTTY`
and is now `newDecoder` sets it
for every system — the line it removed carried a comment saying nothing there
ever waits for an escape byte, because the console delivers a key event whole.
Half of that is right. windows-vm measured it on a real console at 91335a9:
an arrow decodes to `key.Up` in 1ms, so a sequence built from a key event
never reaches the wait, and pressing the Escape key produces a lone escape
byte that resolves in 103ms against the 100ms the constructor sets. So the
wait is not dead on Windows, it is one keypress away. The practical stake is
small — a zero wait there would make a lone Escape resolve sooner rather than
wrong, because a whole sequence is already pending and is returned regardless
of the timeout — and the wording is the point: "nothing here ever waits" reads
as permission to treat the value as dead, and it is not.

## What the banner does not watch

The banner the example prints is the cheapest check either peer has: it is
written before any input, so it needs nobody at the keyboard, and it
exercises the markup writer, the bbcode resolution, the colour decision and
the terminal write in one go. windows-vm diffs it byte for byte against a
kept baseline, which is what caught nothing moving through three reshapes.

What it cannot watch is stated here so that nobody reads more into a green
banner than it carries. The colour bleed ken-mba found — a newline written
inside an attribute the markup left open — is only visible when a background
colour is the thing left open, because a background fills the rest of the
row and a foreground does not. The banner sets no background. A byte for byte
diff does prove attributes are closed before their newline, which is the
mechanism, but that is one step short of watching a row fill.

Closing it needs one line in the example with a background left open, and a
human at the console to see the row rather than a session reading bytes.

## Why the baselines are files rather than assertions

windows-vm keeps the example's banner as recorded bytes and diffs new runs
against them, rather than asserting a pattern over them. That choice has paid
three times, and the reason is worth stating: a pattern can be confidently
wrong and produce a plausible number, and a byte diff cannot — it either
matches or shows you the bytes.

The three, all theirs, all caught by themselves, all failing by matching more
than was meant. A search for an escaped tab matched the `t` in `select` and
reported Tab as pressed when nobody had pressed it. An anchored pattern
defeated by CRLF reported backspace as unpressed when it was. And a pattern
meant to find a newline before an attribute reset was written with a `\n`
that collapsed to a bare `n`, so it found the `n` in `rline`, `statement`,
`Enter` and `showing`, and briefly looked like the colour bleed.

Every one of those answers looked like a measurement. None of them cost
anything, because the same run carried a byte diff that disagreed.

## Things that look like a platform bug and are not

Two reports of "no colour on Windows" were the launcher rather than the port,
and both looked convincing. Written down because the next person will see the
same thing.

`NO_COLOR` is set in some agent environments and is inherited by any terminal
started from one, so colour is correctly turned off and the program looks
broken. Check the environment before the code.

Clearing it per host is where the second one came from. In `cmd.exe`,
`set NO_COLOR= && ...` sets the variable to a single space rather than
emptying it, because cmd takes everything between the equals sign and the `&&`
as the value. `set "NO_COLOR=" && ...` empties it. The convention says any
non-empty value turns colour off whatever the value is, so a single space
turning it off is right and is not to be softened. The visible result was
colour in two Windows hosts and not the third, which is about as convincing a
platform difference as could be invented, and was entirely the quoting.

So: a colour difference between Windows hosts is more likely to be the
launcher than the host, and a colour difference that appears everywhere is
more likely to be the environment than the code.

## Where a corpus lives

A corpus is a single file under `testdata/` when the bytes it records are the
same everywhere, which is true of most of them. It is split when something
changes those bytes from one build to another, and the split is by whatever
actually changes them.

Two are split, and they are split differently, which is the point.

`testdata/<variant>/refresh.txt` holds the redraws, split by the compile time
branch that picks the mark at the end of a wrapped row: a return symbol on
macOS and a left arrow everywhere else. `sys_darwin.go` and
`sys_nondarwin.go` name the variant beside the glyph, so the two cannot drift
apart. 336 of the 1584 recorded redraws hold that mark. The split is by the
branch and not by the system, because keying it on the system would be finer
than the thing it stands for and would demand a separate recording from every
system that compiles the same branch. Windows and Linux share one set.

`internal/capture/testdata/<goos>/` holds the recorded sessions, and that one
is per system. The sessions come from running the C demo, so they hold whatever
that system's C build does, and `term.c` differs by more than one glyph:
macOS asks the terminal for its color palette, and Windows has a whole console
emulation that neither of the others compiles.

Getting either wrong is quiet rather than loud, and both have gone wrong once.
The redraw was made per system in the code before its corpus was, so macOS
compared its own glyph against a recording made on Linux. Then the corpus was
keyed on the system rather than on the branch, so Windows was told to record a
set it already had. And the check for a missing set of recorded sessions lived
in a file tagged to Linux and macOS, so on Windows it left the build and the
package passed while testing nothing.

A missing corpus is therefore a failure with a message saying how to record
one, never a skip, and the check has to compile on the system that is missing
it.

The rule behind all three: when a check is tagged per system, ask what runs on
the systems it excludes. Each of the three was found by making something else
per system, never by a test noticing, and each one looked green from the
machine the work was done on.

## Waiting for a program that has not started

`internal/capture` collects the output a program writes when it starts by
reading until the program goes quiet. That cannot tell a program which has
finished writing from one which has not started, and the two look identical:
nothing arrives. A program built moments earlier is usually still being
loaded when the quiet period runs out, so the recording is taken to have
begun, the first input goes into a terminal nobody is reading, the terminal
echoes it, and entering raw mode throws it away. The session then shows text
the program never saw.

It went wrong twice, both times in a test that builds the example and drives
it, and both times it was patched by guessing a longer wait for that one
session. The guess is gone. `Session.Start` is how long to wait for the first
byte, five seconds by default, and the quiet rule takes over once anything
has arrived. That removes the guess from every session rather than adding one
to each session that happens to show the problem.

Nothing in the recorded sessions moved, because the C demo writes its banner
at once and was never waiting on this.

## The blind spot a recorded corpus has

A corpus records what the C answered. So it cannot record anything the C has
no answer for, and everything this port added over the C sits in a hole
exactly its own shape. That is not a gap in the corpora; it is what a corpus
is.

Three findings have now come out of it, and none could have come from
anywhere else.

`ReadLine` answering `ErrInterrupted`. The C clears the line and returns an
empty string, so there is nothing to record and no recording could disagree.

`LoadHistory` returning an error. The C ignores every failure while reading
the history file, so the error is the port's own and the corpus has no
opinion about it. It was unreachable for a long time and nothing said so.

Every editing operation returning whether it changed anything, which is what
decides whether the line is drawn again. The C has no such value — it returns
early instead — so `editor_test.go` discards it with a comment saying the
corpus does not record it. That comment is correct, and it is the whole
problem: making `cursorLeft` always answer false, or always answer true, left
the entire suite green in both directions, and both are faults. Always false
leaves a key doing nothing on screen until something else forces a redraw.
Always true draws the line again for a key that did nothing, which is the
flicker the value exists to prevent. Found by ken-mba, who took their own
earlier finding about discarded values seriously enough to sweep every one in
the tests.

A fourth, which is the same shape wearing a disguise, and which is worth
recording with the measurement because it nearly became a second rule.

`Style` and `StyleRunes` both refuse a negative position, and the comment
cites the C as the reason. ken-mba reported that taking the guard out of
either one left the whole suite green, and proposed a second class beside the
first: what the port kept from the C on purpose, where the corpus records the
behaviour but nothing records that the guard is why.

Measured, that is half true and the half that is true is the first class
again. `TestHighlightMatchesC` does hold `Style`'s guard — the corpus drives
`Style` with positions of -6, -3 and -1, so removing it fails the replay.
`StyleRunes`'s guard was held by nothing, and that is because `StyleRunes` is
itself something the port has and the C does not: the C has one function with
a sign convention where the port has two methods, so no recording can reach
the second one. The proposed second class dissolves on measurement and the
first one swallows it.

Worth noting what nearly happened, since the section would have carried it.
The second class was offered, hedged, and could have been written in on
trust; it was plausible and it came from the person who had found the first
three instances. Had it gone in, this section would read as two shapes where
there is one, and nothing afterwards would have caught it, because a document
is not run. The measurement is what kept it right, and the cost of the
measurement was four minutes.

The test ken-mba wrote is worth keeping either way. It holds `StyleRunes`'s
guard, which nothing did, and it writes the promise down for both.

How the claim came to be half right is its own finding, and it is not the one
I first guessed. I wrote that the run had been scoped too narrowly; it had
not, it covered the whole package. `Style` and `StyleRunes` differ by one
character — the sign on the count — so the mutation matched `StyleRunes`
alone, and the result was reported for both. `Style`'s guard was never
asked about. Two methods that differ by one character are two mutations, and
one was run.

That is the mirror of something ken-mba hit from the other end earlier: a
markup test covered `Write` and not `WriteString`, so reverting `WriteString`
alone came back not caught. A rule written twice needs mutating twice and
testing twice, and the only difference between the two mistakes is whether
the copy that was skipped sat in the test or in the mutation.

So when looking for what to check next, "what does the port have that the C
does not" is a better question than "what is uncovered". Coverage will not
point at any of these: every one of those lines ran, every time.

## Tests that pass without checking anything

The rule above is one case of a wider one, which has now cost time eight times
on this port. A test can report success while verifying nothing, and nothing
about the run says so. The ways it has happened here:

A skip that reads as a pass. The macOS recordings were skipped rather than
compared, so the platform looked covered. The two Windows console tests skip
unless the standard input is a console, which `go test` never gives them, so
following the instruction that was written above them produced a skip that a
reader would have reported as a pass.

A check that leaves the build. The test for a missing set of recordings sat
in a file tagged to Linux and macOS, so on Windows it was not compiled and
the package passed while testing nothing at all.

A test that asserts what the code does rather than what is true. Two
expectations for the Windows function keys held the same wrong numbers the
encoder produced, so the unit test agreed with the bug and stayed green while
F11 arrived as F4. A directory case on Windows expected the one answer that
every directory gives there, so it passed for the wrong reason and would have
stayed green through a real regression.

A contract that was true by accident in three places. A nil ansi.AttrBuf is
usable and every method accepts one, which is what lets rline pass nil down
ten signatures to mean "record no attributes" instead of branching at each.
Taking each nil guard out and running the whole module showed four of them
panicking — Length, At, DeleteAt and the fill behind SetAt and UpdateAt — and
three changing nothing: Clear, Extend and InsertAt. So the words "every
method" rested on four methods. `TestNilAttrBufAcceptsEveryMethod` calls all
of them on a nil buffer, and all seven guards are load-bearing now.

The first version of this entry said three and four rather than four and
three, and named the wrong set. Five guards were measured and the other two
were inferred, and the inference was written down as if it were a
measurement. ken-mba could not reproduce it against the pushed tree and said
so rather than assuming they had run it wrong. Measuring what you did not
measure is the same fault as an assertion copied from an observation, one
step further back.

Code that no recording reaches at all, found by looking rather than by a
failure. `ansi.ParseANSI256` reads a palette index out of text, and
`testdata/bbcode.txt` holds no `ansi-color` tag, no `ansi-sgr` and no
`bgcolor=`, so the whole 130,000 line corpus never calls it. The decimal scan
behind it was written out again when the code moved packages, which is the
worst combination: a reimplementation with nothing watching. `names_test.go`
now pins both readers against what the C's sscanf does — leading space, an
optional sign, digits, and whatever follows ignored — and five mutations of
the scan, the hex reader and the range check are all caught.

A coverage number that looks like a gap and is not. After the fix above,
`ansi.scanHex` reports 90.9 per cent, and the missed line is the `return` in
its `else if err != nil`. That branch cannot run: the string handed to
ParseUint is not empty and every byte of it passed isHexDigit, which is the
only way ParseUint gives ErrSyntax, so the ErrRange above it is the only
error reachable. Measured by windows-vm over 220 all-hex strings, one per
digit at every length from 1 to 200, with no error that was not ErrRange.
The branch is kept because it becomes live the day the scanning loop accepts
something ParseUint does not, a "0x" prefix or a digit outside ASCII, and it
now says so in place, so the next sweep does not spend an hour on it.

Code that answered nothing where the C answers something, found by a reader
rather than a test. `ansi.scanHex` carried the comment "sscanf into a 32 bit
value keeps the low bits of a longer number", and for up to sixteen hex
digits it did. Past that the number overflows the 64 bit accumulator, and the
port refused the whole value where the C library saturates and hands back
0xffffffff, so `[#fffffffffffffffff]` gave no color instead of white. Found
by windows-vm, who noticed the code contradicting its own comment and said
plainly that settling it needed a compiler they do not have. Settled here by
running gcc on the same strings, which also showed the comment right about
the low bits and wrong about where they stop: "#123456789" is 0x23456789 and
"#100000000" is black, not a refusal. The measured answers are now in
`names_test.go` as the expectations, and the comment says which library was
measured, because the C standard leaves an unrepresentable value undefined.

Names the port carried over from C, and what they cost. `free` was a method
on the terminal that flushes and leaves raw mode and frees nothing, kept from
`term_free` into a garbage-collected language; it is `restore` now.
`isATTY`, `isXDigit`, `toXDigit` and `fromXDigit` are POSIX and `<ctype.h>`
spellings of things Go names differently, and
`getNumberOfConsoleInputEvents` was a Win32 function copied down to its out
parameter. That last one is the instructive shape: the C-ism was not only the
name but the signature, so the fix returns a value rather than filling a
pointer, and the out parameter stops at the Win32 boundary where it belongs.

`indexZero` was `bytes.IndexByte(b, 0)` written out by hand, which is the
same species as the C's own allocator surviving: a standard library was there
the whole time. And `boolInt`, `boolToInt` and `btoi` were three names for
one four-line helper in one package, which is what happens when each test
file is ported in turn and nobody looks across.

Both Gemini and DeepSeek were asked independently and agreed on every one of
these. They disagreed on the public API, which is where they are worth less:
DeepSeek wanted `ReadLine` renamed to `Readline` on the grounds that peers
spell it that way, and `bufio.Reader.ReadLine` says otherwise; it wanted
`SetCompleter` renamed because `SetX` is unidiomatic, and `log.Logger` has
`SetFlags`, `SetOutput` and `SetPrefix`; and it wanted the sentinel errors
turned into `errors.New` values so that callers could use `errors.Is`, which
they already can, measured. A model is a good reader of a name and a poor
witness to what the standard library does, so every claim it made about
precedent was checked against the precedent.

A gate that was never reading what it gated on. tools/lint.sh computed its
cache key as `key=$(cat "$mod/go.mod" "$mod/go.sum" | cksum | tr -d ' \t') ||
exit 2`. A pipeline's status is its last command's, so that `|| exit 2` reads
tr, which succeeds on anything. Measured: with the pin files missing, cat
fails, cksum checksums nothing, and the key comes out 42949672950 while the
script carries on and caches under a key that means "no pin at all". The same
family as the world-writable cache directory, and in the same script.

`set -o pipefail` fixes it, and the one line fixes the pipelines nobody has
written yet rather than the one that was found. It is right here because the
script gates on status; it would be wrong as a blanket habit, because a
pipeline whose reader closes early, `long_thing | head -1`, starts reporting
the writer's SIGPIPE as failure.

The construct has now caught all three sessions inside two hours, which is
the part worth writing down. windows-vm read `go build ./... | head` as the
compiler's status and printed a build failure that did not exist. ken-mba
pushed a red commit through `./tools/lint.sh 2>&1 | tail -1 && git commit`,
where the `&&` was gating on tail and never saw the linter at all. This
session has been piping into head all week without once thinking about it,
and survived only because go build is quiet on success, so absence of output
and exit zero coincide almost always. They come apart exactly when the tool
is noisy on success, which is when you least want to be wrong.

windows-vm's is the fifth, and it came with the diagnosis that generalises
the rest. They piped vet through head and read the pipeline's status, and
printed `exit=0` beside `android/amd64` on a line whose own output said the
vet had failed. They caught it only because the output contradicted the
number beside it — had vet been quiet on failure, as `go build` is, the wrong
figure would have travelled with nothing to disagree with it. The narrow
lesson is not "do not pipe". It is that the reach for a pipe is a reach to
shorten output, output is longest when something is going wrong, and so the
habit fails hardest in exactly the case it is used most. Writing the status
into a variable before touching the output is the fix that does not depend on
remembering.

ken-mba's reading of why is the useful one: a rule cannot compete with a
reflex. Piping into tail is what your hands type when you want to see the
last line, and wanting to see the last line has nothing to do with wanting to
know whether it passed. The harness fix for the third answer worked because
it removed the choice rather than reminding anyone to make it, and pipefail
in the scripts that gate is the same move. Nobody has the equivalent for an
interactive shell.

Suspecting the case before the code, and why the order is not symmetrical.
ken-mba wrote a test that brace matching reaches the drawing, and it went
red. The story that came with it was plausible: brace matching never runs.
The code was right and the test case was wrong — `highlightMatchBraces`
counts a brace as under the cursor when the cursor is the position *after*
it, `i == cursorPos-1`, so a cursor sitting on the closing brace of `(x)`
matches nothing, and position 3 is what a person pressing right at the end
of the line actually has.

Their own statement of why it matters is better than any rule: a bad test
wastes the writer's time, a false finding wastes the reader's and then goes
into a document both of them rely on. The costs are not symmetrical, so the
order is suspect the case first — and it matters most exactly when
confidence is highest, because a red test feels like evidence.

Four things today had that shape and only this one cost nothing, because it
was caught before it was sent: an empty probe run that produced a diff of
the right size, a count of five guards written down as seven, a difference
from pyrepl reported as a bug, and this. In each of the other three the work
was done before the cheap check that would have redirected it.

What the four have in common is narrower than carelessness, and ken-mba
named it: in each one the work followed a belief rather than a measurement,
and the belief was recent and had just been right about something adjacent.
The red brace test arrived already fitting a hypothesis formed an hour
earlier, about a feature that had genuinely just been shown untested. That
is what made it read as confirmation rather than as a question. A belief
that has just been right is the dangerous kind, because being right about
the neighbouring thing is not evidence about this one.

And the reason this one was free is not a property of it. The check was one
command and it happened to be run before the message was sent. Had
`highlightMatchBraces` been harder to read, or had the message gone first
and the investigation second, it would have cost what the other three did.

A wrapper that looked like a habit and was load-bearing.
`defer func() { _ = f.Close() }()` appeared twelve times, and the item
against it assumed the plain `defer f.Close()` would do. Measured first:
that form fails errcheck, which golangci-lint v2 has on by default, so the
wrapper was not ceremony but the thing keeping the tree lint-clean.

The fix is a four-line errcheck exclusion for `(*os.File).Close` and nothing
wider, with its own reason written beside it. A deferred close on a file
opened for reading is the one unchecked error worth having: the read has
already succeeded or already failed and the close cannot change either.

Two things about it are worth knowing rather than discovering. errcheck
cannot scope an exclusion to deferred calls, so a future unchecked close on
a file opened for WRITING passes too — which is why `history.go` checks its
write close explicitly and says why, and why the one remaining wrapper in
`sys_windows_test.go` is on a file from `os.Create` and now says that is the
reason. And the exclusion was checked for over-reach rather than assumed
narrow: an unchecked `f.Write` is still reported.

Four wrappers stay because they are not closes at all —
`windows.SetConsoleCursorPosition`, `windows.SetConsoleMode`, `os.RemoveAll`
and the library's own `Prompt.Close` — and dropping those errors is a
decision each time rather than a pattern.

Three options for three streams, named two ways. `WithInput`, `WithOutput`
and `WithStderr` matched no established pattern: the standard library and the
readline packages are symmetric one way, and cobra is symmetric the other.
`os/exec.Cmd`, `x/crypto/ssh.Session` and `chzyer/readline.Config` all carry
`Stdin`, `Stdout` and `Stderr` taking arbitrary readers and writers, and
`cobra` carries `SetIn`, `SetOut` and `SetErr` over `inReader`, `outWriter`
and `errWriter`. The defect was the asymmetry rather than either vocabulary,
and the stream names win here because the third option was already `Stderr`
and nothing was going to rename that.

So `WithStdin` and `WithStdout`, and the fields with them. Neither name
promises the real standard stream, which is what `os/exec` established and
what the doc comments now say: a bytes.Buffer is a perfectly good Stdout.
Neither promises a file descriptor either. `WithStdin` does look for one, by
asserting to `interface{ Fd() uintptr }`, because editing needs a terminal —
and `os/exec.Cmd.Stdin` branches the same way on whether it was handed a real
`*os.File`. Gemini read that assertion as an argument for the name and
DeepSeek read it as beside the point; DeepSeek is right, because the
precedent covers the behaviour and not only the word.

Every precedent either model named was checked against the package. DeepSeek
marked two of its three as needing verification and both were right; Gemini
marked nothing and was also right, having been confidently wrong about a
package API earlier the same day.

A positive bool's zero value is off, and every literal then has to say so.
Turning the `no...` options positive was mechanical in the option setters and
in the reads, which the compiler and the corpus between them police. What it
broke was every struct literal in the tests, and not because the literals
were wrong: they had relied on an unset negative meaning the feature was on.
`termOptions{Sizer: ...}` had colour, because NoColor was unset. The menu
recording lost its greys because `noBraceMatch` unset had meant brace
matching on, and `braceMatching` unset means off.

Nothing about that is a fault in the flip; it is the flip working. A negative
field hides its default in the zero value, so a literal that says nothing is
still asking for something. A positive field makes the literal say what it
wants, which is why the test envs now list all six switches rather than the
one or three they used to. That is more lines and it is the point.

The `New` half of the item was already done for everything that was not a
bool. It is a block of `true` now, with the reason written beside it.

One switch went entirely rather than being flipped. `WithHighlighting` and
the field behind it said what a nil highlighter already said: the only use
was `fn := ev.highlighter; if !ev.highlighting { fn = nil }`, which is a
guard around whether to call a callback that may not be there. A caller turns
highlighting off with `WithHighlighter(nil)` or `SetHighlighter(nil)`, and
the test harness showed the redundancy plainly by setting the flag only where
it also supplied a highlighter. Verified load-bearing afterwards: passing nil
in place of `ev.highlighter` fails two tests.

`hints` and `braceMatching` look like the same shape and are not. There is no
hint callback: a hint is the rest of the only completion that fits, so the
nil test would be on the completer, and turning hints off that way would take
tab completion with them. Brace matching has no callback at all. Both switch
a distinct behaviour over shared machinery, which is what a switch is for.

Three switches were not options and went anyway, for consistency:
`completeNoPreview` became `completePreview`, whose comment had defended the
old name on the ground that the C names its flag that way; `noEdit` became
`canEdit`, which turns `return !s.noEdit` into `return s.canEdit` in
`Interactive`; and the `noColor` parameter threaded through the filename
completer became `color`. The one place `noColor` survives is `comp_test.go`,
where it names a field of the corpus rather than a field of this package: the
recording holds what the C was handed, so the test reads it in the C's
vocabulary and passes the port the opposite.

A difference read as a fault. Comparing the Enter handler against pyrepl,
which is the closest modern implementation and which rline resembles closely
because WithContinue is its more_lines callback, turned up a clause rline
does not have: pyrepl breaks the row whenever the cursor has rows below it,
before asking the callback at all. Measured here, rline submits in that case:
going up into the first row of a finished statement and pressing Enter hands
the statement back rather than splitting the row.

That was written up as a bug and a guard was added, which broke a test that
had recorded the opposite on purpose. The C is the reason: isocline's Enter
finishes wherever the cursor is, and its only exception is the line
continuation character. pyrepl needs its heuristic because Enter is the only
key it has; rline has Ctrl-J, which breaks a row without finishing, so the
heuristic would take a choice away rather than add one.

So the guard came out and the documentation says which behaviour this is and
why. The lesson is about the method rather than the key: a difference from a
well-made neighbour is a question, not a finding, and the way to tell is to
ask what the thing being ported does and whether the neighbour has the same
alternatives available. Both answers were one command away and neither was
run before the change was written.

A document that names a function that is gone. Renaming `isATTY` to
`isTerminal` left seven mentions of the old name in this file, describing
code that no longer spells it that way. Found by windows-vm, who noticed the
rename was missing from a list of them and said the thing that makes it
matter: they had referred to `isATTY` by name in a dozen reports, so anyone
searching the record for it would land on a name the source no longer has.

Three mentions stay on purpose and the distinction is the point: the entry
recording the rename keeps the old name because that is the before, and the
two historical statements say what it was then and what it is now. The four
describing current code were changed. A document that mixes what is true now
with what was true then has to say which it is doing every time, or a reader
cannot tell a deliberate old name from a stale one.

Checking for it is the same shell loop as the file names, run against the
identifiers a rename touches rather than against the disk.

A document that describes files that are gone. PLAN.md's list of where
things live named `winkey.go`, deleted eight commits earlier when its
contents moved into what was then `decoder.go` and is now `decoder.go`, and
`password.go`, whose code is in `rline.go`. It also said what was then
`tty_posix.go` and is now `tty_posix.go` is tagged `linux || darwin`
while the file
says `unix && !aix` and another section of the same document says so
correctly, so it contradicted itself. Found by windows-vm while checking a
message of mine that repeated the listing.

The listing was rewritten in the commit that split the editor out, which is
the point: rewriting a section is exactly when a stale entry gets carried
across, because the eye reads what the line means rather than whether the
file is there. Every file name in the listing is now checked against the
disk, and every build tag the document quotes against the tag in the file
it names. Both checks are one shell loop and neither had ever been run.

A file that only one platform compiles. Qualifying every call after `text.go`
moved to `internal/text` was done with the compiler as the oracle, and the
compiler on this machine never reads `sys_windows.go`. It still called
`limitToLength`, which no longer exists, and the whole suite passed green.
The cross-vet loop over eleven GOOS and GOARCH pairs caught it, which is the
job it was added for. Anything that edits call sites across the package has
to end with that loop and not with `go test`, because a build tag is a
perfectly good place for a rename to hide.

A refactor whose one behavioural change the whole corpus is blind to.
Rewriting `updateProperty` to assign fields instead of writing through
pointers dropped the `return name` on every property case, so a property fell
through to the style search as well. All 130,000 recorded calls pass either
way, because the C probe never defines a style whose name collides with a
property, and because every property sets its field before the answer is read:
the difference only appears once a style of that name exists to be applied on
top. Verified by putting the fault back and watching the whole suite stay
green, then measuring the real difference directly — a style named `bold`
leaves `Color` at `None` when the answer is returned and at `0x01ff0000` when
it is not. `TestPropertyBeatsAStyleOfTheSameName` now pins it, and fails on
the fault. This is the corpus blind spot from the other side: a recording
cannot rule out a case its author never thought to record.

A corpus that cannot tell two answers apart. The completion menu recordings
were first made against a `bbcode` with no styles defined, so every style name
in the menu rendered to nothing and `[ic-info]` and `[ic-emphasis]` produced
the same bytes. The recording looked complete and would have passed with the
wrong style on every entry. Both the probe and the replay now define the
styles a `Reader` defines, which is what makes the names observable at all.

What is still not reached, stated rather than left to be assumed. The menu
corpus drives `completionMenu` only, so nothing records
`generateCompletions` itself: which of its three branches runs, and whether
the longest shared start goes in before the menu opens.
`TestTabFillsInWhatEveryAnswerShares` drives it through the editor instead,
and taking the longest-prefix call out fails it, so the entry point is no
longer untested. A recording would still be worth having, because a driven
test says what the port does and a recording says what the C did.

What the shared start does once it is asked for is recorded, in the
`prefixmixed` cases of `testdata/completions.txt`: entries that take away
different amounts of the line, a shared start shorter than the amount they
take away, and replacements long enough to be cut by the 256 byte buffer the
C copies them into, including a pair whose 256th byte falls inside a three
byte character. Five deliberate breakages of `applyLongestPrefix` are caught
by those and were caught by nothing before.

The beep on no answer is out of reach of both: `term_beep` writes to the
standard error rather than through the terminal, in the C as well, so
neither a recording nor a driven test sees it.

A check that cannot fail where it is run. `TestWritesToTerminalLooksAtTheWriter`
asserts that a plain file is not taken for a terminal, which catches the
Windows fault only while a console is on the standard input. The go tool hands
the test binary a null standard input whatever window it was started from, so
under `go test` on Windows that check passes against the bug it was written to
catch, and it is only a real check on Unix. It logs which state it ran in
rather than skipping, and `TestConsoleWritesToTerminalOnAConsole` makes the
same check where it can fail. Measured by windows-vm, who ran both halves from
one console window: `isTerminal(0)` is false under `go test` and true in the
same binary started directly.

A diagnostic that cannot be read in the case it was written for.
`TestConsoleWritesToTerminalOnAConsole` logs whether the standard output is a
console, and `t.Logf` writes to the standard output, so capturing the line is
what makes it say false. It can never be captured saying true. Found by
windows-vm while running the test both ways as asked. The answer here is not
more machinery for one line: the assertion asks the console what the answer
should be rather than fixing it, so it checks itself, and the comment now says
that an attached run gives an exit code and nothing capturable. But the shape
is worth naming, because it is the null standard input again in miniature:
observing changes the thing observed.

A corpus that was never attacked. A recording that is written and then only
ever run green is the same failure as an expectation copied from an
observation: it looks complete because nothing has tried to make it fail.
Every recording on this port that was attacked turned out to have a hole.
The completion menu could not tell one style name from another, and could
not reach the two column layout at all, until cases were added for both.
The shared start of a set of completions was recorded in four ways and the
fifth, a shared start shorter than the amount the entries take away, needed
a case that was not there; nothing said so until the guard was deleted and
the corpus stayed green.

Reading the exit status matters twice as much when attacking a corpus. A
mutation that was never applied looks exactly like a mutation that was not
caught, and the answer it suggests — add a case — is the wrong work. Both
sessions hit this from different directions: a shell loop that counted lines
of output rather than reading the return code, where BSD sed had taken the
expression as a backup suffix and patched nothing; and a `go test ... | grep
... && git commit`, where grep succeeding masked the test failing. Patch,
assert the file actually changed, run the test, read the return code, restore
the file.

Where to spend a test, when everything cannot have one. An error stream is
worth more than most writers, because it is where a program says what went
wrong: one that fails silently turns a shell into one that has stopped
reporting errors and does not say so. The failure mode is the absence of the
thing that would have told you, which is the hardest kind to notice and the
cheapest kind to prevent. ken-mba's argument, made while finding that
`Session.Stderr` swallowed a failed write and dropped the reason with it.

Reading the return code is not enough on its own, because a mutation that
does not compile also returns non-zero, and reads as caught. `if false {`
around a branch leaves the variable it tested unused, which Go refuses to
build, so the test never ran and the claim that it would have failed is
unproven. Found while seconding the interrupt work: the mutation that was
meant to show `ReadLine` notices Ctrl-C did not compile, and a compiling
version of the same mutation was needed to show it. So check the run
compiled as well as that it failed, and treat "did not compile" as a third
answer beside caught and not caught rather than folding it into either.

`tools/mutate.sh` does all of that, so nobody has to remember it: it asserts
the patch applied and changed the file, builds before it tests, tells the
three answers apart, runs the whole package unless given a pattern, and
asserts the file came back. Its own three answers on the interrupt branch are
the worked example — the `if false {` mutation says it did not compile, a
compiling version says caught, and widening the condition with a key that
never ends the loop says not caught, which is the "a line nothing reads" case
rather than a missing test.

A fourth answer, which the harness cannot give and a reader has to. An
equivalent mutation changes nothing, so NOT CAUGHT is correct and useless:
`term.write(s)` swapped for `term.writeBytes([]byte(s))` is the body of
`write` written out. Not caught, not uncovered, indistinguishable. Read the
mutation before believing the answer, or it sends you to write a test that
cannot exist.

How to attribute a catch, which is the half the harness cannot do. A "caught"
is a pass and there is nothing to re-run, so nothing in the tooling will tell
you that the test you think is responsible is not. The only way to find out
is to run the same mutation scoped to that test alone and see whether it goes
red by itself. That is how `Style`'s guard was shown to be held by the corpus
replay rather than by the test beside it, and how ken-mba confirmed it
independently on macOS — scoping to the one test also ruled out their own new
test as the thing catching it.

So: if a mutation is not caught, suspect the case before the code, and the
harness will now widen the run for you. If it is caught, suspect the
attribution before believing it, and that one is yours to check.

A run owns the tree while it lasts. A test started beside one in the
background read a mutated `history.all` and reported two failures that
belonged to neither test. `tools/mutate.sh` takes a lock directory now rather
than leaving that to convention, at windows-vm's suggestion — they had hit
the same shape twice with their own console harness, where a run builds a
binary that another run is still writing.

Scope the run to the whole package, not to the test being defended. Three
mutations in that same round read as not caught against the two tests the
change had added, and all three were caught by other tests in the package
that nobody had thought of as covering them. A mutation that only the rest
of the suite catches is worth knowing about — it says the new test is
narrower than it looks — but it is not a hole, and reporting it as one sends
someone to write a test that already exists.

`tools/mutate.sh` does this itself now, because the rule was a paragraph
here and it caught both sessions anyway, the second time catching the person
who wrote the paragraph. A run given a pattern that answers not caught runs
the whole package before it says anything, and reports the narrow answer and
the wide one together. A rule that has to be remembered at the moment of use
is a rule that will be forgotten at the moment of use.

Mutate every copy of the thing you are claiming about. Two methods that
differ by one character are two mutations, not one. `Style` and `StyleRunes`
both guard against a negative position; ken-mba mutated the guard in
`StyleRunes`, found it unheld, and reported that neither was held. `Style`'s
guard was held all along, by the highlighting corpus, which drives it with
negative positions. The wrong half of that claim nearly became a rule in the
section above before it was measured. This is the same shape as a test that
covers one of two copies of a rule, which happened the same week to
`Write` and `WriteString`: the difference is only whether the copy you
skipped was in the test or in the mutation.

Coverage answers whether a line ran, which is not the question anyone is
asking, and it gets both of the shapes below wrong in opposite directions. A
check tagged out of the build never runs and coverage shows the gap, which is
the easy one. A line whose value is discarded runs every time and coverage
shows green. windows-vm's pairing: both are invisible to the tool you would
reach for first, and for the same reason.

A line that runs and proves nothing. `history.load` returns an error in three
places, and the third — a line in the file that cannot be read — was reached
by `TestHistoryLoadStopsAtABadLine`, which had been running it for a long
time and threw the error away with an underscore. So swallowing the error
came back uncaught. Found by ken-mba, seconding the two error paths I had
added and noticing the third. It is not that nothing reached the path: it is
that the thing reaching it discarded the value, which is coverage that proves
nothing, and the same family as a corpus that was never attacked.

A premise nobody checked on the system it was about. A test for the error
`LoadHistory` returns reached the opening half with a path that goes through a
file, and its comment said that cannot be opened "on every system this builds
for". It is ENOTDIR on Unix and reads as a missing path on Windows, and a
missing file is deliberately not an error, so the whole subtest passed there
with a nil error. Found by windows-vm running it. The universal claim was the
fault, not the construct: nobody had asked two of the three systems. It uses
a zero byte in the path now, which no system allows and none reports as a
missing file.

New API with nothing behind it, which is the plainest shape and the easiest
to leave. Three reshaping passes added `Style` and `StyleRunes`, `FromColor`,
`Color.RGBA`, `LineStyle.Text`, `Completion.Text` and `Completion.Cursor`,
and the example exercised all of them while nothing in the package did,
because the example runs as a separate program and its coverage is not
recorded. Found by reading `go test -cover` rather than by anything failing:
forty functions at zero, of which those were the ones a caller reaches.

A claim that someone checked something is theirs to make, not yours to
infer. The last commit before this one was announced as "verified on both
platforms" while the macOS session had not yet seen it. It was green, and
nothing turned on it, and that is the point: the answer being right is not
what makes the claim true. This is the same species as inventing a cause for
a mutation that was almost certainly true and unmeasured — and it happened in
the sentence claiming the work was verified, which is where it costs most,
because a reader takes "verified" as meaning somebody looked.

Explain a gap, and say whether it is a gap. Four or five comments in this
package now say why something is not checked, and each one has stopped the
next reader taking the hole for a decision. The exception is the one that
proves the rule needs its second clause: `editor_test.go` explained, entirely
correctly, that the corpus does not record whether an operation changed
anything, because the C has no such value. The explanation was right, and its
rightness is what made the hole invisible — a good account of why something
is not checked reads to the next person as a reason not to check it. So
saying why is not enough. Say also whether anything else holds it, and when
nothing does, say that.

Three layers that each look like coverage, none of which covered it. The
initial escape wait is 100ms everywhere and 200ms on macOS. The function
picking it was untagged, so every system compiled both figures; a test named
both figures; and a second, untagged test mentioned the function. Nothing
checked that any figure was right.

`TestDefaultEscInitialIsSane` named both — and rebuilt the same
`runtime.GOOS` branch to decide which to expect, so a Linux run compared
100ms against 100ms and never looked at the 200. It also sat in a file tagged
`unix && !aix`, so Windows, plan9 and js ran neither figure: `go test -run
TestDefaultEscInitialIsSane` there prints `no tests to run`, which is not a
skip and says nothing. And `decoder_test.go`'s untagged mention asserts
`escInitialTimeout == defaultEscInitial()`, which ties the constructor to the
function and passes whatever the function returns.

Measured at 91335a9, all three ways. ken-mba changed the 200 to 300: the test
failed on darwin, a linux vet of that same tree exited 0, and running it here
on linux passed. windows-vm ran the same mutation on Windows and got `no
tests to run`. So the figure was checkable on exactly one machine, and the
comment above it claimed every system's run checked the branch it does not
take.

The fix is to make the system an argument. `escInitialFor(goos string)` holds
the branch, `defaultEscInitial()` calls it with `runtime.GOOS` — still a
constant, so nothing costs anything at run time — and `TestEscInitialFigures`
names the figures for darwin, linux, windows, freebsd and solaris in an
untagged file. The 200-to-300 mutation now fails on linux, and the test
compiles into the windows, plan9 and js test binaries. `decoder_test.go`'s wiring
assertion stays, because it is the only thing tying the constructor to the
function and the table does not cover that.

The general shape: a comment claiming a test runs everywhere is mechanically
checkable against the file's build tag, and nothing checks it. Untagging the
code under test does not untag the test. windows-vm found it only by chasing
`no tests to run` rather than reading past it, while intending to report the
parameterisation as a tidy improvement to something that worked.

A constant nothing exercised, found by mutating it and believed only after
the behaviour was shown. `termiosSetFlush` is `TCSETSF` on the System V side
and `TIOCSETAF` on the BSD side: both set the terminal attributes *and* empty
the input queue, where `TCSETS` and `TIOCSETA` set them and leave it alone.
The C asks for the flush by name, with `TCSAFLUSH`. ken-mba changed
`TIOCSETAF` to `TIOCSETA` on macOS and the whole suite passed; the matching
change here passed too. `termiosGet` was pinned; its neighbour was not.

The part worth copying is what they did next, which was to say the finding
was two thirds of a finding and hand it over unfinished. A mutation nothing
catches is not yet a defect: it is either an untested promise or an
equivalent mutant, and the difference needs a demonstration. Their probe hung
and they called it open rather than reporting whichever answer they had.

What made handing it over cheap was not the restraint but the precision, and
that is ken-mba's own correction to this entry rather than a reading of it:
the missing third was named exactly — a mutation that is not caught, a probe
that hung, and no demonstration either way — so picking it up cost one
afternoon's worth of context rather than a re-derivation. An unfinished
handover that cannot say which third is missing costs the receiver more than
finishing would have cost the sender. They also noted the motive was partly
fatigue rather than judgement, which is worth recording because the rule has
to work on a tired afternoon or it is not a rule.

It was the promise, on both families. Measured on a pseudo-terminal on linux:
with the flushing constant, four bytes typed before raw mode started were
waiting beforehand and gone after, and nothing was read; with the
non-flushing one the four bytes were still there and the editor read the `a`.
ken-mba then measured the BSD constant on darwin, which is the side the
finding came from: `TIOCSETAF` flushed and `TIOCSETA` left the `a` waiting.
So the mutation is not equivalent on either family, and a keystroke typed
before the prompt was drawn would have been read as though typed at it.

The reason a probe hangs is the line discipline, and it was the whole
difference between the attempt that failed and the one that worked: in
canonical mode a byte-count ioctl reports nothing until a whole line is
present, so a probe that types bare characters and waits for one to be
waiting waits forever. Typing `abc\n` instead finished ken-mba's probe in
0.31s, and the terminal echoed five bytes rather than four — the line
discipline adds the carriage return.

`TestRawModeDiscardsWhatWasTypedBeforeIt` covers it now, and it is built
against this section's own rule rather than trusting the green. It waits for
the terminal's echo instead of sleeping, because the echo coming back is what
proves the bytes reached the queue, and a test that flushes an empty queue
would otherwise pass while checking nothing. A byte-count ioctl would say so
directly and is not portable: of the systems this builds for, only Linux
spells one that `x/sys` exports. After the flush it writes one more byte and
requires it to arrive, so the silence is a flushed queue rather than a
terminal that stopped delivering — without that byte the test cannot tell a
flush from a terminal that stopped delivering, and would pass for the wrong
reason in exactly the case where something had gone properly wrong. Both
halves were then broken on purpose:
the mutation fails it twice over, the second failure reading `b` where `z`
was sent because a kept queue shifts every later key, and removing the typed
line fails it with the message saying nothing was waiting.

Reading a green exit code from a command that does not answer the question.
Three times in one afternoon, by two different sessions, and the same shape
each time: `go vet` was run on a deliberately broken tree and its success
reported as evidence about tests. It is not. Vet type-checks, so a wrong
*value* passes it every time — ken-mba nearly reported a clean linux vet of a
tree with the escape figure mutated as proof the figure was not portable,
which is true, but the vet does not show it; and the same reflex, in the
other direction, invented the rule that vet cannot see test files at all.
Both are assumptions about a tool's scope presented as facts about the
code. The rule is narrow: a mutation is answered by running the thing that
would fail, and when that cannot be run, by reading the source that decides
it. An exit code from a command that never executes the assertion is not
weak evidence, it is none.

The untested side was untested because it is the side this machine does not
take. Three findings in two days took this shape, and the common factor is
not the kind of value. ken-mba's were a number and an ioctl request;
this one is a whole code path. In each case the value was handled correctly
wherever anyone ran it, and nothing read back the branch the developer's own
system never reaches: the macOS escape figure from Linux, the flush constant
from a machine whose termios request is the other one, and the logged
password path from a Unix that always takes the unlogged one.

That is a different failure from forgetting to test something. The test
exists, it runs, it passes, and it exercises the arm this machine compiles.
What is missing is a reason for any machine to execute the other arm, and no
amount of care on one system produces it. The fixes have all had the same
shape too: make the branch a value a test can name — a parameter instead of
`runtime.GOOS`, a mutation at the use site instead of the definition, a
compile-time assertion instead of a runtime one — so that the arm not taken
is still checkable from here.

Enumerate the arms; do not list them. The platform split above was got wrong
twice before it was got right, and both times by measuring rather than
guessing. ken-mba looped over seven targets typed by hand and reported the six
that answered yes, which was true of every target the loop was given and
false as a statement about the port: linux was never asked. The correction
named eight and was also a typed list, two short. The right answer came from
`go tool dist list`, which knows what the arms are. A loop cannot report on
what it was not given, and its output looks identical either way — so a
measurement over a hand-written list carries the author's blind spot into
something that reads like data. This belongs with the fixes above: replacing
a sense of which arms exist with something that knows is the same move as
replacing `runtime.GOOS` with a parameter.

What to do about it. Write the expected value from the C, the specification
or the intent, never from running the code and recording what came out.
Before landing a corpus, break the code it covers on purpose, once per thing
the corpus is meant to pin, and check that each break fails it; a break that
does not fail it names either a missing case or a line that nothing reads,
and both are worth knowing before the corpus is trusted. When a test can
skip, make the skip say what was not checked rather than why it could not
be. And when a check is worth having on every system, make sure it compiles
on every system, because a check that is absent is indistinguishable from a
check that passed.

## How files are organized

The rule is Gemini's, written for this project after it reviewed the layout.
It decides where new code goes without anyone having to ask.

A type does not have to live in one file. The Go standard library splits one
type across files by what the methods do: `os` splits `File` across
`file.go`, `stat.go` and `dir.go`. Group by behavior.

A file past five hundred to a thousand lines is a reason to look, not a limit
to obey. `bbcode.go` is long and stays whole, because the attribute that
carries a color, the markup that names it and the highlighter that applies it
are one subject. A split there would cut between an attribute and the markup
for it. A file that long which covers three subjects is a different matter and
splits by behavior.

A color itself is not part of that subject, which is why it is not in that
file. What a color is, and how one is reduced to what a terminal can actually
show, is answerable without knowing anything about markup, a line, or a
terminal session, so it is the `ansi` package and a caller can use it alone.

Two constants, two axes, and files named for neither. The per-system termios
files looked like four copies of the same idea, and the note against them
said linux and solaris were identical so consolidate. Measuring first showed
why they were identical: the ioctl pair splits System V against BSD, and the
escape timeout splits macOS against everything else, so any file named for a
system has to duplicate one axis or the other. linux and solaris agreed on
both by coincidence of this particular pair, and reading that coincidence as
the structure would have produced a file that was right by accident.

The rule the project already had said it: name a file for the axis it splits
on rather than for one system on it. So `tty_sysv.go` is `linux ||
solaris` and `tty_bsd.go` is the five BSDs including darwin, each holding
the two ioctls and nothing else.

The timeout went the other way entirely, under the rule that comes first in
this document: logic specific to one system that makes no system call stays
untagged, so every system compiles it and every system's tests run over it.
It is a heuristic about how terminal emulators send alt with a key, not a
kernel fact. It is one untagged function now, with `runtime.GOOS == "darwin"`
inside — a constant, so the branch is resolved at build time — and the test
names both figures, which means every run checks the macOS one. Before this
the 200ms sat where only a Mac ever compiled it.

That removed a duplication nobody had noticed: the constructor, then `newTTY`
and now `newDecoder`, set the timeout from an untagged constant and the
Unix opener overwrote it one line later with the
per-system copy. Both were 100ms everywhere but macOS, so changing one and
not the other would have split the behaviour of a real terminal from the
behaviour of the test harness, silently.

The note's other suggestion, one `unix && !aix` file with a function or an
`init()` choosing the ioctls, does not compile. `unix.TCGETS` does not exist
in the darwin build of `golang.org/x/sys/unix` and `unix.TIOCGETA` does not
exist in the linux build, so a file naming both fails everywhere. The only
way to write it is raw hex, which trades the upstream definitions for a magic
number. Gemini and DeepSeek reached that independently and agreed on the
split; they differed on the timeout, where this document already had an
answer.

A name earns its place by saying something the place it is used does not.
`terminatingSignals` was a package-level slice of seven signals with one
reader, and `signal.Notify(d.stopCh, terminatingSignals...)` said exactly
what `signal.Notify(d.stopCh, unix.SIGTERM, ...)` says, so the name bought an
indirection and some mutable package state and nothing else. It is inline
now, with the paragraph explaining why those seven and not SIGSEGV.

Checked for others with go/ast rather than by eye, and there are none. The
seventeen composite literals with one reader are all data — the 172 colour
names, the LS_COLORS table, the recorded test cases — where the name is how
a reader finds a hundred lines of values. The twelve tiny functions with one
caller are almost all predicates, where `isHexDigit(c)` plainly says more
than the three comparisons it stands for, and several are named after the C
functions they were ported from, so inlining would cut the thread back to
the original. The test is not how many times a name is used.

A method lives with the behavior it serves, which is usually but not always
the file that declares its type. The two rules pull against each other and
the order matters: group by behavior first, and a type whose methods all do
one thing then ends up in one file without anyone arranging it.

Checked with go/ast rather than by eye, because a regex missed cases both
ways. What is left after the check is all deliberate. `env` is the session,
and its methods are in the file for the subject they serve: completion in
`comp.go`, the history walk in `history.go`, the menu in `menu.go`. Moving
them to `prompt.go` because that is where the struct is declared would empty
`menu.go` of everything it was split out to hold. `capture.Transcript.Encode`
sits in `transcript.go` with `Escape` and `writeBlock`, which are the only
things it uses. And `tty` and `fileSizer` are each declared once per
platform under mutually exclusive build tags, so the methods beside each
declaration are already at home; a tool that keys on the type name alone
reports those as strays and is wrong.

Two were genuinely astray and moved. `showSearchMatch` was in `history.go`
and writes markup into the area below the line, which is drawing rather than
history. `fieldReader` walks the fields of a recorded line and is used by two
test files, so it belonged in `common_test.go` with the other shared
helpers rather than in whichever one happened to declare it.

Platform-specific code follows four patterns, in this order of preference.

Logic that is specific to one system but makes no system call stays untagged.
It then compiles and runs its tests everywhere, so it cannot rot on the one
machine nobody builds on. Turning Windows key events into escape sequences is
written this way, and it sits inside `decoder.go`, which carries no build tag
either, so every system compiles it and every system runs its tests.

Code that orchestrates a platform-specific step stays untagged and calls a
small interface. The tagged files implement the interface. `Password` does
this: the reading, the clearing and the echo back are one untagged function,
and only turning the echo off is per system.

Code shared by a group of systems takes the broad tag for that group.
`tty_posix.go` is `unix && !aix`, and holds everything the Unix systems do
the same way.

Values that differ per system take a narrow tag, and the file is named for
what it covers. `tty_sysv.go` and `tty_bsd.go` hold two ioctl numbers
each. A system nobody mapped gets no constants and falls to the stub in
`tty_other.go`, which reads plain lines and says the terminal is
unsupported.

Name a file for the axis it splits on rather than for one system on it.
`tty_sti.go` and `tty_nosti.go` split on whether the system has the
`TIOCSTI` ioctl, which is what actually differs: OpenBSD removed it and
Solaris does not offer it, while five other systems have it.

Gemini recommends a fuzz test in a file of its own, because the corpus and
the helpers crowd out the unit tests. This project puts them together anyway,
in `decoder_test.go`, because Ken asked for fewer files and the fuzz test here is
one function over a corpus that lives in `testdata`.

## Where things live

Four packages, and only one of them is public.

`ansi` holds what a terminal understands. `ansi.go` has colors, the palette,
and the reduction that finds the nearest color a terminal can show; `attr.go`
has the attribute that carries a color and reads one out of an SGR escape
sequence; `attrbuf.go` has a run of attributes, one per byte; `names.go` has
the 172 color names and reads a color out of written text. It depends on
nothing outside the standard library, and a caller can use it on its own.

`internal/text` is the bytes below everything: the buffer a line is edited in,
the widths, the word and line boundaries, the rows and columns, the QUTF-8
codec, and in `brace.go` the matching of one bracket to its partner, which both
the editor and the highlighter ask for. It calls nothing outside the standard
library.

`internal/editor` is a line being edited and the operations that change it,
with the undo stack behind them. It depends on `internal/text` and on `ansi`
and on nothing else.

The buffer is in `internal/text` rather than in `internal/editor`, which is
where the refactor list asked for it, and the reason is what the six callers
in `rline` use it for. Only the editor edits with it. `term.go` buffers escape
sequences in one, `bbcode.go` renders markup into two, `menu.go` builds the
completion menu, `comp.go` applies a completion, `history.go` assembles an
entry, and `prompt.go` holds the area below the line. Putting it in the editor
would make the terminal writer and the highlighter import the editor to reach
a byte buffer, which is the cycle that sent `brace.go` down to the same place.

Both are `internal` on purpose. Moving the editor out of `rline` exports 42 of
its members, fourteen of them fields that the redraw path, history search and
completion write to directly. Under `internal` that is a layout; under
`rline/editor` it would be a public API frozen by the first tagged release,
for a library whose whole selling point is being a drop-in replacement. Gemini
and DeepSeek were asked separately and both said the same thing unprompted:
that a type which must export 42 members is a struct that has been moved
rather than a boundary that has been drawn, and that `internal` is where it
belongs until the fields become methods. If that refactor ever happens and the
count falls under about fifteen, the package can be promoted without moving a
line.

`rline` is everything else. The layout follows what a reader is looking for
rather than what the C file it came from was called, so several C files land
in one Go file and the header of each says which.

  rline.go       the public interface, the session log, and reading a
                 password with no echo
  prompt.go      reading one line: drawing, the hint, resize, dispatch, help
  comp.go        completions, completers and file names
  menu.go        the menu of completions, which reads its own keys
  history.go     the history list, its file, walking and searching
  bbcode.go      markup and the highlighting built on it
  term.go        writing to a terminal
  tty.go         reading keys, decoding escape sequences into them, and
                 turning Windows key events into sequences: that last part
                 carries no build tag on purpose, so it is compiled and
                 tested on every system rather than only on Windows

Per system, one file each where the tags allow it. `sys_darwin.go`,
`sys_nondarwin.go`, `sys_unix.go` and `sys_windows.go` each hold everything
that shares their tag. Four files keep their own tags because no other file
shares them: `tty_posix.go` is `unix && !aix`, `tty_sysv.go` is
`linux || solaris`, `tty_bsd.go` is the five BSDs including darwin, and
`tty_other.go` and `termsize_other.go` are both
`(!unix || aix) && !windows`, which is the fallback.

Test files follow the source files rather than the old one-to-one pairing,
with one exception: `driven_test.go` holds the keystroke harness and the tests
built on it, because those are one thing and neither half is useful alone.

## Continuous integration

`.github/workflows/test.yml` runs the checks below on four hosted runners:
Linux on amd64, Linux on arm64, Windows on amd64 and macOS on arm64. Between
them the three supported systems are covered, and two of them on both
architectures.

Two things about it are deliberate rather than convenient.

It installs the Go the module asks for, through `go-version-file`, rather
than the newest. A library that builds only with the newest Go is one its
consumers cannot use, and this one dropped to 1.25.0 so that FreeBSD could
build it at all. Testing on a newer Go than the module names would not find
the case where someone on 1.25 cannot compile it.

It skips one test on Windows rather than the package that holds it. `TestGoldenSetIsPresent` fails there
by design, because recording a session needs a pseudo console that nobody has
written. A job that is always red teaches people to ignore it, so the gap
lives in this document instead.

Dropping the whole package was the first attempt and was wrong. windows-vm
pointed out that it would take `TestRecordIsUnsupported` with it, which is
tagged `!linux && !darwin` and exists to check that very platform. The only
automated Windows environment anyone has would have stopped running the one
test written for Windows, which is the tagged-out shape from the list below,
chosen deliberately this time. `-skip TestGoldenSetIsPresent` costs nothing
and they measured it green there.

Two more things the first run found, both about assuming the runner is like
this machine. The vet loop pins `GOARCH=amd64`, because it is about build
tags rather than architectures and several of those systems have no arm64
port: the arm64 job failed on `unsupported GOOS/GOARCH pair dragonfly/arm64`.
And `.gitattributes` forces LF, because git on Windows checks out CRLF by
default and gofmt then reports every file as unformatted — fifty-two of them,
none actually changed. That one matters beyond formatting: the corpora are
compared byte for byte, so a rewritten line ending would fail tests that are
right, on the platform least able to explain why.

The race detector step is the one thing in this project that needs a C
toolchain on Windows. Everything else is arranged so that a Windows
contributor needs no compiler, and someone running these steps by hand
without one will be told `-race requires cgo` and think they have broken
something. The hosted runner ships mingw-w64.

One thing to know before trusting a green run: the arm64 Linux runner is
free for public repositories and needs a paid plan for private ones. On a
private repository that job can sit queued rather than fail, and a queued job
is not a passing one.

## Checks

Four checks run on the Go code, plus one that runs when someone remembers to.

1. `gofmt -l .` names any file that is not formatted.
2. `go vet ./...` reports suspicious code.
3. `go vet` for every system, not only the three. A build is not enough: a
   test file whose tag no longer matches its source still compiles on the
   systems where both are excluded, and only vet on a system where they
   disagree says so. That happened the moment the Unix tag widened, and
   what is now `tty_other_test.go` kept the old tag for an hour. The systems
   are not named: the loop enumerates `go tool dist list`, which is 47 pairs
   and 15 systems, for the reason in the entry below. This also checks that
   the fallback still compiles. `tty_other.go` exists so
   that such a system builds and reads plain lines, and a function added with
   implementations for only two of the three tag groups breaks it invisibly:
   vet for linux, darwin and windows all pass, and nobody builds the rest.
   That happened at 95ec387, when `fileIsTerminal` was split out of what was
   then `isATTY` and is now `isTerminal`, with no answer here, and nothing
   said so for a day.
Enumerate the targets; do not name them. The loop here and the one in CI
were both hand-written lists of ten systems, and both were reported as
coverage for months. `go tool dist list` gives 47 pairs across 15 systems, so
the lists were silently missing aix, android, ios, js and wasip1 — and aix is
a system this package makes a specific claim about, in the `unix && !aix` tag
that four files carry. Nothing in the output said so, because a loop cannot
report on what it was not given and ten clean lines read exactly as
forty-seven would.

Enumerating also removed the pinned `GOARCH=amd64`. That pin existed because
a sweep over arm64 hit `unsupported GOOS/GOARCH pair dragonfly/arm64`; an
enumeration never proposes a pair that does not exist, so the loop now covers
both architectures wherever the toolchain has them. `android/arm64` vets
clean while the other three android pairs do not, which a per-system list
could not have expressed at all.

A target that cannot be asked is reported, not skipped — and "cannot" turned
out to be two different things. android and ios need external linking, and
`CGO_ENABLED` defaults to 0 for a cross-target, so the first sweep counted
five pairs as unaskable. ken-mba separated the cases: four need a toolchain
nobody has, and `ios/arm64` merely needed the variable, because a Mac's own
clang can target it. That is a missing NDK in one case and an environment
variable in the other, and only the first is a fact about the machine.

Being askable is a property of the pair and the machine together, not of the
pair — and the reason is the host's C compiler, which windows-vm found by
reading the *second* error rather than the first. With cgo off the message is
`requires external (cgo) linking, but cgo is not enabled`, which is about the
build settings. Turn cgo on and the message becomes `C compiler "gcc" not
found` or `C compiler "clang" not found`, which is about the machine. The
first was hiding the second, so the retry is not only a way to ask more
pairs, it is what makes the failures say why.

Each target names the compiler it wants: android asks for gcc, ios for clang.
So a pair is askable where the host has a compiler that can *target* that
GOARCH, and presence alone is not enough. Measured here, on a box that has
both gcc and clang: `android/arm` fails with `gcc: error: unrecognized
command-line option '-marm'` and `ios/arm64` with `gcc_arm64.S:30:19: error:
expected ']'`, both of which are a compiler that exists and cannot aim there.
`android/arm64` links internally and needs no external compiler at all, which
is why it is clean on every machine with cgo off or on, without being a
special case.

That accounts for every cell in the table above, including the two
asymmetries, and it explains the one pair nobody has asked. `android/arm`
fails on both machines for the same reason wearing different clothes: here
`gcc: error: unrecognized command-line option '-marm'`, and on ken-mba's Mac
`clang: error: unsupported option '-mno-thumb' for target 'arm64-apple-darwin'`
— each host compiler refusing a flag the toolchain emits for 32-bit ARM,
because the compiler being invoked is the host's and not a cross one. So the
last cell needs an NDK rather than a variable, and that is the single piece
of work between 46 of 47 and all of them. It is a toolchain to install, not
anything in this tree, and nobody is proposing it: installing an NDK to vet
one pair of a package with no cgo is a poor trade, and the record saying
which pair is unasked and why is worth more than the pair. It also makes
predictions — the GitHub Windows runner ships a mingw gcc, so the three
android pairs that fail on a bare Windows box should go clean there while ios
stays red with a clang message. If android stays red the rule is wrong and
the cause is something other than compiler presence. The prediction being
falsifiable on a runner we already have is the part worth keeping; the rule
is only as good as the next matrix run. With cgo on, this Linux box answers `android/386`, `android/amd64` and
`ios/amd64`, which ken-mba's Mac cannot; the Mac answers `ios/arm64`, which
this box cannot. Neither answers `android/arm`. So between two machines,
five of the six pairs are reachable and each of us had reported a different
subset of them as impossible. The loop now retries with `CGO_ENABLED=1`
before giving up, which takes this machine from five unaskable to two, and
the matrix is what makes it worth doing: the union across runners covers
more than any single job, and no job can know that alone.

Dropping the rest would be the same fault as the hand-written list — a loop
that quietly skips what it cannot build reports clean for the wrong reason —
so the loop counts them and says how many, separating the two kinds. Checked
twice by breaking the tree on purpose, because a loop just taught to tolerate
a class of failure is the one that starts passing for the wrong reason: an
undefined symbol in `sys_windows_test.go` exits 1 at `windows/386`, and one
in `tty_other.go` exits 1 at `aix/ppc64` — a platform the ten-name list never
asked at all.

The general form is ken-mba's and outlives this loop: an exit code that means
two things is the same fault that cost the mutation harness its third answer,
where vet exiting non-zero for a build failure and for a diagnostic were
indistinguishable. Here it was "cannot ask" and "did not ask correctly".

The retry only vets the same program because this module has no cgo, and that
was measured rather than assumed on the way in: `go list` reports zero
`CgoFiles` in all seven packages, and on every target where both invocations
can list at all the file set is byte-identical with cgo off and on. What
changes is the link mode. If a cgo file ever arrives, the retry starts
quietly vetting a different program and this paragraph is the thing that
should have been read first.

An answer that arrives exactly shaped to end the investigation deserves the
check you were about to skip — with ken-mba's caveat, which is what keeps it
from being a rule that covers more than it does. It worked for them because
the convenient answer was also a large claim, and large claims are where
anyone is already primed to look. A convenient answer that is small would not
trip it, and the small convenient answer nobody bothers to check is exactly
where this fails. There is no fix for that, so it is written here as a
detector with a known blind spot rather than as a practice.

A correction placed beside the error instead of on top of it. The comment
above the no-echo guard claimed that if its build tag and `sys_unix.go`'s ever
disagreed, the line would stop being compiled "which is the failure it exists
to catch". That is false: the guard catches a lost `startNoEcho`, and a lost
tag makes it silently stop existing. When ken-mba measured that and sent it,
the sentence added was true — the pairing is hand-maintained and nothing
enforces it — and it was appended to the false clause, which stayed. The
paragraph then asserted both things four lines apart, and the false one came
first, so a reader who stopped early got the wrong answer and a reader who
went on got a contradiction.

Worse than the gap it was fixing, and worth separating from it. A missing
record leaves a question open and the next person finds nothing; a record
that denies the gap answers the question wrongly, and it answers it in the
very place that would otherwise have prompted someone to check. The fix is to
delete the wrong clause rather than to qualify it — appending is what feels
like correcting, because the new sentence is true and adding it is the part
that takes effort.

And neither the writer nor the conversation could have caught it. ken-mba had
a message saying the fact was recorded, which it was, and no reason to open
the file; the author read the paragraph already knowing what it was meant to
say. It surfaced only because ken-mba applied to this record the check this
document had just recommended for theirs. That is the argument for the
arrangement, better than the platform columns were: not that two measure more
than one, but that nobody can audit their own record.

Getting that check wrong is its own entry, and it is a shape not yet in this
list. ken-mba first compared hashes of `go list` output across the two
invocations and found `ios/arm64` DIFFERS — on the one pair the whole
correction rested on. It does not differ. With cgo off, `go list` on
`ios/arm64` fails outright and prints the same `requires external (cgo)
linking` message, so the hash being compared was an error message against a
file list. A comparison cannot tell "these differ" from "one of them did not
run", and it reports the second as the first. The fix is the same as
everywhere else here — check that both sides succeeded before comparing what
they said — and the comparison in this document's own check has an explicit
"one failed, not comparable" branch for that reason.

And the retry fires only on failure, which is a different loop from one that
always runs with cgo on. Counted: 47 pairs, 5 first-pass failures, 5 retries
attempted, 42 pairs that never reached it. ken-mba computed the macOS column
and got the same arithmetic with different membership — 42 clean, 1 rescued,
4 still failing — where the one it rescues is among the two this box cannot
ask and three of its four failures are the ones this box rescues. Between the
two, 46 of 47 pairs are vetted and `android/arm` is the only pair neither can
reach.

Which leaves a reporting problem the matrix does not solve, and this is a
decision rather than a fix. A pair that no runner can ask and a pair that one
runner covers are different facts, and a per-job report cannot tell them
apart: summarising by the worst answer per pair calls four covered pairs
unasked, and by the best answer loses that those four rest on a single
runner. ken-mba's answer is the best answer with the runner named beside it,
so that losing a runner shows up as coverage moving rather than as nothing
changing. That needs the jobs to pool their results, which GitHub Actions
does not do without an artifact and a collecting job, and none of that is
built. What exists is each job printing its own counts, and this paragraph
recording the union. If the matrix ever loses a runner, nothing will say
which pairs went with it.

Cross-vet is the whole type-check, including test files. `go vet`
type-checks a package's tests as well as its source, so a rename that breaks
a file tagged for another system fails the cross-vet from any machine.
Measured both ways rather than assumed: an undefined symbol appended to
`sys_windows_test.go` on linux gives `GOOS=windows go vet ./...` the same
error and the same exit code as `GOOS=windows go test -c`, and windows-vm ran
the mirror, breaking `tty_test.go` — tagged out on Windows — and finding
that `GOOS=linux go vet ./...` catches it while the native vet correctly does
not. So `limitToLength` was not a gap in what the loop can see; it was a gap
in the loop being run.

What that leaves for another machine is the run, not the build. `go test -c`
can in principle fail where vet passes — link-time symbols, cgo, assembly, a
`//go:linkname` that resolves to nothing — and none of those apply to this
package, which is pure Go. So a remote platform is asked whether the tests
pass, whether a real console behaves, and whether the banner still hashes to
its baseline. It is not asked whether the names resolve.

4. `go tool golangci-lint run ./...` runs the linters that `.golangci.yml`
   names.

`go.mod` pins golangci-lint with a `tool` directive, so `go tool` builds it
with the toolchain of this module. A golangci-lint binary built elsewhere can
fail to read the export data of a newer Go release, and then it reports every
standard library import as an error.

5. `modernize ./...` names constructs that a newer Go writes better. It is
   not pinned and not in the module, because it rewrites source rather than
   judging it: pinning it would freeze the definition of modern, which is the
   one thing about this check that should move. Install it when it is wanted:

       go install golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest

   Then `modernize ./...` to see what it would change and `modernize -fix
   ./...` to let it. Read the diff rather than trusting it: of the fifteen it
   found here, fourteen were right as written and one produced
   `_, after, ok := strings.Cut(...)` followed by `rest := after`, which is a
   correct rewrite and a worse line than naming the value at the Cut.

## What rline offers usql

Ken set the direction on 2026-09-20 and it decides every question in this
section: **rline never adapts to usql.** usql is the program this port exists
for, but it is meant to be a blind consumer of a package that knows better,
and it will adapt here once this is working. Where usql's current reader does
something poorly, the answer is to do it well and let usql come to it, not to
grow a second way of doing it that matches what usql has.

So the eleven methods of `usql/rline.IO` are read here as a list of things
usql will need to do, not as a list of shapes to fit.

That change of question is also why the section can stop drifting. "What usql
needs" is a question about a moving target, and it produced two wrong answers
from two people in opposite directions, both by asking what would fit. "What
rline offers" does not depend on what usql happens to do this month. The
error stream is the clean instance: usql's two streams interleave however the
operating system buffers them, so the thing worth building was never a second
destination — it was the ordering guarantee, and that only became visible
once the question changed. ken-mba's observation, and the best argument for
the principle that either of us has made.

Five need nothing. `Close`, `Interactive` and `Password` map straight across.
`Completer` maps to `SetCompleter`, which also lets usql replace the completer
when the connection changes, which is what it does today. `Stdout` maps to the
`*Prompt` itself, which is an `io.Writer`, and a better one than `os.Stdout`
because the terminal it writes through is the one that knows where the prompt
is.

Three are renames usql makes in its adapter, and each is the better shape.
`Next` returns runes where `ReadLine` returns a string and can also say that a
line was given up rather than ended. `Prompt(string)` sets a whole prompt
where `SetPrompt` takes a marker and a continuation marker, which is what a
statement spanning rows needs. `Save(string) error` does two things where
`AddHistory` and `SaveHistory` do one each.

`Cygwin` is a concept this package should not have. The Windows console is
handled natively, so there is nothing for usql to ask. It drops the method.

`Stderr` is built, and is better than what usql has. `WithStderr` says where a
program's errors go and `Session.Stderr` returns the writer. usql today
writes to `os.Stdout` and `os.Stderr` directly — `readline.Stdout` and
`readline.Stderr` in gohxs/readline are plain package variables holding those
two, `std.go:12` and `:13` — so its two streams interleave however the
operating system buffers them. Here they keep their order, because the
terminal is flushed before each error.

`SetOutput` is the one thing usql should stop doing rather than the one thing
this package should add. Its `outputHighlighter` at `handler/handler.go:134`
takes the accumulated statement buffer from previous reads, prepends the line
about to be drawn, re-parses the whole thing with the driver's SQL parser,
highlights all of it, and returns only the last line with a
colour-continuation prefix. All of that exists because gohxs/readline hands it
one line at a time while a SQL statement spans several.

It does not have to. `WithContinue` keeps a whole statement in one buffer
across as many rows as it takes, and hands it back at once, which is what the
example demonstrates. `h.buf` is reset after each statement and on interrupt
— `handler.go:262` among others — so it never holds more than the one
unfinished statement `WithContinue` already holds. A `Highlighter` therefore
sees the whole statement as it is typed, and marks it with named styles
rather than returning a decorated string with a colour left open.

That last detail is not incidental: the open colour is what made the newline
bleed in `MarkupWriter` a real fault, and the structured interface cannot
produce it.

This is the claim in the section most worth checking before it is relied on,
because it is the one nobody has built yet. The check is cheap: point a
`Highlighter` at a multi-row statement and see whether it is handed all of it.

### Ctrl-C now says so, which is the one behavioural departure

usql cannot work without telling Ctrl-C from an empty line. Its loop reads
`case err == rline.ErrInterrupt: h.buf.Reset(nil); continue`, which is how a
user abandons a half-typed statement, and `main.go` lets that error out of the
program without reporting a failure.

The C cannot say it. Ctrl-C deletes the line and ends the loop, and the C
hands back an empty string, byte for byte what Enter on an empty line gives.
That is deliberate rather than a fault: `editline.c:949` says "ctrl+G or
ctrl+c cancels (and returns empty input)". So this was the one place where
fidelity to the C and the needs of the program this port exists for pointed in
opposite directions.

Ken decided for the program. `ReadLine` answers `ErrInterrupted` for Ctrl-C
and for Ctrl-G, which the C treats alike. Ctrl-D on an empty line still
answers `io.EOF`, and the two are deliberately not folded together: one says
the line was given up, the other says there is no more input.

This is the only place where the port departs from the C over a behaviour
rather than a fault, and it is listed under the departures as well.

### What the count is now

Five need nothing, three are renames usql makes, `Cygwin` is dropped,
`Stderr` is built, and `SetOutput` is a thing usql stops doing. That is the
eleven, and nothing on the list is waiting on this package.

This section is checked against a usql checkout rather than remembered, and
it has drifted once already: `Stderr` was recorded as fitting because a
`*Prompt` is an `io.Writer`, which is true and is not the question. Anything
written here about what usql does should be re-read against the source before
it is relied on, with the file and line beside it as above.

The drift went in while this section was being rewritten against the new
names, not while it was being researched: the first audit said outright that
`Stderr` had no answer, and the rewrite upgraded it to fitting on a true
statement that answered a different question. That is the ordinary way a
document goes wrong, and it is why the file and line matter more than the
sentence — ken-mba found it by opening `usql/rline/rline.go` rather than by
re-reading this.

## The terminal types are named for what they are

Three types shared the terminal between them and only one was named for what
it does. `ttyDevice` held the file descriptor, the saved and raw termios pair
and the signal watchers, which is what a tty is. `tty` held two pushback
buffers and sixteen escape-decoding methods, plus five that forward to the
device — and it runs with no device at all, which every test that feeds it an
`idleReader` proves. `term` is the output half and was already right.

So the device is `tty`, the decoder is `keyDecoder`, and the constructors
follow: `openTTYDevice` is `openTTY`, the old `openTTY` is `openDecoder`, and
`newTTY` is `newDecoder`. The field on `env` that paired with `term *term` is
`keys *keyDecoder`. Files follow the types: `tty.go` is `decoder.go` and the
`ttydev_*.go` family is `tty_*.go`, which crosses — the old `tty_test.go`
tested the decoder and is `decoder_test.go`, while `ttydev_test.go` takes the
name `tty_test.go`. Tests named for the wrong half were renamed with them,
including three whose names had become inverted: `TestOpenTTYDeviceRejectsAPipe`
called `openTTY` and `TestOpenTTYRejectsAPipe` called `openDecoder`.

`gopls rename` did the work for the systems it can see, which on Linux is
every file except the Windows pair, `tty_other*.go` and `tty_nosti.go`. Those
were patched by hand in an order that word boundaries make safe: `openTTY`
before `openTTYDevice`, since `\b` does not match inside the longer name, and
`*tty` before `ttyDevice`, since renaming the device first would have created
`*tty` occurrences that must not move. Fifteen `GOOS/GOARCH` pairs vet clean,
including plan9, js and aix, which is the check that covers what gopls could
not see.

The one thing the rename must not break is that `TestEscInitialFigures` stays
untagged. It was in `tty_test.go`, which is now `decoder_test.go`; the file
that inherits the name `tty_test.go` is tagged `unix && !aix`. Checked rather
than assumed: the test is still in the windows test binary.

### The interface keeps a method the decoder never calls

`terminalDevice` is `terminalController`, and narrowing it to match its name
was tried and reverted. Inside `decoder.go` the interface is used only to
control — `ctrl` appears ten times and all ten are in the five forwarding
methods, while reads go through `src` — so the embedded `byteReader` looks
like a dependency nothing uses. It is not. `readNoEcho` in `rline.go` reads a
password through `ctrl` on purpose, because `WithLogger` wraps `src` in a
`logReader` that records every byte and the terminal underneath is never
wrapped. Reading the password through `src` would write it into the session
log.

Two models were asked and both endorsed the narrowing, because the fact they
were given — that the interface is only ever used for control — was measured
in one file and stated as though it held for the package. It holds for
`decoder.go` and is false for `rline.go`. The compiler caught it, which is
luck: had the password path used the decoder's own `readByte`, the narrowing
would have compiled and quietly started logging passwords.

## A password reaches the session log on the systems without no-echo

Found while narrowing `terminalController`, and not fixed here because the
fix is a decision about what a logger is for.

`Password` has two paths. Where the terminal can turn echo off and leave its
own line editing on, it calls `readNoEcho`, which reads through `ctrl` and so
never touches the `logReader`. Where it cannot, it goes to raw mode and calls
`readHidden`, which reads through the decoder — `keys.read()`, then `src`,
which `WithLogger` has wrapped. Every byte of the password is recorded.

`startNoEcho` is declared in `sys_unix.go` alone, tagged `unix && !aix`. The
split was taken from `go tool dist list` rather than from a list anyone typed,
which took three attempts to get right and is the method note below. Ten
targets compile that file and take the unlogged path — android, darwin,
dragonfly, freebsd, illumos, ios, linux, netbsd, openbsd and solaris — and
five do not: aix, js, plan9, wasip1 and windows. Of those five only Windows
and aix have a terminal to speak of, so Windows is where it matters.

The branch is a runtime type assertion rather than a build tag, which matters
for how it was checked. ken-mba measured it on a real pseudo-terminal on
darwin — `ctrl.(noEchoDevice)` is true there, so `Password` calls
`readNoEcho` — and windows-vm measured the other side on a real console,
where the assertion is false and `Password` takes `readHidden`. Both went
through the public API rather than calling the inner function, which is what
makes them statements about what a caller gets.

Demonstrated rather than reasoned: driving `readHidden` with a logger over a
fed `hunter2` leaves the log holding

    "> h\n> u\n> n\n> t\n> e\n> r\n> 2\n> \\r\n"

which is the password, one byte to a line. Worth keeping the shape of that
string, because the obvious test for this bug does not find it: a
`strings.Contains(log, "hunter2")` returns false. A check written the natural
way would have passed while the password sat there in full.

And "recoverable" undersells it, which is windows-vm's correction after
seeing the real artefact. The bytes are in order, one per line, in a column,
with nothing between them: a person who opens the log reads the password at a
glance, more easily than if it had been written as one string, because it is
the only column in the file. So the two facts point opposite ways. A human
sees it immediately; a grep, a secret scanner looking for a known value, and
a test asserting the password is absent all answer no. The only thing that
fails to find it is a program looking for the obvious, which is every cheap
way of asking.

What defeats the check is the framing interleaved with the content, not the
escaping. A scanner that strips the direction prefixes and joins the read
lines finds the password at once. So the rule for a log format that has to
survive a secret scanner is that framing must not be able to split a value
across records — or, from the other side, any check on this log has to
reassemble before it searches, and the obvious check does not.

The safe path is safe by accident, and now says so. `Password` reaches
`readNoEcho` through a type assertion, so if `*tty` stopped satisfying
`noEchoDevice` — the method moved, renamed, or given a narrower tag — the
assertion would just be false and every Unix would fall through to the logged
path. Nothing would fail, and the only signal would be passwords in logs
where the natural check does not find them. `tty_test.go` now carries
`var _ noEchoDevice = (*tty)(nil)`, in a file whose build tag is the one on
`sys_unix.go`, so the breakage is a compile error. Checked by renaming
`startNoEcho`: vet reports `*tty does not implement noEchoDevice`, and
ken-mba confirmed the property that actually matters by running the same
rename from illumos, android, linux and dragonfly — targets that machine
never takes — and getting the error from each, while windows and aix exit 0
because the tag excludes them, which is right.

What the guard cannot guard is its own tag, measured rather than worried
about. Narrowing `tty_test.go` to `//go:build linux` and changing nothing
else leaves vet at 0 on darwin, illumos, dragonfly and linux, and the suite
green: the guard has silently stopped covering nine of the ten platforms it
was written for and nothing anywhere says so. No guard is proposed for it —
the regress has to stop somewhere and one tag is a far smaller surface than
what it protects — but the pairing with `sys_unix.go` is hand-maintained
rather than derived, and that is now written beside it so the next person
knows which fact to keep true.

The guard is sufficient only because three separate facts hold, and it is
worth having them in one place rather than scattered across a comment, a
commit message and a peer's message. The assertion covers the *type*.
`ctrl` having exactly one concrete type on those systems — `openDecoder` is
the only assignment outside the Windows file — is what makes a claim about
the type a claim about the *value* at the branch. And a nil `ctrl` would make
the assertion false with no panic, dropping a safe platform to the logged
path silently; that is unreachable through `New`, because `canEdit` is
`ttyErr == nil && isInteractive()` at `rline.go:591` and `ctrl` is set by
`openDecoder` on exactly the path where `ttyErr` is nil, so `canEdit` implies
`ctrl` set. A `Session` built by hand in a test can have neither, but that is
the test's doing rather than something the package can reach. Take away any
one of the three and the guard stops meaning what it is read as meaning.

Here the build tag is the claim rather than a limitation, which is worth
saying after a week of treating tags as the thing that hides checks. The
assertion is only true where `sys_unix.go` is built; in an untagged file it
would fail on Windows for exactly the reason the finding exists. Matching the
tag states the claim precisely. That is windows-vm's distinction and it is
the first time in this document a tag has been the right answer rather than
the problem.

And the two sides were not symmetrical. Unix had a compile error if the safe
path was lost; Windows had a paragraph, and paragraphs in this document have
gone stale three times. The direction that would rot it is the harmless one —
someone adding `startNoEcho` to `sys_windows.go` would make Windows safe and
this entry false, with nothing to notice. So `sys_windows_test.go` carries
`TestConsoleDoesNotOfferNoEcho`, a runtime check, because Go cannot assert at
compile time that a type does *not* satisfy an interface. It is a test whose
failure means the document is wrong rather than the code, and it says so in
its own message.

What it needs is a way to stop recording for the length of a password read,
which is a change to what `sessionLog` promises rather than to the password
code, and the logger's type is already an open question below. Fixing one
without the other would settle the second by accident.

## The shape of the test suite

TESTING.md holds the design: seven layers, what each alone can answer, what it
must not be asked, what is missing today, and what usql needs that nothing
tests. It was written against Gemini and DeepSeek asked the same question
independently, and it records where both of them disagree with what this
repository currently does rather than settling it here.

The short version of the gaps: resize and bracketed paste are untested
anywhere, history search and undo are pinned to the C and untested as
properties, there is no layer holding rline to usql's contract at all, and the
layer that drives a Session over fixed input has no name and no directory,
which is why it is the one a new feature skips.

## Tests against a real terminal

`internal/capture` drives a pseudo-terminal and compares bytes, and a
pseudo-terminal is not a terminal emulator: nothing renders the escape
sequences, so nothing checks what a person would see. It also cannot test the
other direction, because a real terminal decides for itself which bytes a
keystroke becomes, and shift-enter, ctrl-left and alt-backspace do not exist
in ASCII.

`uitest/` opens real emulators, types with the compositor's own virtual
keyboard, and records three things: the byte log, which `WithLogger` already
produces and which is uniform across every terminal; a screenshot after every
step; and, where the terminal can be asked, its screen as text. Only the byte
log is compared. Screenshots are for a person, because font rendering varies
by machine, by hinting and by graphics driver, and an image comparison would
fail for reasons that have nothing to do with this port. See `uitest/README.md`
for the matrix and the reasoning behind it, which came from Gemini and
DeepSeek asked independently.

### The keys go to a compositor, not to a window

This is the thing to understand before changing any of it. A synthesised
keystroke is delivered to whatever has keyboard focus at the instant it is
sent, and a check beforehand does not make that safe: the check and the
keystroke are different moments. The first version of this harness did check,
and still typed test input into Ken's own windows twice, because a terminal
window closed between the two and focus moved on. One of them landed in the
middle of a sentence he was writing.

So on Linux the tests run on a private headless compositor with its own
runtime directory, and the injector is pointed at that socket. Nothing can
reach the desktop, because the desktop is a different compositor. That is
isolation rather than a guard, and it is the difference between a race that is
usually won and one that cannot be run.

macOS and Windows have no equivalent yet and share the desktop with a focus
check, which is the weaker arrangement. It is written down in the README
rather than glossed, and `-visible` does the same on Linux for anyone who
wants to watch.

### What isolation fixed that was not about safety

The runs were not reproducible on a desktop and the reason was the same
tiling that makes a desktop useful. A terminal opens at the size it asked for
and is then resized to fit the layout: foot asked for 80x24, got it, and was
tiled to 47x174 a second later. Every resize is a SIGWINCH and a redraw, so
three runs of one session produced 86, 66 and 42 lines. On a compositor with
one window and a fixed output they are equal.

The other candidate was ruled out rather than assumed. A redraw loop in the
port would have looked the same from outside, so the demo was left idle on a
plain pseudo-terminal for five seconds: it wrote nothing after its prompt. The
repaints were the desktop's, not the port's.

### Things it found before it was finished

An emulator's own bug, which is what a matrix is for: wezterm panics at
`window/src/os/wayland/keyboard.rs:113:38` on this machine and never draws.
Found because the harness keeps whatever the terminal writes to its stderr,
which was added after a window closed too fast for Ken to read the error in
it.

A GTK rule nobody would guess: an application id that is not reverse-DNS is
ignored silently, so ghostty and gnome-terminal kept their own and the harness
could not find its window. The window is now identified by a title the program
sets itself with OSC 0, which every terminal supports because it is a terminal
feature rather than a launcher flag — and it must be unique, since matching a
shared id like `org.gnome.Terminal` would have found Ken's own windows and
typed into one.

A path that was relative when it had to be absolute: a terminal chooses the
working directory of the program it runs, and wezterm chooses the home
directory, so the demo wrote its log where nothing was watching and the run
reported that the program had never started.

And a hole in the isolation that isolation alone did not close.
gnome-terminal does not open a window itself: it asks gnome-terminal-server
over D-Bus. The private compositor sets `WAYLAND_DISPLAY`, which that server
never sees, because it is already running on the person's own session bus and
the request goes to it. Six windows opened on Ken's desktop from one run.
Nothing was typed into them — the focus check queries the private compositor,
found no window of ours there, and refused — which is the argument for
keeping a guard behind the isolation rather than instead of it. The fix is a
private message bus, `dbus-run-session`, so there is no server to answer and
one starts inside the right environment. Measured both ways: six windows
before, none after.

The general shape is worth more than the fix. An environment variable is a
request to the process you start, and a process that delegates to another one
does not carry it: anything reached through D-Bus activation, a daemon or a
socket already in the environment is outside whatever the variable was meant
to contain. Isolation by environment holds only for children, and the leak
looks exactly like success from inside the sandbox.

### Quiescence has to be longer than the program's own delays

The rule that a step is finished when the log has been quiet for a while is
only sound if the program has stopped deciding to draw. rline has not: it
draws a hint after `DefaultHintDelay` of no typing. The harness's quiet period
was 400ms and that delay is 400ms, so the two landed together and the next
keystroke raced the hint. Any session that typed a word with a completion
behind it then differed between runs, which is most of them.

It surfaced in the history session rather than in the sessions about hints,
and that is the part worth keeping: the sessions written *about* hints carried
long deliberate waits and passed, while the one that typed `select` on its way
to somewhere else did not. A harness's timing bug hides in the tests that are
not about timing.

The quiet period is now comfortably past that delay, and both constants say in
a comment that they move together. The general form: a quiescence rule must be
longer than the longest redraw the program delays on its own, and a program
that draws on a timer has to declare that timer to whatever is timing it.

### A key injector that drops keys, found through its goldens

`wtype` uploads a keymap and then sends the key, and with no pause between
them the key can arrive before the compositor has applied the map, so it is
dropped. The text path was given `-s 60 -d 12` early, after "select 1;"
arrived as "tes aselect 1;". The key path was not, and nothing noticed for a
day, because a dropped arrow key does not look like a dropped key: it looks
like an editor that ignored an arrow.

Measured over three runs each, before and after: `hints` went from 1/3 to 3/3
and `hints-arrows` from 0/3 to 3/3. The damage was wider than the flake — the
goldens for every session using arrows had been recorded through the same
injector, so some of them had pinned runs where a keystroke never arrived. All
were regenerated. A golden recorded through a broken harness is worse than no
golden, because it pins the harness's bug as the expected answer.

### What it cannot pin down

The completion menu. Tab on an ambiguous prefix draws a numbered menu under
the line, and typing the number picks an entry — sometimes. On other runs the
same number arrives as a plain character and the line becomes `se2` rather
than `select`, so whether the menu is still listening when the next key
arrives is not something this harness can currently time. The log differed
about one run in three.

That session is marked `Watch`, which means it takes its screenshots and
compares no golden. A golden that is usually right is worse than none,
because the failures teach whoever sees them to rerun rather than to look.
What the menu does between opening and the next keystroke is an open question
rather than a settled one, and the screenshots are still there to look at.

Hints, which are the other timing-dependent feature, are pinned and stable.
They needed a fixed wait rather than the quiescence rule: the hint is drawn
after rline's own delay of no typing, so the log goes quiet *before* the hint
appears and a step that waits only for quiet sends its next key into the gap.
`hintSettle` is that wait, and it is tied by a comment to `DefaultHintDelay`
so the two cannot drift apart silently.

## The linter only ever looked at this machine

CI went red on the commit that added `uitest`, and the useful part is that
`tools/lint.sh` had said the tree was clean. It was clean, for Linux.

golangci-lint analyses the build it is pointed at, exactly as `go vet` does, so
on this machine it never opened `platform_darwin.go` or `platform_windows.go`.
Four findings went out in them: an unused function on Windows that a comment
in the file admitted was unused, an unwrapped `filepath.Abs`, an unwrapped
`exec.Cmd.Output` on darwin, and a slice that wanted preallocating. The macOS
and Windows runners found all four in the step that this repository treats as
the last word on style.

It is the same fault as a test file whose build tag takes it out of the build,
one layer up, and the lesson had already been written down here — for vet, and
not carried across to the linter. Cross-vet was widened twice this week while
the linter stayed pointed at one system.

The fix is the one the loop already uses: the linter honours `GOOS`, so it now
runs for linux, darwin and windows. Three rather than fifteen, because that is
where the platform files are and a fourth would analyse the same fallback
twice. It costs a few seconds and needs nothing installed, which is the
uncomfortable part — the check was cheap and available the whole time.

The arm runner failed for something else entirely: the linter is built by that
script, and on `ubuntu-26.04-arm` the link step called the system compiler and
got `collect2: fatal error: cannot find 'ld'`. That is the image's toolchain
rather than this repository, and the answer is that the linter needs no C at
all, so it is built with `CGO_ENABLED=0` and never asks.

## Gaps left on purpose

Everything else in this document arrived because something was wrong. These
are here because somebody weighed them and said no, and a reader has no way
to tell the two apart unless the text does it for them. ken-mba's point, and
worth the section on its own.

The flush is asserted on two platforms of seven. `termiosSetFlush` is
compiled wherever `tty_sysv.go` or `tty_bsd.go` is, which is seven systems,
and `TestRawModeDiscardsWhatWasTypedBeforeIt` needs a pseudo-terminal, so it
runs on linux and darwin only. On NetBSD and OmniOS the suite reports it as
absent rather than as passing, and a run report should say so rather than let
a green summary imply otherwise.

`capture.OpenPTY` is not untagged over unix, though it could be. It exists
for linux and darwin, and `posix_openpt` is what `record_darwin.go` already
uses and what `tools/probe-refresh.c` does for every system this builds for,
so untagging it would let the flush test follow the constant rather than the
harness and cover all five BSDs. It is not done because nobody can check it:
of those five, this machine has SSH to a NetBSD VM and none of the others,
and ken-mba has none. Adding coverage that cannot be verified on the systems
it claims to cover is the failure this whole week was about, pointed in the
direction that looks like diligence.

`android/arm` is the one vet target nobody asks. It needs an NDK on every
machine we have, for the reason in the cross-vet section, and installing one
to vet a single pair of a package with no cgo is a poor trade.

The cross-vet matrix does not pool its results. Each job reports its own
counts and this document records the union; if a runner disappears, nothing
will say which pairs went with it. Pooling needs an artifact and a collecting
job, and that is machinery for a report.

## Open questions

Seven questions have no answer yet.

First, does `rline` adopt `github.com/xo/terminfo`? isocline contains no
terminfo code. `term.c` reads the `TERM`, `COLORTERM`, `NO_COLOR`,
`WT_SESSION`, `ITERM_SESSION_ID` and `VSCODE_PID` variables to pick a color
palette. terminfo is therefore new work, not saved work.

Second, does `rline` adopt `github.com/xo/inputrc`? isocline never reads an
inputrc file, because it carries fixed key bindings. inputrc is a new feature.

Third, how does `isocline/` reach another host? It is a separate git
repository, so this repository ignores it for now.

Fourth, should the cursor be a bar rather than a box, and who decides? Ken
asked for an option, and the facts are gathered here so that the answer does
not have to start from nothing.

The sequence is DECSCUSR, `CSI Ps SP q`, with a literal space before the q.
Taken from terminfo rather than from memory: `Ss=\E[%p1%d q`. The values are
0 and 1 blinking block, 2 steady block, 3 blinking underline, 4 steady
underline, 5 blinking bar, 6 steady bar, the last two being xterm's
extension. xterm, tmux, kitty, foot, wezterm, vte and alacritty advertise the
capability; screen, the Linux console, vt100 and rxvt-unicode do not. A
terminal that does not know the sequence swallows it, so emitting it is safe,
but it cannot be relied on to work.

Restoring it is the part with a trap in it. The obvious answer is terminfo's
`Se`, and `Se` is wrong here: xterm, tmux, kitty and wezterm all give
`\E[2 q`, which is steady block rather than whatever the person had. A
library that set a bar and then restored with `Se` would leave a block behind
on the terminal of someone who had chosen a bar, which is Ken's own setup.
`foot` gives `\E[ q` instead, an empty parameter meaning the configured
default, and that is the behaviour wanted — but xterm's own ctlseqs
documents 0 as blinking block rather than as a reset, so there is no sequence
that reliably means "put it back". The honest design touches the cursor only
when a caller asks, and says in the option's documentation that it cannot be
perfectly undone.

The harder finding is that the behaviour Ken described is not a setting at
all, which is the fifth question below.

Fifth, does `rline` grow vi modes? This is Ken's, and it is what the cursor
question turned into rather than a separate idea.

A bar in insert mode and a box in normal mode is not a cursor setting. It is
two modes, and there are none here: isocline carries fixed emacs-style
bindings and this is a faithful port of them, so rline is always in what vi
would call insert. Nothing in the port refuses modes; nothing in it expects
them either. The key dispatch is one switch over a key code in
`handleKey`, with no notion of a mode to dispatch differently under.

It is a real feature rather than a rename, and it reaches into three things
at once: the dispatch, the cursor shape above, and the second question above
about inputrc, which is where a person would expect to configure `set
editing-mode vi` if that ever arrives. Worth deciding as one thing rather
than three.

Sixth, what type does the logger take? `WithLogger` takes an `io.Writer` and
writes a line per exchange, escaped so a person can read it. That is the
shape isocline's own recording had and it is enough to replay a session, but
it is not what a program embedding this would want: a Go program has
`log/slog`, and a writer cannot carry a level, a field or a handler. Changing
it is Ken's, and it is why the option was named for a logger rather than for
a log.

Seventh, how does a logger stop recording? The section above has a password
reaching the session log on every system without a no-echo terminal. The fix
is not in the password code, which is already careful; it is that nothing can
ask `sessionLog` to stop for a moment. Whatever answers the sixth question
should answer this one, because a handler with levels and fields has somewhere
to put "not this" and an `io.Writer` does not.

`WithContinue` belongs to the same question, from the other side. Its
parameter is the only one that does not name the field it sets, because the
field is `isIncomplete` and the option is `WithContinue`, and the two are
opposites: the function answers true when the line is *not* finished. Both
names are held for the review of how a caller accumulates lines, where the
whole callback is likely to change shape anyway.

## License

isocline is MIT licensed, and the copyright belongs to Daan Leijen, 2021. Carry
that notice into the Go source files that derive from the C source.
