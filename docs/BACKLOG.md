# Backlog

This is the work that is known and not done. Each item says where it came
from. An item that waits on a choice names the open question in
[PLAN.md](PLAN.md) that holds the choice. rline has no open issue and no
open pull request of its own on 2026-09-27, so most items come from usql and
from the documents in this folder.

When you finish an item, delete it here, and say in the commit which item it
was.

## What usql needs first

usql labels its line editor issues `readline`, and fourteen are open on
2026-09-27. D35 in [PLAN.md](PLAN.md) makes that list the queue for this
repository. [USQL.md](USQL.md) sorts the issues, and these items need
something from rline before usql can fix them.

1. Draw the prompt after text that a caller left in the middle of a row.
   The redraw starts with an unconditional carriage return, so `\echo -n`
   output is written over. rline has no way for a caller to say that the
   cursor is where it meant to leave it. From: usql issue 215, which Ken
   kept as a numbered backlog item. usql said on 2026-10-30 that it still needs this, at a low priority, because it only shows after `\echo -n` and `\warn -n`. Ken will consider it later (2026-10-30).
2. Explain why the first pager after startup behaves and later ones do not.
   D48 added `Session.Release`, which gives the terminal to a child process
   and takes it back, and usql/pager uses it. Check usql issue 508 again
   with that pager. From: usql issue 508, and "Two programs reading one
   terminal" in [USQL.md](USQL.md).
## Faults of rline's own

4. A focus notification is decoded as a key. The decoder reads `CSI I` as
   Tab or PageUp and `CSI O` as F3. It is a fault whether or not it is what
   the reporters saw. From: "A focus notification is read as a keystroke" in
   [USQL.md](USQL.md), as the likely cause of usql issues 93, 122, 483 and
   490.
6. File name completion uses `/` as the only path separator, so it is wrong
   on Windows. From: the comment on `dirSeparator` in `comp.go`, found while
   this file was written. The comment said that the old `PLAN.md` recorded
   it, and it did not.

## New features

7. Bracketed paste, so that a paste does not trigger completion. rline sends
   `ESC[?2004h` in raw mode and `ESC[?2004l` when it leaves it and in
   `Release`, decodes `CSI 200~`, and reads raw bytes up to `ESC[201~`
   rather than keys, so that an escape in the paste survives. The stub of
   `bracketed-paste-begin` in `internal/readline` rings the bell until
   then. Ctrl-V now runs readline's `quoted-insert`. From: usql issue 472,
   and isocline pull request 43 for the decoding.
9. History that filters by prefix on the up arrow, and a walk through the
   history by query. Nobody has examined either yet. From: usql issues 72
   and 320.
11. Rewrite the history. D10 records that it is going to be rewritten. Three
    things wait for the rewrite. The file mode is settled: D21 stands, as
    readline does. One is an explicit access control list on Windows. One is
    whether the file becomes an append-only log that is compacted, as
    `e19767f` describes. From: D10, D21, and "Known departures from the C
    code" in [PORT.md](PORT.md).

## Tests that are missing

12. The consumer contract layer, L5, does not exist. It is the layer that
    holds usql issues 215, 414, 478 and 508 and the focus fault in place.
    None of them has a test today. From: "What exists today, honestly" in
    [TESTING.md](TESTING.md), and "What is not guarded" in
    [USQL.md](USQL.md).
13. The session layer, L2, has no name and no folder, so a new feature skips
    it. From: [TESTING.md](TESTING.md).
14. Resize, history search, undo as a property, the help screen, brace
    matching across quotes, and the model layer of markup, palette reduction
    and width. From: "The gaps, by feature" in [TESTING.md](TESTING.md).
15. usql issue 414 is answered by the design of the history file and held by
    no test. Keep it as a regression test. From: "Already answered here" in
    [USQL.md](USQL.md).
16. The static build in usql issue 546. The end-of-input path is measured
    and does not spin. A static build that changes what `isTerminal` answers
    is not tested. From: "Probably not this package" in [USQL.md](USQL.md).
17. The markup parser has no fuzz test. Ken's first plan asked for one. From:
    "Test method" in [PORT.md](PORT.md).
18. Nothing records `generateCompletions` against the C. A driven test holds
    it, and a recording says what the C did. From: "Tests that pass
    without checking anything" in [LESSONS.md](LESSONS.md).
19. No session is recorded on Windows. Recording needs a pseudo console from
    `CreatePseudoConsole`, and nobody has written it. Until then CI skips
    `TestGoldenSetIsPresent` there. From: "Continuous integration" in
    [PLATFORMS.md](PLATFORMS.md).
20. The example banner sets no background color, so a byte check of it
    cannot show a color that bleeds to the end of a row. It needs one line
    with a background left open, and a person to look. From: "What the
    banner does not watch" in [LESSONS.md](LESSONS.md).
21. `uitest` cannot time the completion menu, so that session compares
    nothing. On macOS and Windows it types on the shared desktop with a focus
    check, and has no private compositor. From: "Tests against a real
    terminal" in [LESSONS.md](LESSONS.md).

## Found while this setup was written

22. The repository has no `LICENSE` file. `ansi`, `internal/text` and
    `internal/editor` derive from the C and carry no MIT notice. The
    package comments of `rline` and `key` carry it for their own packages.
    From: D5, which says that each file derived from the C carries the
    notice.
24. The receivers of `tty` are named `d`, and those of `keyDecoder` are named
    `t`. Both are left from before `d08ea9a` renamed the types, when `tty`
    was `ttyDevice` and `keyDecoder` was `tty`. A receiver is named for its
    type.
25. [USQL.md](USQL.md) says that thirteen usql issues are open, and fourteen
    are open on 2026-09-27. usql issue 478 is closed. Count again against
    usql before the section is relied on, as the section itself asks.
26. The list in "Where things live" in [LAYOUT.md](LAYOUT.md) names
    `tty.go` for reading keys. `d08ea9a` renamed that file `decoder.go`, and
    `tty.go` does not exist. The list in AGENTS.md is correct.

## Found while the transit example was written

27. rline does not tell a highlighter what changed. It hands over the whole
    text, so a highlighter that parses again from the old tree, as transit
    asks, has to compare the text with the text before. `editBetween` in
    `_examples/transit/main.go` does that. From: the transit example, which
    Ken asked for as the test of the transit API.
28. rline sorts the completions before it draws the menu, so a completer
    cannot put the keywords before the other tokens. isocline pull request
    39 adds a function to set the sort or turn it off, which is the same
    idea. From: the transit example.
29. No test in `_examples/transit` holds the rule that a node with the same
    range as the node before it keeps the first style. No sample in seven
    languages had two such nodes with different styles, so breaking the rule
    changed nothing. From: the transit example.

51. `_examples/transit` still writes `[ic-info]`, because its `go.mod`
    requires a published rline that has only the old style names. Change it
    to `[rline-info]` when the `go.mod` moves to a release with D49 in it.
    From: D49.

## Found when the examples moved

30. Nothing vets or lints `_examples/sql` since D37 moved it, because
    `./...` leaves out a folder whose name starts with an underscore. The
    tests still build it. Naming the folder in the vet steps of CI and in
    `tools/lint.sh` would put it back under both. From: D37.
## Found while answering usql

32. No test holds that a highlighter is given every row of the text.
    `prompt.go` hands it `e.Input.String()`, which holds them all, and
    "What rline offers usql" in [USQL.md](USQL.md) says that usql's
    highlighting rests on it. From: a question from the usql session on
    2026-10-01.
33. A continuation prompt that follows the text. usql draws `%R` on each row,
    which says whether the row is inside a string or a parenthesis. rline has
    one continuation marker for every row, set by `WithPrompt` or
    `SetPrompt`. Nothing says what happens if `SetPrompt` runs during an
    edit, and nothing tests it. Supporting `%R` needs new API, such as a
    function that is given the text and returns the prompt of each row, and
    Ken decides it. From: the usql session, 2026-10-01. usql said on 2026-10-30 that it still needs this, as a function that is given the text before a row and returns the prompt of that row. Ken will consider it later (2026-10-30).
## Found in phase 1 of the readline work

39. `yank-nth-arg` and `yank-last-arg` split an earlier line at spaces.
    readline splits it as the shell would, so a quoted word with a space in
    it is one word there. No case records a quoted word yet. From: phase 1.
40. `transpose-chars` with a numeric argument drags the character more than
    one place in readline. Only the count of one is written and recorded.
    From: phase 1.

## Found while the keys moved to readline

52. Nothing tests that a command that can do nothing rings the bell.
    `runEditLoop` calls `term.bell` when `readline.Line.Bells` goes up, and
    `term.bell` writes to `os.Stderr` itself, so a test cannot see it.
    Breaking the call fails no test. From: backlog 41.

## Found while completion moved to readline

65. Make the README GIF again with `contrib/make-demo-gif.sh`, and put it on
    the `assets` branch. The `readme` session now indents its rows alike and
    waits after the last Enter, so the result and the next prompt show. The
    script reads the window from the screenshot of that session, and foot
    keeps a solid cursor without focus. The result line still wraps onto two
    rows, and a shorter statement would fix it. Ken will make the GIF later.
    From: the release of v0.2.0, and Ken's recording on 2026-10-30.
## Found while search moved to readline

60. In an incremental search, C-w adds the next word of the line to the
    text searched for, and C-y adds the rest of the line. Neither is
    recorded or written, so both end the search and run as commands. From:
    backlog 47.
61. In an incremental search, readline reads Esc as a prefix when more
    input follows within a moment, so that M-f ends the search and moves
    forward a word. The decoder now hands Esc and f over as two keys, so
    the Esc ends the search and the f is typed. An arrow key arrives as one
    key and works. From: backlog 47.

## Found while every command of readline was added

These come from 2026-10-30, when rline gained the 45 commands of readline
8.3 that it did not have.

68. `keyseq-timeout` is readline's default of 500 in `dump-variables` unless
    an inputrc file sets it, although the decoder waits its own time. The
    two colors of the active region now come from terminfo (D55). From: the
    recordings of `dump-variables`, which the test matches by setting the
    values that the probe's terminal gives.
69. `tty-status` rings the bell. readline sends the terminal's status
    character, which BSD and macOS have and Linux does not, so the probe on
    Linux records nothing. Sending it needs a system call on those systems.
    From: the recording of `tty-status`.
## Decided on 2026-10-30

71. Which of isocline's sequences to read from terminfo (D55, D65). rline
    reads the entry of `$TERM` and takes the two colors of the active region
    from it. No sequence that rline writes has moved yet, because each one
    changes the recorded corpus. D65 proposes which to move. From: D55.
73. A strategy for character widths. go-runewidth and isocline's table
    disagree on 3870 code points. Three kinds are open: the Hangul medial
    vowels and final consonants, the tag characters in a flag, and the
    UTF-16 surrogate halves. tblfmt measures by grapheme cluster with
    go-runewidth's `StringWidth` (tblfmt D39), which gives isocline's answer
    inside a cluster. Ken's pull requests 123 and 124 on go-runewidth change
    none of the three. Ken wants a larger strategy before rline changes.
    From: the eleventh open question, and tblfmt on 2026-10-30.

## Found while the checks were cut to D60

74. The VS Code terminal is in the matrix of D60, and `uitest` cannot open
    it. VS Code has no command line that opens a terminal at a given size
    and runs a program in it. The terminal is xterm.js, so a harness could
    drive xterm.js itself in a browser or in `@xterm/headless` under Node.
    From: D60.
75. No tmux or Windows session is blessed yet, and the blessed logs of the
    other terminals are stale, because the banner of `_examples/sql` changed
    after they were blessed. The format of the log did not change with D54.
    `go run ./uitest -terminal tmux` runs, and its screenshots looked right
    on 2026-10-30. A person looks at the logs and the screenshots, and then
    runs it again with `-update`. The four Windows entries were written
    without a Windows host, and have not run. From: D60.

## Found while terminfo was adopted

76. A string variable that an inputrc file sets reaches the line as the file
    wrote it, so `set active-region-end-color \e[0m` holds a backslash and an
    e, and `dump-variables` prints it as `\\e[0m`. The mode strings of D53 and
    `isearch-terminators` read their escapes. Which other string variables
    readline reads the escapes of is to be recorded from readline 8.3 first.
    From: D55.
77. The visible bell. readline's `bell-style visible` writes the terminal's
    `flash` sequence, and xterm's holds the padding `$<100/>`, which
    github.com/xo/terminfo v1.2.1 does not apply or remove. A function that
    applies it would be a new feature, which terminfo's D14 does not allow.
    Ken has to answer open question 5 in terminfo's docs/PLAN.md first.
    From: D55.
78. An empty `.rline_example_history.<number>` file, mode 0600, was left in
    the root after a `uitest` run, twice on 2026-10-30. The history is saved
    through a temporary file that is renamed over the old one, and a program
    that the terminal kills between the two leaves the temporary file. A
    signal handler that removes it, or a name that the next save cleans up,
    would stop it. From: the `uitest` runs of D54 and D60.

## Found while vi mode was written

64. readline acts on a lone Esc in vi insert mode after keyseq-timeout.
    With readline's defaults, every sequence that starts with Esc decodes
    to one key, so Esc is no prefix there and acts at once. An inputrc that
    binds a sequence such as `\ex` in vi-insert makes Esc a prefix, and
    rline then waits for the next key with no timeout. From: backlog 50.

## Found in isocline's open pull requests

These come from a review on 2026-10-29 of the pull requests that are open on
isocline. Its `main` has no commit after the one that rline was ported from.

55. A CSI u sequence for ctrl and a letter, such as `ESC[97;5u` from kitty,
    decodes as `'a'|ModCtrl` and not as Ctrl-A, so it runs nothing. And
    `readCSI` drops every modifier when Caps Lock or Num Lock adds 64 or 128
    to the modifier number. Both change `testdata/tty.txt`, so they need
    the delta file. From: isocline pull request 44.
56. The history could give a hint, as fish does: the rest of the newest
    entry that starts with what is typed, drawn as rline draws a completion
    hint. It goes with items 9 and 11, and a hint of the program's own would
    need an API. From: isocline pull requests 22 and 42.

## Known and not asked for

These are C functions with no option behind them in rline. The state that
each one sets is already in place. None is built, because nobody asked.

- `ic_enable_history_duplicates`, `ic_enable_completion_preview`,
  `ic_set_tty_esc_delay` and the two prompt marker getters. From: "What the
  C exposes and the port does not" in [PORT.md](PORT.md).
- `ic_enable_color` and `ic_enable_beep`. The options set both when a
  `Session` is made, and nothing changes them afterwards. From: `159465c`.
