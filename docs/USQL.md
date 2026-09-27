# What usql needs from rline

usql is the program that rline exists for. This document says how the API of
rline answers each thing that usql does with its current line reader, which
open usql issues need something from rline, and three faults that reading
those issues found.

D16 in [PLAN.md](PLAN.md) sets the direction: rline never adapts to usql. The
sections came from the old `PLAN.md` in the root, unchanged, when the
documents moved to `docs/` (D36).

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
rather than a fault, and it is listed under the departures in PORT.md as well.

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

## What usql needs, and who moves first

usql carries a `readline` label, and what wears it is the queue this package
plans against:

    https://github.com/xo/usql/issues?q=is%3Aissue+is%3Aopen+label%3Areadline

The arrangement is that usql attacks them as they come in and this package
plans for them. So the job here is to know, for each one, whether rline has to
offer something before usql can do anything at all — and to have measured that
rather than assumed it. Thirteen are open today, and they sort into six kinds.

### rline must offer something new, or the issue cannot be fixed at all

**215, drawing a prompt after output that was left mid-line.** `\echo -n`
suppresses its newline correctly and the editor then erases the row before
drawing the next prompt. The redraw opens with `term.startOfLine()`, an
unconditional carriage return, so whatever a caller deliberately left on that
row is written over. Nothing in the API expresses "the cursor is where I meant
to leave it". This is the one Ken kept as a numbered backlog item, and it is
the clearest case of the shape: usql cannot fix it from its side.

**508, a child process reading the same terminal.** Measured here with a
control: ten keys, no child, the editor reads ten; with a child holding the
same pseudo-terminal, it reads none. The split belongs to the other reader —
`cat` reads greedily and takes everything, `less` reads one key and waits,
which is why the report describes alternation. What is missing is a contract
for handing the terminal over for the length of a child and taking it back.
The reporter's asymmetry, that the first pager after startup behaves,
constrains any answer and is not yet explained.

**478's invariant, which is about what can be observed.** `SetCompleter` and
`SetHighlighter` are write-only, so nothing can ask a Session what it holds
and no test can assert that connecting replaced the completer. Inside, the two
states already differ.

### rline has a defect of its own

**The focus mis-decode**, behind 93, 122, 483 and 490 as a hypothesis and a
defect on its own terms regardless. `CSI I` and `CSI O` are decoded as Tab and
F3 when they are focus notifications. Two of those four reporters named
alt-tab; none named a pager.

### New features, never in isocline either

**472**, which wants both halves: bracketed paste so a paste does not trigger
completion, and a quoted-insert binding so a tab can be typed. `key.CtrlV`
exists as a code with nothing behind it, and bracketed paste is in neither
implementation. Nobody dropped these in the port.

**236 and 552**, vi bindings, filed five years apart. Worth knowing before a
mode abstraction is designed rather than after.

### Already answered here

**414**, a history file with entries concatenated and no newlines. Every entry
is escaped before writing, so an embedded newline becomes two characters, and
each entry is followed by a real one. The file is renamed over rather than
truncated, and one that was not read in full is refused. Keep it as a
regression test rather than as work.

### Probably not this package, stated narrowly

**546**, a busy loop on static builds. At end of input the first `ReadLine`
returns `io.EOF` in about a microsecond and so does every later one, so a
caller that checks the error cannot spin. That covers the EOF path and not the
static-link case: if a static build changes what `isTerminal` answers, a
different path is taken before EOF handling is reached. The mechanism as the
reporter described it is not here; that is narrower than "not ours".

**528** is evidence rather than a proposal — it predates the decision to use
this package.

### Not examined

**72**, prefix filtering on up-arrow, and **320**, walking history by query
rather than by line. Both read as requirements rather than defects and both
want the model layer.

### What is not guarded

None of the above has a test. 508 and the focus mis-decode were measured by
probes that were then removed, and 414 is answered by design rather than by
anything that would fail if the design changed. They are findings, not
behaviour anyone is holding in place, and the layer that would hold them —
the consumer contract — is the one TESTING.md records as absent.

## A focus notification is read as a keystroke

Measured while reading usql's open issues. The escape decoder maps `CSI I` to
Tab or PageUp and `CSI O` to F3. Those are focus in and focus out, which a
terminal sends when a window gains or loses the keyboard — on alt-tab, for
instance.

Neither this port nor isocline ever enables focus reporting, so in a clean
terminal they never arrive. They arrive when something else turned it on and
did not turn it off, which a program run from the same shell can do.

Four usql issues over five years describe what that would look like from the
outside — 93, 122, 483 and 490, two of them naming alt-tab. See TESTING.md.
The mis-decode is measured; that it is what those four reporters saw is a
hypothesis.

A fifth issue, 508, was first recorded here as the other end of this and is
not. It is a second seam and the two are unrelated: see the section below.

## Two programs reading one terminal, and no way to say whose turn it is

usql issue 508: input meant for a pager is read by the prompt instead. It was
recorded here as probably the same fault as the focus-reporting mis-decode
above, on the reasoning that a pager could leave a mode set. That was wrong
and the usql session corrected it with the reporter's own measurement: strace
shows `less` opening the terminal directly and reading every other keypress.
Nothing is left in a bad state. Two processes have the terminal open and the
kernel gives each keystroke to whoever reads first.

This port inherits it, measured with a control rather than assumed. A child
started on the same pseudo-terminal, then ten keys sent:

    no child      the editor read 10 of 10
    with a child  the editor read 0 of 10

The child there was `cat`, which reads greedily in a loop and took everything.
`less` reads one key and waits, which is why the reporter saw a clean
alternation rather than starvation. So the split is not a property of the bug,
it is a property of the other reader, and a fix that tunes the sharing rather
than ending it would behave differently against every pager.

What it asks for is a contract this API does not have: a way to stop reading
and give the terminal back for the length of a child process, and to take it
back afterwards. Whether that is a Suspend and Resume pair or handing the
caller the descriptor with a documented obligation is a design question. What
is not in question is that "whoever reads first wins" is what happens when
nothing says whose turn it is.

One thing the reporter found constrains any answer: the first pager after
startup behaves and later ones do not. Nothing here explains that asymmetry
yet, and a fix that stops the double-read without explaining it is probably
incomplete.

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
which took three attempts to get right and is the method note in LESSONS.md. Ten
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
the first time in the old PLAN.md a tag has been the right answer rather than
the problem.

And the two sides were not symmetrical. Unix had a compile error if the safe
path was lost; Windows had a paragraph, and paragraphs in the old PLAN.md have
gone stale three times. The direction that would rot it is the harmless one —
someone adding `startNoEcho` to `sys_windows.go` would make Windows safe and
this entry false, with nothing to notice. So `sys_windows_test.go` carries
`TestConsoleDoesNotOfferNoEcho`, a runtime check, because Go cannot assert at
compile time that a type does *not* satisfy an interface. It is a test whose
failure means the document is wrong rather than the code, and it says so in
its own message.

What it needs is a way to stop recording for the length of a password read,
which is a change to what `sessionLog` promises rather than to the password
code, and the logger's type is already an open question in PLAN.md. Fixing one
without the other would settle the second by accident.
