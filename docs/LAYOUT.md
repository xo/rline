# Where code goes

This document holds the rules that decide which file and which package new
code goes in, the list of what is where, and the names of the terminal
types.

The decisions behind these rules are D15, D23 and D30 in
[PLAN.md](PLAN.md). The sections came from the old `PLAN.md` in the root,
unchanged, when the documents moved to `docs/` (D36).

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
