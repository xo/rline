# rline

`rline` is a readline package for Go. A readline package reads a line of text
from a terminal, and gives the person typing it editing, history, completion
and colored output.

This package is written in pure Go. It does not use cgo, and it does not link
against any C library.

`rline` is a port of [isocline][isocline], a readline replacement written in C
by Daan Leijen, under the MIT license.

## Status

The editor works. Two parts are not finished.

What works: editing a line, moving by character, word and line, the eleven
kinds of delete, undo and redo, input over more than one line, syntax
highlighting, matching braces, hints, markup for colored output, a history
file, and reading a plain line when there is no terminal to edit on.

What does not work yet: completion when the Tab key is pressed, and walking
through the history with the arrow keys. Both are being written.

## Quickstart

```go
r, err := rline.New(
    rline.WithPrompt("> ", "| "),
    rline.WithHistory(".history", 200),
)
if err != nil {
    return err
}
defer r.Close()

for {
    line, err := r.ReadLine("")
    if errors.Is(err, io.EOF) {
        return nil
    }
    if err != nil {
        return err
    }
    fmt.Fprintf(r, "read: %s\n", line)
}
```

`ReadLine` answers `io.EOF` when the input ends. A `Reader` is an `io.Writer`
for plain text, and `Print` writes markup such as `[red]text[/red]`.

## The example

`example/` is a small SQL prompt. It shows input over more than one line,
highlighting that changes as the line is typed, and a completer.

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
