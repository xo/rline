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
10. `highlight.c`. Done. `highlight.go` holds the environment a highlighter
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

    The redraw is done too, in `editline.go`, together with the environment
    type that carries everything the editor needs besides the line itself.
    `tools/build-probe-refresh.sh` builds the eleventh probe, and
    `testdata/refresh.txt` records 1584 redraws across ten lines, two prompts,
    a hint or none, three kinds of content shown below the line, and both
    settings of the indent and brace matching.

    The key dispatch and the main loop are done as well, in
    `editlineloop.go`, together with the hint, the resize and the reading of
    one line from start to finish.

    `editline_help.c`, `editline_history.c` and `editline_completion.c`,
    which are textual includes of `editline.c` rather than separate units,
    are done as well. `editlinehistory.go` holds walking through the history
    and the incremental search that Ctrl-R opens, which draws its own prompt
    below the line and reads its own keys. `editlinecompletion.go` holds
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
    reader and cannot say which terminal it is on. `api.go` puts that state in
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

`feed_test.go` covers it. It feeds a string of keystrokes to the editor and
asserts on the line and the cursor that come out, which is the shape
python-prompt-toolkit uses in `tests/test_cli.py`. It is the only comparable
project with a test suite worth copying: GNU readline ships example programs
rather than tests, and linenoise has none. jline3 has a large one, and its
list of what it tests is where `behaviour_test.go` comes from: how a line
ends, input that is not UTF-8, characters of more than one byte, and a
completion list too long to show.

`behaviour_test.go` also reaches one layer up, calling `editLine` rather than
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

## Known departures from the C code

The port keeps the behavior of the C code, even where that behavior is wrong,
because recorded output from the C build is the test corpus. Each departure
carries a comment where the code makes it.

One departure is over a behaviour rather than a fault, and is the only one.
`ReadLine` answers `ErrInterrupted` when the user presses Ctrl-C or Ctrl-G,
where the C clears the line and hands back an empty string that no caller can
tell from Enter on an empty line. usql cannot work without that distinction.
Ken decided for the program over the C. See "What usql needs" below.

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
keeps the branch in `wrapmark_darwin.go` and `wrapmark_other.go`, because the
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
`ttydev_windows.go` arrange as raw mode is entered and left.

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
`ttydev_windows.go`'s encoder and the escape decoder agreeing under a human's
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
macOS and a left arrow everywhere else. `wrapmark_darwin.go` and
`wrapmark_other.go` name the variant beside the glyph, so the two cannot drift
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

## Tests that pass without checking anything

The rule above is one case of a wider one, which has now cost time seven times
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

Scope the run to the whole package, not to the test being defended. Three
mutations in that same round read as not caught against the two tests the
change had added, and all three were caught by other tests in the package
that nobody had thought of as covering them. A mutation that only the rest
of the suite catches is worth knowing about — it says the new test is
narrower than it looks — but it is not a hole, and reporting it as one sends
someone to write a test that already exists.

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

## What usql needs

usql is the program this port exists for, and it talks to its reader through
one interface, `usql/rline.IO`, with eleven methods. That interface is the
surface to fit, not readline's own. Audited read-only by ken-mba against a
usql checkout, and the two claims that decide anything were checked again
here against a second checkout.

Three methods fit as they are: `Close`, `Interactive` and `Password`, the last
exactly. One behavioural difference under it: usql answers
`ErrPasswordNotAvailable` when built non-interactive, where this reads the
password plainly from the input.

Four want a thin wrapper. `Next` returns runes where `ReadLine` returns a
string. `Prompt(string)` is a whole prompt where `SetPrompt` takes two
markers. `Save(string) error` splits into `AddHistory` and `SaveHistory`.
`Completer` adapted in shape but not in lifetime, because usql sets its
completer after the reader exists and replaces it when the connection changes,
and there was only `WithCompleter` at construction; `SetCompleter` now covers
that.

Four are not here at all. `Stdout()` and `Stderr()`: `*Reader` already
implements `io.Writer`, and an adapter returns the Reader itself from
`Stdout()`, which is both the easy answer and the right one, because the
terminal it writes through is the one that knows where the prompt is. `Write`
says so now. `Stderr()` still has no answer: there is no second stream here,
and whether errors should go through the same terminal or straight to
`os.Stderr` is undecided. `Cygwin()`
may genuinely not be needed, since the Windows console is handled natively
here, but that is a question rather than something to assume away.
`SetOutput(func(string) string)` is the real mismatch: usql passes a filter
over the text about to be drawn, which re-parses its whole accumulated
statement buffer and returns the last line with a colour-continuation prefix.
`WithHighlighter` is handed one line and marks stretches with named styles,
and cannot see outside that line. usql's is stateful across reads by design,
because a SQL statement spans them. Bridging means usql giving up cross-read
highlighting inside the editor, or this accepting a filter over the drawn
string.

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

None of this is a defect. It is all downstream of having ported isocline
faithfully, which is what was asked for, and it is the list of decisions that
turning it into usql's reader needs.

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
