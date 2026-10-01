# The port against the C

This document compares rline with isocline, the C library it is a port of. It
records the order the C modules were ported in, how the port is checked
against the C, each place where the port does something different from the
C, and the faults that the port found in the C.

The decisions that these sections carry out are in [PLAN.md](PLAN.md). The
sections came from the old `PLAN.md` in the root, unchanged, when the
documents moved to `docs/` (D36).

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

    `_examples/sql/` is the smallest program that uses the package, and
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

## Known departures from the C code

The port keeps the behavior of the C code, even where that behavior is wrong,
because recorded output from the C build is the test corpus. Each departure
carries a comment where the code makes it.

One departure is over a behaviour rather than a fault, and is the only one.
`ReadLine` answers `ErrInterrupted` when the user presses Ctrl-C or Ctrl-G,
where the C clears the line and hands back an empty string that no caller can
tell from Enter on an empty line. usql cannot work without that distinction.
Ken decided for the program over the C. See "What rline offers usql" in USQL.md.

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
