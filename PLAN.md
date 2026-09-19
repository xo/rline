# Port plan

This document records how `rline` becomes a pure Go port of isocline. isocline
is a readline replacement written in C by Daan Leijen, under the MIT license.
The C source sits in `isocline/`, which is a separate git repository. We do not
commit to it.

## Decisions

The port is one Go package named `rline`. Each C module becomes one or more
files in that package. The C headers contain a cycle, because `attr.c` includes
`term.h` and `term.h` includes `attr.h`. One Go package removes that cycle.

The port does not use cgo at any stage. cgo is the Go facility that calls C
code. An earlier plan built a cgo binding layer first, to get a reference to
compare against. We dropped that step for two reasons. The binding code gets
discarded at the end. The completion and highlight callbacks cost real work to
pass across the boundary.

A module is the unit of work, not a function. isocline passes its own allocator
into most structures, as the type `alloc_t`. A module keeps that ownership in
one place, so a module ports cleanly and a single function does not.

## Port order

The C headers give an acyclic order. Port the modules from the leaves up:

1. `common.c` and `common.h`. This holds the allocator, the UTF-8 helpers and
   the character classes. Delete the allocator. Go collects garbage.
2. `wcwidth.c`. This is the wcwidth function of Markus Kuhn. It returns 0 for
   combining marks and for control characters.
3. `stringbuf.c`. This is a growable buffer that moves the cursor by character,
   not by byte.
4. `tty.c` and `tty_esc.c`. This reads the terminal and decodes escape
   sequences.
5. `attr.c`. This holds the text attributes.
6. `term.c` and `term_color.c`. This writes to the terminal and reduces colors
   to what the terminal accepts.
7. `bbcode.c` and `bbcode_colors.c`. This parses markup such as
   `[red]text[/red]`.
8. `history.c`, then `undo.c`.
9. `completions.c`, then `completers.c`.
10. `highlight.c`.
11. `editline.c`. This is the edit loop and the key dispatch. The files
    `editline_help.c`, `editline_history.c` and `editline_completion.c` are
    textual includes of `editline.c`, not separate units.
12. `isocline.c`. This is the public API. API means Application Programming
    Interface.

`wcwidth.c` is a textual include of `stringbuf.c`, so the two port together.

## Test method

The C code has no unit tests. The `test/` directory holds two demo programs.
Build the test corpus from recorded bytes instead:

1. Add a capture mode to the C demo. Run the C demo under a pseudo-terminal. A
   pseudo-terminal is a program that acts as a terminal for another program.
2. Record the input bytes and the output bytes as a golden file. A golden file
   holds the expected output of a test.
3. Feed the same input bytes to the Go port. Compare the output bytes exactly.

Fix the terminal size and the `TERM` variable, or the output changes between
runs. Inject a clock, because the escape decoder uses a timeout to tell the
Escape key from an escape sequence.

Add fuzz tests for the escape decoder and for the markup parser. A fuzz test
feeds random input to find errors. Run the C code and the Go code on the same
random input, and compare. Turn each difference into a test.

Pin the Unicode version that the width tables use. The golden files change when
that version changes.

## Open questions

Four questions have no answer yet.

First, does `rline` adopt `github.com/xo/terminfo`? isocline contains no
terminfo code. `term.c` reads the `TERM`, `COLORTERM`, `NO_COLOR`,
`WT_SESSION`, `ITERM_SESSION_ID` and `VSCODE_PID` variables to pick a color
palette. terminfo is therefore new work, not saved work.

Second, does `rline` adopt `github.com/xo/inputrc`? isocline never reads an
inputrc file, because it carries fixed key bindings. inputrc is a new feature.

Third, which package measures character width? `github.com/mattn/go-runewidth`
matches the C behavior. `golang.org/x/text/width` does not, because it gives
East Asian width only and returns no zero width for combining marks.

Fourth, how does `isocline/` reach another host? It is a separate git
repository, so this repository ignores it for now.

## License

isocline is MIT licensed, and the copyright belongs to Daan Leijen, 2021. Carry
that notice into the Go source files that derive from the C source.
