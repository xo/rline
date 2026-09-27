# Plan

rline is a readline package for Go. It is a pure Go port of isocline, a line
editor written in C by Daan Leijen. This document holds the plan: the
purpose, where the port stands and where it goes next. Then it holds every
decision that shapes rline, and at the end the questions that are open for
Ken.

## How to read the decisions

The decisions are in the order they were made. An entry is append only. A
decision is never edited to change its conclusion. When a later decision
changes an earlier one, the earlier entry keeps its text, and both headings
name each other: "Amends D10" in the new one, and "Amended by D21" in the old
one.

Each heading ends with a status. The status is `Decided` when a commit
message or the plan says that Ken decided it, chose it, asked for it or set
it, or when Ken's first commit states it. The status is `Proposed` when the
history does not show that Ken chose it, even when the code has followed it
since. A proposed decision holds until Ken answers it. The open questions at
the end ask about each one.

A bare number such as D3 is a decision in this document. A decision of
another repository names that repository, such as dbmeta D110.

D1 to D35 were written on 2026-09-27 from the history of the repository,
from `39330c6` (2026-09-19) to `5630135` (2026-09-23), and from the code at
`5630135`. Each entry names its commits. The reason comes from a commit
message, a code comment or the old `PLAN.md`. If no text gives a reason, the
entry says "Reason not recorded". Much of the reasoning is long, and it is
kept in the topic documents in this folder. An entry points at the section
that holds it.

| Decision | Title | Status |
| --- | --- | --- |
| [D1](#d1-the-port-is-one-go-package-named-rline-decided-amended-by-d8) | The port is one Go package named rline | Decided. Amended by D8 |
| [D2](#d2-the-port-does-not-use-cgo-at-any-stage-decided) | The port does not use cgo at any stage | Decided |
| [D3](#d3-a-c-module-is-the-unit-of-work-and-the-modules-are-ported-from-the-leaves-up-decided) | A C module is the unit of work, and the modules are ported from the leaves up | Decided |
| [D4](#d4-output-recorded-from-the-c-build-is-the-test-corpus-decided) | Output recorded from the C build is the test corpus | Decided |
| [D5](#d5-each-file-derived-from-the-c-carries-isoclines-mit-notice-decided) | Each file derived from the C carries isocline's MIT notice | Decided |
| [D6](#d6-rline-runs-on-linux-macos-and-windows-and-on-windows-in-cmdexe-and-powershell-proposed) | rline runs on Linux, macOS and Windows, and on Windows in cmd.exe and PowerShell | Proposed |
| [D7](#d7-character-width-comes-from-go-runewidth-and-the-terminal-settings-from-xsysunix-proposed) | Character width comes from go-runewidth, and the terminal settings from x/sys/unix | Proposed |
| [D8](#d8-the-key-codes-are-exported-from-their-own-package-key-proposed-amends-d1) | The key codes are exported from their own package, key | Proposed. Amends D1 |
| [D9](#d9-the-port-writes-escape-sequences-to-the-windows-console-and-carries-none-of-the-console-emulation-in-termc-proposed) | The port writes escape sequences to the Windows console and carries none of the console emulation in term.c | Proposed |
| [D10](#d10-the-history-file-mode-is-left-to-the-umask-decided-amended-by-d21) | The history file mode is left to the umask | Decided. Amended by D21 |
| [D11](#d11-a-program-makes-its-own-session-and-sets-it-up-with-options-proposed) | A program makes its own Session, and sets it up with options | Proposed |
| [D12](#d12-password-turns-echo-off-and-leaves-line-editing-to-the-terminal-decided) | Password turns echo off and leaves line editing to the terminal | Decided |
| [D13](#d13-ctrl-c-answers-errinterrupted-and-setcompleter-replaces-the-completer-decided) | Ctrl-C answers ErrInterrupted, and SetCompleter replaces the completer | Decided |
| [D14](#d14-a-session-reads-lines-a-markupwriter-writes-markup-and-a-prompt-holds-both-decided) | A Session reads lines, a MarkupWriter writes markup, and a Prompt holds both | Decided |
| [D15](#d15-files-are-grouped-by-what-a-reader-looks-for-and-there-are-fewer-of-them-decided) | Files are grouped by what a reader looks for, and there are fewer of them | Decided |
| [D16](#d16-rline-never-adapts-to-usql-decided) | rline never adapts to usql | Decided |
| [D17](#d17-an-error-is-a-constant-of-a-string-type-and-its-text-is-its-name-without-err-decided) | An error is a constant of a string type, and its text is its name without Err | Decided |
| [D18](#d18-the-terminal-code-builds-for-every-unix-system-but-aix-and-three-systems-are-tested-proposed) | The terminal code builds for every Unix system but aix, and three systems are tested | Proposed |
| [D19](#d19-the-library-needs-go-1250-and-the-linter-is-a-module-of-its-own-proposed) | The library needs Go 1.25.0, and the linter is a module of its own | Proposed |
| [D20](#d20-continuous-integration-runs-every-check-on-four-hosted-runners-with-the-go-that-gomod-names-proposed) | Continuous integration runs every check on four hosted runners, with the Go that go.mod names | Proposed |
| [D21](#d21-saving-the-history-never-makes-the-file-shorter-and-the-file-is-created-0600-proposed-amends-d10) | Saving the history never makes the file shorter, and the file is created 0600 | Proposed. Amends D10 |
| [D22](#d22-the-completion-file-is-named-compgo-decided) | The completion file is named comp.go | Decided |
| [D23](#d23-color-is-the-ansi-package-and-the-line-editor-and-the-text-under-it-are-internal-packages-proposed) | Color is the ansi package, and the line editor and the text under it are internal packages | Proposed |
| [D24](#d24-the-recorded-color-and-sgr-cases-of-10b07d6-are-discarded-decided) | The recorded color and SGR cases of 10b07d6 are discarded | Decided |
| [D25](#d25-ansicodergba-keeps-its-named-results-decided) | ansi.Code.RGBA keeps its named results | Decided |
| [D26](#d26-every-switch-is-named-for-what-it-turns-on-proposed) | Every switch is named for what it turns on | Proposed |
| [D27](#d27-each-options-parameter-names-the-field-it-sets-decided) | Each option's parameter names the field it sets | Decided |
| [D28](#d28-the-stream-options-are-withstdin-withstdout-and-withstderr-proposed) | The stream options are WithStdin, WithStdout and WithStderr | Proposed |
| [D29](#d29-a-deferred-close-of-a-file-opened-for-reading-is-not-checked-proposed) | A deferred close of a file opened for reading is not checked | Proposed |
| [D30](#d30-the-terminal-types-are-named-tty-keydecoder-and-term-proposed) | The terminal types are named tty, keyDecoder and term | Proposed |
| [D31](#d31-the-checks-cover-every-system-that-the-go-toolchain-lists-proposed) | The checks cover every system that the Go toolchain lists | Proposed |
| [D32](#d32-four-gaps-in-the-checks-are-left-on-purpose-proposed) | Four gaps in the checks are left on purpose | Proposed |
| [D33](#d33-real-terminals-are-tested-on-a-private-compositor-never-on-the-desktop-proposed) | Real terminals are tested on a private compositor, never on the desktop | Proposed |
| [D34](#d34-the-test-suite-is-designed-as-seven-layers-proposed) | The test suite is designed as seven layers | Proposed |
| [D35](#d35-usqls-readline-label-is-the-queue-that-rline-plans-against-proposed) | usql's readline label is the queue that rline plans against | Proposed |
| [D36](#d36-rline-is-set-up-for-coding-agents-as-every-xo-repository-is-decided) | rline is set up for coding agents as every xo repository is | Decided |

## Purpose

rline reads a line of text from a terminal, and gives the person who types
it editing, history, completion and colored output. It is written in Go,
with no cgo, and it links against no C library.

usql is the program that rline exists for. usql today uses a readline
package that hands it one line at a time and leaves the terminal to it.
rline is meant to replace it, and D16 says that rline does not bend to fit
usql's current code. usql moves to rline once rline does the work well.

isocline is the reference for behavior, and `isocline/` holds its C source.
That folder is a separate git repository. This repository ignores it and
never writes inside it.

## Where it stands

Every module of isocline is ported or replaced on purpose. Nothing of
`isocline.c` is translated, because its global state became a `Session` that
a program makes (D11). [PORT.md](PORT.md) gives the state of each module,
and each place where the port does something different from the C.

The port is checked against the C. A probe program links the C code and
records what it answers for a fixed set of inputs, and the Go code must give
the same answers. About 130,000 recorded calls are in the corpora. Sessions
recorded from the C demo under a pseudo-terminal test the whole program on
Linux and macOS. [TESTING.md](TESTING.md) holds the design of the test
suite, and [LESSONS.md](LESSONS.md) holds what the tests taught.

Linux, macOS and Windows are tested, and a person drove the editor by hand
on each. FreeBSD, NetBSD and OmniOS pass the suite. OpenBSD, Dragonfly and
Solaris compile and are not tested. [PLATFORMS.md](PLATFORMS.md) gives the detail and the checks.

The API is shaped for Go and not translated from C. `New` returns a
`Prompt`, which holds a `Session` that reads lines and a `MarkupWriter` that
writes styled output (D14). `ReadLine` answers `ErrInterrupted` for Ctrl-C,
which is the one place where the behavior departs from the C (D13).

## Direction

The next work comes from usql. usql labels its line editor issues
`readline`, and fourteen are open on 2026-09-27. usql fixes what it can on
its side, and this repository plans for the rest (D35). [USQL.md](USQL.md)
sorts them by what each one needs from rline.

Three of them need something that rline does not offer yet:

1. A way for a caller to say that it left the cursor in the middle of a row
   on purpose, so that the next prompt is drawn after that text (usql 215).
2. A way to give the terminal to a child process, such as a pager, and to
   take it back afterwards (usql 508).
3. A way to ask a `Session` which completer and which highlighter it holds
   (usql 478).

Two faults of rline's own are known. A focus notification from the terminal
is decoded as a key press. On a system with no no-echo terminal mode, a
password reaches the session log. [BACKLOG.md](BACKLOG.md) lists these and
every other piece of known work. The open questions below hold the choices
that Ken has to make before some of that work can start.

## Decisions

### D1. The port is one Go package named rline. Decided. Amended by D8.

Commits: `39330c6` (2026-09-19).

Ken's first commit states this as a settled decision:

The port is one Go package named `rline`. Each C module becomes one or more
files in that package. The C headers contain a cycle, because `attr.c` includes
`term.h` and `term.h` includes `attr.h`. One Go package removes that cycle.

Reason: the cycle in the C headers.

### D2. The port does not use cgo at any stage. Decided.

Commits: `39330c6` (2026-09-19).

Ken's first commit states this as a settled decision:

The port does not use cgo at any stage. cgo is the Go facility that calls C
code. An earlier plan built a cgo binding layer first, to get a reference to
compare against. We dropped that step for two reasons. The binding code gets
discarded at the end. The completion and highlight callbacks cost real work to
pass across the boundary.

### D3. A C module is the unit of work, and the modules are ported from the leaves up. Decided.

Commits: `39330c6` (2026-09-19).

A module is the unit of work, not a function. isocline passes its own allocator
into most structures, as the type `alloc_t`. A module keeps that ownership in
one place, so a module ports cleanly and a single function does not.

Ken's first commit also sets the order: "The C headers give an acyclic
order. Port the modules from the leaves up." The order and the state of each
module are in "Port order" in [PORT.md](PORT.md).

### D4. Output recorded from the C build is the test corpus. Decided.

Commits: `39330c6` (2026-09-19), `9aeab74` (2026-09-19), `2418caf`
(2026-09-19).

Ken's first commit sets the test method. The C code has no unit tests, so
the corpus comes from what the C code does: record its output, give the Go
port the same input, and compare the bytes. The same commit asks for fuzz
tests that compare the C and the Go on the same random input. `2418caf`
added that fuzz test for the escape decoder.

What was built differs in one way from the first plan. Most corpora come
from probe programs that call the C functions directly, rather than from
the demo alone. The demo still records whole sessions.

It follows that the port keeps the behavior of the C code even where that
behavior is wrong, because a fix fails the comparison. Each departure
carries a comment where the code makes it. "Test method" and "Known
departures from the C code" in [PORT.md](PORT.md) hold the method and the
list.

### D5. Each file derived from the C carries isocline's MIT notice. Decided.

Commits: `39330c6` (2026-09-19).

Ken's first commit states it:

isocline is MIT licensed, and the copyright belongs to Daan Leijen, 2021. Carry
that notice into the Go source files that derive from the C source.

What the code does is narrower. The package comments of `rline` and of `key`
carry the notice and say that it covers the package. `ansi`,
`internal/text` and `internal/editor` also derive from the C and carry no
notice, and the repository has no `LICENSE` file. [BACKLOG.md](BACKLOG.md)
holds this.

### D6. rline runs on Linux, macOS and Windows, and on Windows in cmd.exe and PowerShell. Proposed.

Commits: `9aeab74` (2026-09-19), `231a976` (2026-09-19), `591b9d9`
(2026-09-19), `202866f` (2026-09-20).

`9aeab74` wrote the requirement into the plan: "rline must run on Linux,
macOS and Windows. On Windows it must work in both `cmd.exe` and
PowerShell." Ken then drove the editor by hand in `cmd.exe`, and the
sessions on a Mac and on a Windows machine ran the suite. No commit says
that Ken set the requirement, so it is proposed. "Which systems are
supported" and "Platforms" in [PLATFORMS.md](PLATFORMS.md) hold the detail.

Reason not recorded.

### D7. Character width comes from go-runewidth, and the terminal settings from x/sys/unix. Proposed.

Commits: `51a1ffa` (2026-09-19), `52a9718` (2026-09-19), `b885bf8`
(2026-09-19).

The first plan asked Ken which package measures character width, as its
third open question. `52a9718` chose go-runewidth and did not port
`wcwidth.c`. The history does not record that Ken answered the question.
The old plan gave both dependencies in one paragraph:

The port takes two dependencies. `github.com/mattn/go-runewidth` measures
character width, in place of the table in `wcwidth.c`. `golang.org/x/sys/unix`
reads and writes the terminal settings, because the standard `syscall` package
does not name `TCSETSF` on Linux, so `termios` cannot be done there with the
standard library alone.

The two width tables disagree for 3877 code points. `TestWidthDelta` holds
the disagreement in a committed file, so an upgrade of go-runewidth shows as
a changed file. Three parts of the disagreement are not settled. "The width
table" in [PORT.md](PORT.md) names them, and they are an open question below.

### D8. The key codes are exported from their own package, key. Proposed. Amends D1.

Commits: `b56d755` (2026-09-19).

The one exception is `key`, which holds the key codes from `tty.h`. A program
that binds keys or reads them names those codes, so they are exported from
their own package and read as `key.Up`, `key.CtrlA` and `key.F(5)`. `key`
imports nothing from `rline`, so it adds no cycle. Everything else stays
unexported in `rline` until `isocline.c` is ported and the public API is
decided.

No commit says that Ken chose it.

### D9. The port writes escape sequences to the Windows console and carries none of the console emulation in term.c. Proposed.

Commits: `fd1c1e3` (2026-09-19), `b76e82a` (2026-09-19), `cc5a887`
(2026-09-19).

About 400 lines of `term.c` turn escape sequences into console calls, for a
console that cannot read escape sequences. The port writes escape sequences
on every system, and asks the console to read them when it enters raw mode.
`fd1c1e3` says that this was "a decision that had been arrived at rather
than taken". It drops consoles older than Windows 10, which the C still
supports, and the README now says that rline needs Windows 10 or later.
"Windows" in [PLATFORMS.md](PLATFORMS.md) holds the measurements.

Reason, from the old plan: two calls to `SetConsoleMode` replace the 400
lines, and a Windows 11 console measured with the flag off moved the cursor
correctly once the flag was on.

### D10. The history file mode is left to the umask. Decided. Amended by D21.

Commits: `231ea07` (2026-09-19).

`231ea07` removed the owner only mode that the C code sets on the history
file. Its message says: "Ken's call: the history is going to be rewritten,
so the general operation goes in now and the file mode gets figured out
then, rather than costing more time in a port." The file was then created
as `fopen` creates it, 0666 with the umask applied.

The finding stays whatever the mode is. The file holds every line the user
typed, which can include a password typed at the wrong prompt. On Windows the
protection comes from the access control list of the folder, and nothing in
this package sets it.

### D11. A program makes its own Session, and sets it up with options. Proposed.

Commits: `73aaf21` (2026-09-19), `ea31d9d` (2026-09-19).

isocline keeps one environment in a process global, and every public
function reaches for it, so a program can have only one line reader and
cannot say which terminal it is on. `73aaf21` put that state in a value the
caller makes, with the settings passed as options when it is made rather
than switched afterwards. `ReadLine` answers `io.EOF` where the C answers a
null pointer, and reads a plain line when there is no terminal to edit on.
`ea31d9d` reshaped the API along a review by Gemini: "idiomatic Go over
isocline's shape". Item 12 of "Port order" in [PORT.md](PORT.md) holds the
detail.

Reason, from `73aaf21`: one global state means one reader per program, on a
terminal the program cannot choose.

### D12. Password turns echo off and leaves line editing to the terminal. Decided.

Commits: `1800cfb` (2026-09-19), `6158f9a` (2026-09-19).

isocline has no password read. `1800cfb` added `Password`, because usql
needs one and it cannot be built from outside the terminal layer. It first
read in raw mode. Ken noticed that the text was hidden but Ghostty did not
show its lock. `6158f9a` changed it to turn echo off and leave canonical
mode on, as `getpass` and `golang.org/x/term` do, after "the reference Ken
pointed at". A terminal guesses that a program reads a password when echo is
off and canonical mode is on.

The cost is that the terminal driver edits the line, and Ctrl-C raises an
interrupt as it does at every other password prompt. Where a terminal has no
such mode, `Password` reads in raw mode, and that path writes the password
into the session log. "A password reaches the session log on the systems
without no-echo" in [USQL.md](USQL.md) holds that fault.

### D13. Ctrl-C answers ErrInterrupted, and SetCompleter replaces the completer. Decided.

Commits: `cfa18e7` (2026-09-19), `0af184b` (2026-09-19), `dc94e67`
(2026-09-20).

`0af184b` opens: "Two of the things usql needs, decided by Ken." `ReadLine`
answers `ErrInterrupted` for Ctrl-C and for Ctrl-G. The C clears the line
and hands back an empty string, which a caller cannot tell from Enter on an
empty line, and `editline.c:949` says so on purpose. Ctrl-D on an empty line
still answers `io.EOF`, and the two answers are kept apart: one says the
line was given up, the other that the input ended. `SetCompleter` replaces
the completer after the `Session` exists, because usql replaces it when a
connection opens.

This is the one place where the port departs from the C over a behavior
rather than a fault. "Ctrl-C now says so" in [USQL.md](USQL.md) holds the
detail.

### D14. A Session reads lines, a MarkupWriter writes markup, and a Prompt holds both. Decided.

Commits: `ed2fec9` (2026-09-19), `46b38c2` (2026-09-20), `bc51fd1`
(2026-09-20).

`ed2fec9` opens: "Ken asked for three types where there was one." One type
reads lines and writes plain text, one writes markup, and a `Prompt` embeds
the first and holds the second, so the reading methods are promoted. `New`
returns a `*Prompt`. `Prompt.Markup` never answers nil: a writer with
nowhere to write discards what it is given, so a caller never tests for
nil first.

Gemini asked twice to fold the `Session` into the `Prompt`. `bc51fd1` kept
the split, because "Ken chose the split explicitly". The first reader type
was named `Reader`, and `46b38c2` renamed it `Session`, because it had
`Write` and not `Read`. `bc51fd1` renamed `Writer` to `MarkupWriter`.

### D15. Files are grouped by what a reader looks for, and there are fewer of them. Decided.

Commits: `3de7496` (2026-09-19), `83f2d96` (2026-09-20), `cb29b19`
(2026-09-20).

`3de7496` opens: "Ken asked for fewer files and for Gemini's plan on how."
It took 79 files to 37. Several C files land in one Go file, and the header
of each file says which C files it came from. `83f2d96` made further moves
that Ken asked for, and wrote down the rule that Gemini gave for where code
goes. The fuzz test sits beside the unit tests in `decoder_test.go` against
Gemini's advice, "because Ken asked for fewer files". "How files are
organized" and "Where things live" in [LAYOUT.md](LAYOUT.md) hold the rule
and the list.

### D16. rline never adapts to usql. Decided.

Commits: `bfb70c8` (2026-09-20).

`bfb70c8` opens: "Ken set the direction: usql is meant to be a blind
consumer of a package that knows better, and it will adapt here once this
works." Where usql's current reader does something poorly, rline does it
well and usql comes to it. rline does not grow a second way to match what
usql has. So each method of usql's reader interface is read as something
usql has to do, not as a shape to fit. "What rline offers usql" in
[USQL.md](USQL.md) applies it.

### D17. An error is a constant of a string type, and its text is its name without Err. Decided.

Commits: `0d30caf` (2026-09-20), `8d60e21` (2026-09-20), `1747e62`
(2026-09-20).

`0d30caf` opens: "Ken prefers immutable error constants". `8d60e21` opens:
"Ken's rule, applied to all six". The old plan held the rule under "Error
values are constants":

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

### D18. The terminal code builds for every Unix system but aix, and three systems are tested. Proposed.

Commits: `83f2d96` (2026-09-20), `1763b1c` (2026-09-20), `850107e`
(2026-09-20).

Ken asked whether "posix" is a synonym for "unix" here. `83f2d96` measured
it and widened the shared file to `unix && !aix`, with two small files for
the ioctl numbers of each family, and seven more systems compile. Ken then
ran FreeBSD, NetBSD and illumos as virtual machines, and the suite passes on
each. The plan says that compiling is not support, and it names which
systems are tested and which only compile. No commit says that Ken chose to
widen the tag. "Which systems are supported" in
[PLATFORMS.md](PLATFORMS.md) holds the measurements and DeepSeek's argument
against the change.

### D19. The library needs Go 1.25.0, and the linter is a module of its own. Proposed.

Commits: `1763b1c` (2026-09-20).

The library asked for Go 1.27 only because golangci-lint did, and no
consumer builds the linter. go-runewidth asks for 1.23 and x/sys for 1.25.0.
`1763b1c` moved the linter to its own module under `tools/lint`, and
`tools/lint.sh` builds it and runs it from the root.

Reason, from `1763b1c`: FreeBSD ships Go 1.25, and the tree could not build
there before.

### D20. Continuous integration runs every check on four hosted runners, with the Go that go.mod names. Proposed.

Commits: `9cef5e4` (2026-09-20), `b9ab8ba` (2026-09-20).

The runners are Linux on amd64 and arm64, Windows on amd64 and macOS on
arm64. The job installs the Go that `go.mod` asks for, not the newest. On
Windows it skips `TestGoldenSetIsPresent` alone, not its package.
"Continuous integration" in [PLATFORMS.md](PLATFORMS.md) holds the detail.

Reason, from `9cef5e4`: "a library that builds only with the newest Go is
one its consumers cannot use".

### D21. Saving the history never makes the file shorter, and the file is created 0600. Proposed. Amends D10.

Commits: `e19767f` (2026-09-20).

Ken asked whether saving truncates the file and writes over it. It did: a
line that cannot be read stopped the load, and the next save wrote the
shorter list over the whole file. `e19767f` made two changes that answer
that. Saving is refused when the file was not read in full. Saving writes a
temporary file beside the history file and renames it over the old one.

The same commit also set the mode: `DefaultHistoryFileMode` is 0600, and
`WithHistoryFileMode` changes it. That reverses D10, and no commit says that
Ken chose to reverse it. The comment above `history.save` in `history.go`
and the departure in "Known departures from the C code" in
[PORT.md](PORT.md) still describe D10. The status of the mode is an open
question below.

### D22. The completion file is named comp.go. Decided.

Commits: `a474a27` (2026-09-20).

`complete.go` can be read as the finished part rather than the part that
completes. Gemini and DeepSeek both preferred `completion.go`. `a474a27`
says: "the name is Ken's call and this is the name he asked for."

### D23. Color is the ansi package, and the line editor and the text under it are internal packages. Proposed.

Commits: `0233416` (2026-09-20), `ec5f9c3` (2026-09-20), `d91288c`
(2026-09-20).

A color, and the reduction of a color to what a terminal can show, needs
nothing else from rline, so it is the public package `ansi`. The line being
edited is `internal/editor`, and the bytes under it are `internal/text`.
Both are internal on purpose. The editor exports 42 members, 14 of them
fields that other code writes directly. Under a public path they become an
API frozen by the first tagged release. "Where things live" in
[LAYOUT.md](LAYOUT.md) holds the reasoning.

Reason, from `d91288c`: a type that must export 42 members is a struct that
was moved, not a boundary that was drawn. If the fields become methods and
the count falls under about fifteen, the package can be made public.

### D24. The recorded color and SGR cases of 10b07d6 are discarded. Decided.

Commits: `10b07d6` (2026-09-20), `779fca4` (2026-09-20).

`779fca4` reverts `10b07d6` "on Ken's instruction to discard it". Its
message says the work was good, and that the instruction which produced it
reached the wrong session, so it is "a decision about where work is done
rather than about the work". One finding from it stays: the tag parser
changes a value before the decimal scan in `ansi` sees it, so a recording
cannot replace `names_test.go` for that half.

### D25. ansi.Code.RGBA keeps its named results. Decided.

Commits: `4311130` (2026-09-20).

`4311130` removed two named results that did no work. `ansi.Code.RGBA` keeps
`(r, g, b, a uint32)` "on Ken's call", because `image/color.Color` declares
the method with those names, and four bare `uint32` values do not say which
is which. The reason is written above the method.

### D26. Every switch is named for what it turns on. Proposed.

Commits: `ea31d9d` (2026-09-19), `64d0004` (2026-09-20).

`ea31d9d` made the options positive, such as `WithColor(bool)` in place of a
name for turning color off, after a review by Gemini. `64d0004` did the same
for the fields and for three switches that are not options, and `New` sets
them all true in one block. It removed `WithHighlighting`, because a nil
highlighter already says that. A caller turns highlighting off with
`WithHighlighter(nil)`. "Tests that pass without checking anything" in
[LESSONS.md](LESSONS.md) records what the change broke in the tests.

Reason, from `64d0004`: a negative name makes every read a double negative,
and its zero value hides a default.

### D27. Each option's parameter names the field it sets. Decided.

Commits: `64834d1` (2026-09-20).

`64834d1` says this "was Ken's own example: `WithColor(color bool)` setting
`c.color = color`". Five fields took the names of their options to allow it.
Two parameters keep other names, and the commit says why. `WithHistoryFile`
takes `name`, as `os.Open` does. `WithContinue` takes `fn`, because its field
is `isIncomplete` and the two mean opposite things. The name of
`WithContinue` is an open question below.

### D28. The stream options are WithStdin, WithStdout and WithStderr. Proposed.

Commits: `64834d1` (2026-09-20).

The options were `WithInput`, `WithOutput` and `WithStderr`, which match no
convention. `os/exec.Cmd`, `x/crypto/ssh.Session` and
`chzyer/readline.Config` use `Stdin`, `Stdout` and `Stderr` for any reader
or writer. Neither name promises a real standard stream or a file
descriptor. `WithStdin` looks for a descriptor, because editing needs a
terminal.

Reason, from `64834d1`: the third option was already `Stderr`, and nothing
was going to rename it.

### D29. A deferred close of a file opened for reading is not checked. Proposed.

Commits: `72a77ac` (2026-09-20).

`.golangci.yml` excludes `(*os.File).Close` from errcheck, and the reason is
written beside it. The read has already succeeded or failed, and the close
cannot change either. errcheck cannot limit the exclusion to deferred calls,
so an unchecked close of a file opened for writing passes too. `history.go`
checks its own write close for that reason.

### D30. The terminal types are named tty, keyDecoder and term. Proposed.

Commits: `d08ea9a` (2026-09-21).

The type that held the file descriptor and the terminal settings is `tty`.
The type that decodes escape sequences into keys, and runs with no device at
all, is `keyDecoder`. `term` writes to the terminal. The files follow the
types. "The terminal types are named for what they are" in
[LAYOUT.md](LAYOUT.md) holds the detail.

### D31. The checks cover every system that the Go toolchain lists. Proposed.

Commits: `83f2d96` (2026-09-20), `b89a07f` (2026-09-23), `4121b01`
(2026-09-23).

`go vet` runs for every pair that `go tool dist list` gives, 47 pairs over
15 systems, and the loop reports a pair it cannot ask rather than skipping
it. The linter runs for linux, darwin and windows. "Checks", "Verifying with
a list" and "The linter only ever looked at this machine" in
[PLATFORMS.md](PLATFORMS.md) hold the reasons. The models asked about the
loop think it is too much, and [TESTING.md](TESTING.md) says that the choice
is Ken's. It is an open question below.

Reason, from `b89a07f`: a hand-written list missed five systems, and a loop
cannot report on what it was not given.

### D32. Four gaps in the checks are left on purpose. Proposed.

Commits: `b89a07f` (2026-09-23).

The flush on entering raw mode is tested on two systems of the seven that
compile it. `capture.OpenPTY` is not widened to all of Unix, because nobody
can test it there. `android/arm` is not vetted, because it needs an NDK.
The results of the matrix are not pooled. "Gaps left on purpose" in
[PLATFORMS.md](PLATFORMS.md) holds each one. The old plan says that
"somebody weighed them and said no", and names the session `ken-mba` as the
source of the section. It does not say that Ken weighed them.

### D33. Real terminals are tested on a private compositor, never on the desktop. Proposed.

Commits: `3e77b8b` (2026-09-23), `e8a1d71` (2026-09-23).

`uitest` opens real terminal emulators on a headless compositor of its own,
with its own message bus, and types into them with that compositor's virtual
keyboard. The first version typed test input into Ken's own windows twice.
Only the byte log is compared, and the screenshots are for a person. "Tests
against a real terminal" in [LESSONS.md](LESSONS.md) and
[uitest/README.md](../uitest/README.md) hold the detail.

Reason, from `3e77b8b`: a check before a synthetic keystroke and the
keystroke are different moments, so only isolation makes it safe.

### D34. The test suite is designed as seven layers. Proposed.

Commits: `fbe0a8d` (2026-09-23).

The old plan summed it up under "The shape of the test suite":

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

### D35. usql's readline label is the queue that rline plans against. Proposed.

Commits: `1ee5929` (2026-09-23), `5630135` (2026-09-23).

usql fixes the issues it can on its side, and this repository finds out for
each one whether rline has to offer something first. `5630135` calls this
"the arrangement" and does not say who made it. It says that Ken kept usql
215 as a numbered backlog item. "What usql needs, and who moves first" in
[USQL.md](USQL.md) sorts the issues.

### D36. rline is set up for coding agents as every xo repository is. Decided.

Ken decided on 2026-09-27 that every repository in the `xo` namespace is set
up for coding agents the same way. dbmeta records the standard as dbmeta
D110, and records as dbmeta D111 that a small library keeps its decisions in
`docs/PLAN.md`. rline adopts both.

What changed here:

- `AGENTS.md` is new and opens with the three standing rules. `CLAUDE.md`
  holds one line, `@AGENTS.md`.
- `.claude/skills` held the two skills as symbolic links, from `28149af`. A
  Windows checkout writes a link as a small text file, and Claude Code then
  loads no skill. They are ordinary folders now, the same as those in
  `.agents/skills`, installed with `--copy`.
- `CONTRIBUTING.md` is new. `.gitignore` ignores
  `.claude/settings.local.json`. `.gitattributes` already held
  `* text=auto eol=lf`.
- `PLAN.md` and `TESTING.md` moved to `docs/`. The old `PLAN.md` was 3,237
  lines of plan, decisions and findings. This file keeps the plan, the
  decisions and the open questions. Its other sections moved unchanged to
  [PORT.md](PORT.md), [PLATFORMS.md](PLATFORMS.md), [LESSONS.md](LESSONS.md),
  [LAYOUT.md](LAYOUT.md) and [USQL.md](USQL.md).
- `docs/BACKLOG.md` is new.
- `agents_test.go` holds the rules: `TestSkillsAreCopies`,
  `TestClaudeImportsAgents`, `TestTheRootHoldsFourDocuments`,
  `TestTheDecisionIndexIsComplete` and `TestAnAmendmentPointsBothWays`.
  Two more hold the references that the move could break:
  `TestEveryDecisionReferenceExists` and
  `TestEveryDocumentReferenceResolves`.

## Open questions for Ken

The first eight came from the old plan, in its words. The rest were found
while this document was written.

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

Seventh, how does a logger stop recording? [USQL.md](USQL.md) has a password
reaching the session log on every system without a no-echo terminal. The fix
is not in the password code, which is already careful; it is that nothing can
ask `sessionLog` to stop for a moment. Whatever answers the sixth question
should answer this one, because a handler with levels and fields has somewhere
to put "not this" and an `io.Writer` does not.

Eighth, can a caller ask what is attached? `SetCompleter` and
`SetHighlighter` are write-only, so a program cannot tell "no completer" from
"a completer that found nothing", and neither can a test. Inside, the two are
already distinct — `completer == nil` short-circuits before anything is
generated — so this is about what the API exposes rather than about what the
port knows.

It is not hypothetical. usql issue 478 was exactly this: three lines that
reinstall the completer after connecting were dropped, completion silently
returned nothing for about ten months across three databases and two
platforms, and no suite noticed because an empty candidate list is what "there
is nothing to complete here" also looks like. See TESTING.md.

`WithContinue` belongs to the same question, from the other side. Its
parameter is the only one that does not name the field it sets, because the
field is `isIncomplete` and the option is `WithContinue`, and the two are
opposites: the function answers true when the line is *not* finished. Both
names are held for the review of how a caller accumulates lines, where the
whole callback is likely to change shape anyway.

Ninth, which proposed decisions stand? Each decision marked `Proposed` above
was made without a record that Ken chose it: D6, D7, D8, D9, D11, D18, D19,
D20, D21, D23, D26, D28, D29, D30, D31, D32, D33, D34 and D35. Each one that
Ken confirms changes to `Decided`. Each one that Ken rejects gets a new
decision that amends it.

Tenth, is the history file created 0600, or left to the umask? D10 is Ken's
call to leave the mode to the umask until the history is rewritten. D21 set
0600 a day later, and the history does not say that Ken changed the call.
The code does D21. The comment above `history.save` and the departure in
[PORT.md](PORT.md) say D10. One of them is wrong, and the answer decides
which.

Eleventh, how does rline measure the three kinds of character on which
go-runewidth and the C table disagree? These are the Hangul Jamo vowels and
final consonants, the tag characters inside a flag emoji, and the UTF-16
surrogate halves. "The width table" in [PORT.md](PORT.md) says that none is
settled. The surrogate halves cannot reach the editor, so two of them
matter.

Twelfth, does rline support Windows consoles older than Windows 10? D9 drops
them, because they cannot read escape sequences. Keeping them means porting
the 400 lines of console emulation in `term.c`.

Thirteenth, does the cross-vet loop keep all 47 pairs on every push? Gemini
and DeepSeek think it is too much. The loop caught two real faults that a
check of three systems did not. [TESTING.md](TESTING.md) records both sides
and says that the choice is Ken's.
