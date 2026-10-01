# Platforms and checks

This document says which systems rline runs on, what was measured on each,
how the Windows console is handled, and which checks run on the code and in
continuous integration. It ends with the gaps in those checks that were left
on purpose.

The decisions that these sections carry out are in [PLAN.md](PLAN.md). The
sections came from the old `PLAN.md` in the root, unchanged, when the
documents moved to `docs/` (D36).

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
summary flattens them to spaces, which is what `_examples/sql/main.go` asks for.

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
section on baselines in LESSONS.md: a count that is confidently wrong looks exactly like a count
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
test written for Windows, which is the tagged-out shape from the list in LESSONS.md,
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

## Verifying with a list, in the commit about not using lists

The commit that added a font size to uitest broke every CI job at aix/ppc64
with `undefined: setFontSize`. Two mistakes made it, and the second is the one
worth keeping.

The first: the edit that added the function to `platform_other.go` was a
string replacement whose anchor no longer matched, because gofmt had realigned
that line earlier in the same session. Every other edit that day asserted the
match count first. This one did not, so it reported success and changed
nothing.

The second: it was then verified by vetting darwin and windows, typed by hand,
in the same week as a commit arguing that a loop over a hand-written list
carries the author's blind spot into something that reads like data. The
enumeration was one command away and had been written into the CI workflow two
days earlier. Knowing the rule, having written it down, and having automated
it elsewhere were all insufficient; what would have been sufficient is running
it here.

Worth being exact about what the local check would have caught. `go vet` for
darwin and windows passed, because those files did get the function. aix takes
the fallback, and nothing pointed at the fallback until CI did. So the blind
spot was not subtle: it was the one platform group the edit was actually for.

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
