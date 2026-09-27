# What the tests taught

This document is the record of how a test on this port reported success and
checked nothing, and of what was changed each time. Read it before you write
a test, a corpus or a check. [TESTING.md](TESTING.md) holds the design of the
suite. This document holds the history behind it.

The sections came from the old `PLAN.md` in the root, unchanged, when the
documents moved to `docs/` (D36). Most of them name the session that found
the fault. `windows-vm` and `ken-mba` are two coding agent sessions, on a
Windows machine and on a Mac.

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
`isTerminal` left seven mentions of the old name in PLAN.md, describing
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

Enumerate the arms; do not list them. The platform split in USQL.md, for
the password, was got wrong twice before it was got right, and both times
by measuring rather than
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
