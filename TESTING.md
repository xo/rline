# How rline is tested, and how it should be

This is the design for the whole suite: the layers, what each one alone can
answer, what it must not be asked, and what is missing today. It was written
against Gemini and DeepSeek asked the same question independently, and where
they disagreed with each other or with what this repository already does, the
disagreement is recorded rather than resolved by whoever wrote this down.

PLAN.md holds the record of what happened and what was learned. This holds the
shape.

## The layers

Seven, and the ordering is by what they can see rather than by how many there
are of them. Both models produced substantially this list without seeing each
other's.

**L0, the model.** Pure functions and small state machines, tested in memory
with no terminal anywhere: character widths, UTF-8 slicing, BBcode parsing,
palette reduction, the history ring, the undo stack, escape decoding. Answers
whether an algorithm is right, and edge cases are cheap here and expensive
everywhere else. **Not** escape sequences, redraw, or anything about a real
terminal.

**L1, conformance with the C.** The recorded corpora: probes that `#include`
isocline's own sources, replayed in Go and compared byte for byte. Answers one
question and only one — did the port change what the original did? **Not**
anything the C has no answer for, which means nothing about the Go-only API,
cancellation, `log/slog`, or usql. A corpus cannot record what the C has no
opinion about, and this document's worst failures have come from forgetting
that.

**L2, session semantics.** The `Session` driven over fixed input with streams
in memory, asserting what a user would say happened: the line that came back,
the history that resulted, the completions offered, the undo state. Answers
behaviour without the noise of escape sequences. **Not** the bytes: asserting
`\e[2K` here produces tests nobody can read and that break when a redraw is
made more efficient without changing what is drawn.

**L3, the pseudo-terminal.** `internal/capture`: a real pty, input in chunks,
output recorded until the program goes quiet, compared against byte-exact
transcripts. Answers the redraw protocol — what is actually emitted to move a
cursor, repaint a wrapped line, clear a menu, turn echo off. This is where
resize, bracketed paste and password echo belong. **Not** rendering, and not
line-editing logic that L2 answers more legibly.

**L4, real terminals.** `uitest`: real emulators on a private compositor, a
virtual keyboard, screenshots. Answers the two things no other layer can — what
a terminal *renders*, and what a terminal *encodes a keystroke as*. Shift-enter
and ctrl-left do not exist in ASCII, and which bytes they become is the
terminal's decision. **Not** editing logic. Running six GPU-accelerated
emulators to find out whether history search found the right entry is a waste
of a machine and an hour.

**L5, the consumer contract.** usql's seam: a fake parser owning the read loop,
pulling lines through a callback, recomputing the prompt per line from its five
states. Answers whether rline holds up under inversion of control, which is the
thing usql actually does. **Not** rline's internals.

**L6, robustness.** Fuzzing, mutation testing, races, cross-compilation.
Answers whether it crashes or fails to build. Not a correctness oracle: a
mutation that survives is a question, not a defect.

## What exists today, honestly

L1 is the strongest layer by a distance: roughly 130,000 calls across 19
corpora. L3 exists and is real. L4 exists as of this week. L6 exists as
`tools/mutate.sh` and the cross-vet loop.

L0 exists but unevenly — `internal/text`, `ansi` and `key` have real unit
tests; other pure logic is covered only through L1, which means it is tested
against the C rather than against what is true.

L2 half exists, in `driven_test.go` and the `feedEnv` helper, and has no name.
It is the layer most likely to be skipped when a feature is added, because
there is no directory that obviously wants the test.

L5 does not exist at all. There is a section in PLAN.md measuring what usql
would need, and no test that holds rline to it.

## The gaps, by feature

Both models were asked to name these specifically rather than in general, and
they agreed closely. In rough order of what would hurt most:

**Resize.** Untested anywhere. A window that narrows while a statement is
spread over rows is the case most likely to put the cursor in the wrong place,
and `uitest` now has two sessions for it — but nothing at L3 asserts the bytes,
and nothing at L0 asserts the arithmetic.

**Bracketed paste.** Untested. Pasting a hundred lines with semicolons in them
must arrive as text and not as a hundred decisions to execute something.

**History search.** Ctrl-R, a query typed into it, backspacing inside the
query, cycling matches, cancelling and having the original line back. Covered
only incidentally.

**Undo.** Covered by L1 and nothing else, so it is pinned to the C and untested
as a property. Undo after a completion, after a paste, after a kill, and the
redo chain dropping when you type after undoing.

**The help screen.** Drawn over a line being edited and then taken away again,
which is a redraw problem with a modal in it.

**Password.** There is one test and a known defect: on the systems without a
no-echo terminal the password is written into the session log. See PLAN.md.

**BBcode, palette reduction and wcwidth** are pinned at L1 and thin at L0.
Malformed tags, RGB to 256 to 16 being deterministic and monotonic, combining
marks and ZWJ sequences where the cursor column and the display column have to
agree.

**Brace matching** across quotes and SQL strings, where an apostrophe in a
string literal must not open anything.

## What usql needs and nothing tests

A `context.Context` on `ReadLine`, cancelled while a read is blocked, returning
`context.Canceled` with the terminal put back and no goroutine left spinning on
a descriptor. The machinery for this exists and is orphaned: `asyncStop` makes
a blocked read return and `key.EventStop` unwinds the edit loop, and the only
caller outside the interface is a test.

The prompt recomputed between lines of one statement, which is what usql does
on every line, crossed with history navigation that has to keep the prompt's
width right.

A logger that is a `log/slog` handler rather than an `io.Writer`, which is also
what would let a password read say "not this" — see the open question in
PLAN.md.

Cursor shape, vi modes, terminfo and inputrc, each of which is a decision
before it is a test.

And the seam itself: an API under which usql stops owning the read loop.

## A regression test waiting for the consumer layer

From the usql session, and the clearest argument yet for building L5.

usql issue 478: table name completion stopped working between 0.18.x and
0.19.x. Three lines had been dropped from `handler.go` — the reinstalling of
the completer after a successful connect — so usql kept the default completer
it had installed at startup, which knows connection strings and has no
database connection and therefore no table names. Reported by five people
against three databases on two platforms, live for about ten months, and
eventually found to have been fixed months earlier with nobody closing the
issue.

What makes it worth a test here rather than only there is the shape of the
failure. Completion did not error and did not crash. It returned an empty
candidate list, which is indistinguishable from "there is nothing to complete
at this point". Nothing in any suite noticed.

The test is not that completion returns the right names, which needs a
database. It is that the completer in effect after connecting is not the one
installed before it — the invariant that actually broke — and it belongs at
L5, which is the layer that does not exist.

### What it asks of rline's API

`SetCompleter` is write-only: there is no way to ask a Session which completer
it holds. Inside, the two states are already distinct, because `completer ==
nil` short-circuits before anything is generated. From outside they are not,
and a caller cannot write the assertion that would have caught this.

Whether to expose that is a design question rather than a test one, so it is
in PLAN.md's open questions rather than settled here. The general form is
worth stating either way: an API that lets a caller reach a state where a
feature silently yields nothing, with no way to ask whether the feature is
attached at all, will grow this bug again in some other shape.

## What usql's open issues ask for

From the usql session, as requirements for the migration rather than as bugs.
Eight issues are held open against the switch to this package.

**A five-year family: #93, #122, #483, #490.** The same symptom in four
reports across five years — the editor silently starts treating ordinary keys
as though a meta prefix were pending, so `b` goes back a word, `f` forward,
`d` deletes to end of line. Two name alt-tab or window switching as the
trigger, on Windows; two name no trigger and no platform.

Measured here, which is a mechanism and not yet a demonstration: the escape
decoder turns `CSI I` into Tab or PageUp and `CSI O` into F3. Those two
sequences are what a terminal sends on focus in and focus out, and alt-tab is
what produces them. So a focus notification arrives as a keystroke, and a
spurious Tab opens the completion menu, after which ordinary keys do something
other than insert themselves.

Neither rline nor isocline ever asks for focus reporting, so those sequences
should not arrive at all — unless something else enabled it and did not turn
it off. Which is the next issue.

**#508, and why it is probably the same bug.** Input meant for a pager is read
by the prompt instead, and the first pager after startup behaves while a later
one does not. That asymmetry is a terminal handed to a child and taken back in
a different state than it was lent in. A pager that enables focus reporting and
exits without disabling it would leave exactly the condition the family above
needs.

Stated as separable claims, because the middle one is the only one measured:
a program can leave modes set that rline did not set; rline mis-decodes two of
them into keystrokes; and that is what the four reports describe. The third is
a hypothesis. The second is a defect either way — a notification is not a key,
and decoding one as the other is the same species as an empty candidate list
being indistinguishable from nothing to complete.

**#472, which needs two things neither implementation has.** Tab is bound to
completion, so a literal tab cannot be typed or pasted: `select 'a<TAB>b'` is
impossible. `key.CtrlV` exists as a code and nothing implements literal-next,
and bracketed paste is absent from rline and from isocline both. Paste wants
bracketed paste, because pasted text should not trigger completion at all;
typing wants a quoted-insert binding. Both are new work rather than port gaps.

**#236 and #552, vi bindings**, filed five years apart, with chzyer/readline's
vi mode named as prior art. Not urgent, and worth knowing before a mode
abstraction is designed rather than after.

### The shape they share

Every one of these produces plausible output rather than an error. Nobody
reported a crash; they reported that `b` went backwards. That is why the
family survived five years and four reporters: each report reads as user
confusion. It is the same shape as the completer that silently returned
nothing, and the same shape this repository keeps finding in its own tests.

The test that would catch the family is one that drives a session, injects a
focus sequence mid-line, and asserts what the next ordinary key does. That is
L3, and it does not need a database, a terminal emulator, or usql.

## Organisation

Assertions for anything that can be stated: a cursor column, a history entry,
an error being `context.Canceled`. Goldens only for byte protocols and for
pictures, which is to say L1, L3 and L4. A golden that could have been an
assertion is a golden nobody will read.

Every golden needs a generator, and the generator is checked in. A recording
whose origin is a person's afternoon cannot be regenerated when the thing it
records legitimately changes.

A new feature arrives with, in order: a note saying what it is for; a C probe
**only** if parity with isocline matters, and never for something Go-only,
because the corpora are a record of the original and widening them to cover new
work destroys what they are for; unit tests at L0; a transcript at L3 if it
emits anything; a `uitest` session if it can be seen; and an L5 case if usql
touches it.

## What is deliberately not tested

**Pixels across machines.** Font rendering varies by hinting, subpixel geometry
and graphics driver. Screenshots are artifacts for a person; the mechanical
comparison is the byte log. Both models said this unprompted.

**The completion menu's timing**, today. Whether the menu is still listening
when the next key arrives varies between runs, so that session takes pictures
and compares nothing. It is an open question rather than a settled behaviour.

**The C library itself.** The corpora encode its behaviour; testing isocline is
not this project's job.

**Terminals nobody has.** vt52 and adm3a are not targets. `TERM=dumb` falling
back to a plain reader is, and that is a different test.

**Performance as correctness.** Benchmarks are a regression gate, not an
oracle.

## Where both models disagree with what this repository does

Recorded because it is a live question and not a settled one.

The cross-vet loop runs over all 47 `GOOS/GOARCH` pairs on every push, and both
models think that is too much. Gemini's argument is that terminal behaviour is
decided by three OS families, and that `linux/riscv64` against `linux/amd64`
tests the Go compiler rather than this package. DeepSeek's is milder: keep the
sweep, run it nightly rather than per-commit.

The counter-argument, which is this repository's own history, is that the loop
has caught real faults twice — a test file whose build tag stopped matching its
source, and a function implemented for two of three tag groups — and that both
were invisible to a three-platform check. The cost is minutes on a hosted
runner.

What is not in dispute is that the *architectures* add nothing: the value is in
the 15 systems, not the 47 pairs. Whether to keep sweeping pairs, and whether
per-commit or nightly, is Ken's.
