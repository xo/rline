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
`ttydev_bsd.go` and `ttydev_solaris.go`. Nothing else in the terminal layer
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

illumos is the one that earned its console time. It is the only system that
runs `ttydev_solaris.go`, and the only one that takes `ttydev_nosti.go`,
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

The twelfth is `TestTTYDeviceOnARealTerminal`, which needs a pseudo-terminal.
`capture.OpenPTY` is written for Linux and macOS, and FreeBSD opens one
differently again. It lives in `ttydev_pty_test.go` under `linux || darwin`
for that reason, so the eleven that use pipes are not held back by the one
that does not.

Widening the device tests is what found this. The tag on `ttydev_test.go` was
still `linux || darwin` after its source widened, so the layer the widening
enabled had no tests at all on the systems it enabled it for. That is the
same shape as the check that left the build, and it was one line of my own
work old when it was found.

Recording sessions does not reach FreeBSD either. `internal/capture` is
`linux || darwin` and FreeBSD takes the stub, so there is no fourth golden
set and no need for one: the corpus is recorded from the C, and the C build
would have to run there too.

Anything else, including aix, falls to `ttydev_other.go`. It reads plain
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
   length of a slice. What survives is the QUTF-8 codec in `text.go` and the
   ASCII case rules in `text.go`. Neither matches the standard library.
   `tools/build-probe.sh` builds a probe that prints what the C functions
   return, and `testdata/common.txt` records 18576 of those calls.
2. `wcwidth.c`. Not ported. `text.go` calls `github.com/mattn/go-runewidth`
   instead. `testdata/wcwidth.txt` records the width that the C code gives
   every code point, and `testdata/wcwidth-delta.txt` records the 150 ranges
   where go-runewidth answers differently. `TestWidthDelta` keeps that record
   current, so an upgrade of go-runewidth shows up as a change to a committed
   file.
3. `stringbuf.c`. Done. This is a growable buffer that moves the cursor by
   character, not by byte, together with the width, navigation and row and
   column code that the edit loop draws from. The allocator and the growth
   policy are gone, because a Go slice grows on demand. What survives is in
   `text.go`, `text.go`, `text.go`, `text.go`, `text.go` and
   `text.go`. `tools/build-probe-stringbuf.sh` builds a second probe, and
   `testdata/stringbuf.txt` records 76164 of those calls.
4. `tty.c` and `tty_esc.c`. The decoding half is done. `tty_esc.c` is ported
   whole, in `tty.go`. From `tty.c` what is ported is the key codes, now
   the exported `key` package, and the reader in `tty.go`: the two pushback
   buffers, the UTF-8 assembly, the dispatch, and the rewriting of the keys
   that terminals disagree about. `tools/build-probe-tty.sh` builds a third
   probe, and `testdata/tty.txt` records 7157 decodes.

   The terminal itself is done too, in `ttydev_posix.go` with the per system
   requests in `ttydev_linux.go` and `sys_darwin.go`: raw mode through
   `termios`, the UTF-8 test, the resize event, interrupting a read, and
   `tty_read_esc_response`. Windows is still `errUnsupported`.

   That half needs a real terminal, which a corpus cannot give, so
   `TestTTYDeviceOnARealTerminal` opens a pseudo-terminal through
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
   user typed and the file it is kept in, and `editor.go` the stack of saved
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
11. `editline.c`. Started. `editor.go` holds the state of a line being
    edited and every operation that changes it: moving the cursor, the eleven
    kinds of delete, swapping, inserting with the brace that closes itself,
    and the undo and redo stacks. `tools/build-probe-editline.sh` builds the
    tenth probe, and `testdata/editline.txt` records 4177 operations, each one
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
    below the line and reads its own keys. `comp.go` holds
    offering completions and the menu, which draws itself the same way.

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

`testdata/stringbuf-delta.txt` holds the recorded calls whose answer differs
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
`ttydev_other.go` says that it was reading keys.

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
the linter reports, which is why `errUnsupported` lives in `ttydev_other.go`
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

The colour fix above was only half a fix. `writesToTerminal` asked `isATTY`,
which on Windows answers about the standard input whatever descriptor it is
handed, because a console there is reached by handle rather than by
descriptor. So a Windows program with its output redirected still wrote
escape sequences into the file. The Unix `isATTY` does honour its argument,
so the fault was there only on Windows, and only while a console was on the
standard input at the same time to make the wrong answer a plausible one.
`fileIsTerminal` now asks about the file it is given, and `isATTY` keeps the
standard input it was written for. Found by windows-vm, by measuring both
answers with the output redirected rather than by reading the code.

This is the wrap mark again in a different costume: one function standing for
two questions that are the same on one system and not on another, so the
system where they differ is the one that finds out.

And a third instance of the same shape, found by windows-vm while regression
testing the API reshape. `openTTYDevice` on Windows took a descriptor and
ignored it, always opening the standard input, so `WithInput` and
`WithInputFd` silently did nothing there: the caller's stream was accepted and
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
`isATTY` is the one remaining, and its comment now says outright not to give
it a second meaning.

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

A contract that was true by accident in two places. A nil ansi.AttrBuf is
usable and every method accepts one, which is what lets rline pass nil down
ten signatures to mean "record no attributes" instead of branching at each.
Taking each nil guard out in turn showed three of them panicking the suite,
Length, At and the fill behind SetAt and UpdateAt, and four of them changing
nothing. So the words "every method" rested on three methods.
`TestNilAttrBufAcceptsEveryMethod` calls all of them on a nil buffer, and all
seven guards are load-bearing now, measured the same way.

Code that no recording reaches at all, found by looking rather than by a
failure. `ansi.ParseANSI256` reads a palette index out of text, and
`testdata/bbcode.txt` holds no `ansi-color` tag, no `ansi-sgr` and no
`bgcolor=`, so the whole 130,000 line corpus never calls it. The decimal scan
behind it was written out again when the code moved packages, which is the
worst combination: a reimplementation with nothing watching. `names_test.go`
now pins both readers against what the C's sscanf does — leading space, an
optional sign, digits, and whatever follows ignored — and five mutations of
the scan, the hex reader and the range check are all caught.

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
one console window: `isATTY(0)` is false under `go test` and true in the same
binary started directly.

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

Platform-specific code follows four patterns, in this order of preference.

Logic that is specific to one system but makes no system call stays untagged.
It then compiles and runs its tests everywhere, so it cannot rot on the one
machine nobody builds on. Turning Windows key events into escape sequences is
written this way, and it sits inside `tty.go`, which carries no build tag
either, so every system compiles it and every system runs its tests.

Code that orchestrates a platform-specific step stays untagged and calls a
small interface. The tagged files implement the interface. `Password` does
this: the reading, the clearing and the echo back are one untagged function,
and only turning the echo off is per system.

Code shared by a group of systems takes the broad tag for that group.
`ttydev_posix.go` is `unix && !aix`, and holds everything the Unix systems do
the same way.

Values that differ per system take a narrow tag, and the file is named for
what it covers. `ttydev_linux.go`, `ttydev_bsd.go`, `ttydev_solaris.go` and
`sys_darwin.go` hold two ioctl numbers each. A system nobody mapped gets no
constants and falls to the stub in `ttydev_other.go`, which reads plain lines
and says the terminal is unsupported.

Name a file for the axis it splits on rather than for one system on it.
`ttydev_sti.go` and `ttydev_nosti.go` split on whether the system has the
`TIOCSTI` ioctl, which is what actually differs: OpenBSD removed it and
Solaris does not offer it, while five other systems have it.

Gemini recommends a fuzz test in a file of its own, because the corpus and
the helpers crowd out the unit tests. This project puts them together anyway,
in `tty_test.go`, because Ken asked for fewer files and the fuzz test here is
one function over a corpus that lives in `testdata`.

## Where things live

Two packages. `ansi` holds what a terminal understands. `ansi.go` has colors, the
palette, and the reduction that finds the nearest color a terminal can show;
`attr.go` has the attribute that carries a color and reads one out of an SGR
escape sequence; `attrbuf.go` has a run of attributes, one per byte;
`names.go` has the 172 color names and reads a color out of written text. It
depends on nothing outside the standard library, and a caller can use it on
its own.

`rline` is everything else: twenty source files and seventeen test files. The
layout follows
what a reader is looking for rather than what the C file it came from was
called, so several C files land in one Go file and the header of each says
which.

  rline.go       the public interface, and the session log
  prompt.go      reading one line: drawing, the hint, resize, dispatch, help
  editor.go      the line being edited, its operations and the undo stack
  text.go        the buffer, widths, word and line boundaries, rows and columns
  comp.go        completions, completers, file names and the menu
  history.go     the history list, its file, walking and searching
  bbcode.go      markup and the highlighting built on it
  term.go        writing to a terminal
  tty.go         reading keys, and decoding escape sequences into them
  winkey.go      turning Windows key events into sequences: untagged on purpose,
                 so that it is tested on every system rather than only on Windows
  password.go    reading a password with no echo

Per system, one file each where the tags allow it. `sys_darwin.go`,
`sys_nondarwin.go`, `sys_unix.go` and `sys_windows.go` each hold everything
that shares their tag. Four files keep their own tags because no other file
shares them: `ttydev_posix.go` is `linux || darwin`, `ttydev_linux.go` is
`linux`, and `ttydev_other.go` and `termsize_other.go` are the fallbacks.

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

Three checks run on the Go code:

1. `gofmt -l .` names any file that is not formatted.
2. `go vet ./...` reports suspicious code.
3. `go vet` for every system, not only the three. A build is not enough: a
   test file whose tag no longer matches its source still compiles on the
   systems where both are excluded, and only vet on a system where they
   disagree says so. That happened the moment the Unix tag widened, and
   `ttydev_other_test.go` kept the old tag for an hour. The systems worth
   naming are linux, darwin, windows, freebsd, netbsd, openbsd, dragonfly,
   solaris, illumos and plan9. This also checks that the fallback still
   compiles. `ttydev_other.go` exists so
   that such a system builds and reads plain lines, and a function added with
   implementations for only two of the three tag groups breaks it invisibly:
   vet for linux, darwin and windows all pass, and nobody builds the rest.
   That happened at 95ec387, when `fileIsTerminal` was split out of `isATTY`
   with no answer here, and nothing said so for a day.
4. `go tool golangci-lint run ./...` runs the linters that `.golangci.yml`
   names.

`go.mod` pins golangci-lint with a `tool` directive, so `go tool` builds it
with the toolchain of this module. A golangci-lint binary built elsewhere can
fail to read the export data of a newer Go release, and then it reports every
standard library import as an error.

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
