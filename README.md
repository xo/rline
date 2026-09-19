# rline

`rline` is a readline package for Go. A readline package reads a line of text
from a terminal, and gives the person typing it editing, history, completion
and colored output.

This package is written in pure Go. It does not use cgo, and it does not link
against any C library.

`rline` is a port of [isocline][isocline], a readline replacement written in C
by Daan Leijen, under the MIT license.

## Status

Complete and working on Linux, macOS and Windows, including cmd.exe, Windows
PowerShell and pwsh. Every module of isocline is either ported or deliberately
replaced.

What works: editing a line, moving by character, word and line, the eleven
kinds of delete, undo and redo, input over more than one line, syntax
highlighting, matching braces, hints, markup for colored output, a history
file, and reading a plain line when there is no terminal to edit on.

`Password` reads a line without showing it, without recording it, and without
completion or highlighting.

Completion is finished too. Tab fills in the longest start every answer shares,
and offers a menu when several remain. The menu lays out in columns, and how
many depends on how wide the entries are: in the example, `c` then Tab gives
three columns and `co` then Tab gives two.

Inside, the port keeps the C's behaviour even where that behaviour is wrong,
because recorded output from the C build is the test corpus. The interface is
deliberately not a translation of the C one: it is shaped for Go. The one
place a behaviour rather than a name departs is `ReadLine` answering
`ErrInterrupted`, because the C cannot tell a line the user gave up from an
empty one and a shell has to.

## Quickstart

```go
p, err := rline.New(
    rline.WithPrompt("> ", "| "),
    rline.WithHistoryFile(".history"),
    rline.WithHistoryLimit(200),
)
if err != nil {
    return err
}
defer p.Close()

for {
    line, err := p.ReadLine("")
    switch {
    case errors.Is(err, io.EOF):
        return nil            // the input ended: Ctrl-D on an empty line
    case errors.Is(err, rline.ErrInterrupted):
        continue              // the line was given up: Ctrl-C
    case err != nil:
        return err
    }
    fmt.Fprintf(p, "read: %s\n", line)
}
```

`New` returns a `*Prompt`: a `Session` that reads lines, with the markup
`Writer` that goes with it. The reading methods are promoted, so `p.ReadLine`,
`p.Password` and `p.Close` are the `Session`'s.

A `Prompt` is an `io.Writer` for plain text, which is what most of what a
program prints is — a bracket in it is not a tag. Markup goes through
`p.Markup()`, which is never nil:

```go
fmt.Fprintf(p, "read: %s\n", line)                     // plain
fmt.Fprintf(p.Markup(), "[ic-error]%s[/]\n", err)       // styled
```

Write through the `Prompt` rather than to `os.Stdout`, because the terminal it
writes through is the one that knows where the prompt is. A `Prompt` is an
`io.Writer`, so hand it straight to anything that wants one.

`ReadLine` answers `io.EOF` when the input ends and `ErrInterrupted` when the
user gave the line up with Ctrl-C. Those are different answers on purpose: the
C cannot tell them apart, and a shell has to.

## The example

`example/` is a small SQL prompt. It shows input over more than one line,
highlighting that changes as the line is typed, and a completer.

A statement ends at a semicolon. Until then Enter starts another row inside the
same line rather than handing it back, which is what `WithContinue` does. The
rows are one buffer, so the up and down keys move the cursor between them and
the whole statement comes back at once.

`\pass` reads a password and then echoes it back, so that what was collected
can be checked against what was typed. A real program would not echo it.

Run it with:

```sh
go run ./example
```

Do not build it with `go build -o example ./example`. The name given to `-o`
is a directory that already exists, so the binary lands at `example/example`
rather than in the current directory, and an older binary of the same name
goes on running. Use a different name:

```sh
go build -o rlex ./example && ./rlex
```

Pass `-log FILE` to record the session. Every line says which direction it
went: `<` for what was written to the terminal, `>` for a key that was read,
and `=` for a finished line. The bytes are escaped, so the log can be read by
eye.

## How the port was checked

Reading C and writing Go that looks like it is not enough to know the two
agree. So each module was checked against the C rather than against a reading
of it.

A probe program links the C code, calls it with a fixed corpus of inputs, and
writes down what it returns. The Go port then answers the same corpus and the
two are compared. There are fifteen such corpora, holding about 130000
recorded calls, and `tools/` holds the probe that produced each one.

The whole editor is checked the same way. `internal/capture` starts a program
under a pseudo-terminal, sends it keystrokes, and records every byte it wrote.

This found faults that reading would not. It also found the limit of the
method: a corpus checks that a function answers correctly, and nothing in a
corpus checks that the functions are wired together, so the parts that only a
running program exercises are tested by running one.

Between the two sits a third kind of test, which feeds a string of keystrokes
to the editor and asserts on the line and the cursor that come out. That is
what covers the key dispatch — which key reaches which operation — and it is
where the behaviours other line editors test live: how a line ends, input
that is not UTF-8, characters wider than one column, and a completion list
too long to show.

A test that was never attacked is not known to work, so before a corpus is
trusted the code it covers is broken on purpose, once per thing the corpus is
meant to pin, and each break has to fail it. `tools/mutate.sh` does that, and
reports "did not compile" as its own answer rather than folding it into
"caught".

Linux, macOS and Windows each have their own session running the same checks,
and the Windows editor has been driven by hand from a real console — typing,
highlighting, backspace, multi-row statements, the completion menu, the arrow
keys between rows, the password read, and leaving — because everything else
on that platform is a test or an injected record.

`PLAN.md` records the port order, every place the port departs from the C, and
why.

## Platforms

Linux and macOS are supported and tested.

Windows builds, and every corpus passes there. The terminal layer is written
and the console asks to read escape sequences, which Windows 10 and later do.
Recorded sessions for Windows do not exist yet, so that part is unproven.

## About

`rline` supports [usql][usql], a universal command-line interface for SQL
databases.

[isocline]: https://github.com/daanx/isocline
[usql]: https://github.com/xo/usql
