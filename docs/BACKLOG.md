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
   kept as a numbered backlog item.
2. Explain why the first pager after startup behaves and later ones do not.
   D48 added `Session.Release`, which gives the terminal to a child process
   and takes it back, and usql/pager uses it. Check usql issue 508 again
   with that pager. From: usql issue 508, and "Two programs reading one
   terminal" in [USQL.md](USQL.md).
3. Let a caller ask which completer and which highlighter a `Session` holds.
   `SetCompleter` and `SetHighlighter` cannot be read back, so a lost
   completer looks like a completer with nothing to offer. From: usql issue
   478, "A regression test waiting for the consumer layer" in
   [TESTING.md](TESTING.md), and the eighth open question.

## Faults of rline's own

4. A focus notification is decoded as a key. The decoder reads `CSI I` as
   Tab or PageUp and `CSI O` as F3. It is a fault whether or not it is what
   the reporters saw. From: "A focus notification is read as a keystroke" in
   [USQL.md](USQL.md), as the likely cause of usql issues 93, 122, 483 and
   490.
5. A password reaches the session log where the terminal has no no-echo
   mode, which is Windows. The fix needs a way to stop the log for a moment.
   From: `c91bacf`, and "A password reaches the session log" in
   [USQL.md](USQL.md). It waits on the sixth and seventh open questions.
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
8. The cursor shape that goes with vi's modes, a bar in insert mode and a
   block in command mode. vi's insert and command modes are in place, and
   the shape still waits on the fourth open question. readline's
   show-mode-in-prompt, which marks the mode in the prompt, is not
   supported either. From: usql issues 236 and 552.
9. History that filters by prefix on the up arrow, and a walk through the
   history by query. Nobody has examined either yet. From: usql issues 72
   and 320.
10. A `context.Context` on `ReadLine`, so that a read that waits can be
    cancelled and the terminal put back. `asyncStop` and `key.EventStop`
    already make a waiting read return, and only a test calls them. From:
    "What usql needs and nothing tests" in [TESTING.md](TESTING.md).
11. Rewrite the history. D10 records that it is going to be rewritten. Three
    things wait for the rewrite. One is the file mode, which is the tenth
    open question. One is an explicit access control list on Windows. One is
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
23. The comment above `history.save` in `history.go`, and the paragraph on
    the history file in "Known departures from the C code" in
    [PORT.md](PORT.md), say that nothing restricts the file mode. The code
    creates the file 0600. Fix whichever one the tenth open question makes
    wrong.
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

31. The comment on `Completion.Add` says that the completion replaces the
    prefix that was handed to the completer. A completer given to rline
    directly is handed everything before the cursor, and `Add` sets
    `DeleteBefore` to 0, so it deletes nothing. Measured: typing `sel`, then
    Tab with `Add("select")`, gives `selselect`. Either the comment or the
    code is wrong, and which one is Ken's choice. Until then, a completer
    uses `AddCandidate` with `DeleteBefore`, as `_examples/sql` does. From:
    a question from the usql session on 2026-10-01.
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
    Ken decides it. From: the usql session, 2026-10-01.
34. Text that the next `ReadLine` starts with. usql's `\e` opens an editor
    on the query buffer and gives the edited text back for more editing.
    `ReadLine` takes only the prompt text, so the text cannot be placed in
    the line. This needs new API, and Ken decides it. From: the usql
    session, 2026-10-01.
35. A way to change `WithContinue` after the `Session` is made. There is a
    `SetCompleter` and a `SetHighlighter`, and no setter for the function
    that decides whether Enter continues. So a read such as usql's
    `\prompt` keeps the continuation rule of SQL, unless the function asks
    usql which mode it is in. From: the usql session, 2026-10-01.

## Found in phase 1 of the readline work

37. `operate-and-get-next` (Ctrl-O) accepts the line and starts the next one
    at the history entry after it. It needs the `Session`, which reads the
    next line. From: phase 1.
38. `execute-named-command` (`\ex`) reads the name of a command in a prompt
    of its own, as incremental search does, so it comes with search in
    phase 4. From: phase 1.
39. `yank-nth-arg` and `yank-last-arg` split an earlier line at spaces.
    readline splits it as the shell would, so a quoted word with a space in
    it is one word there. No case records a quoted word yet. From: phase 1.
40. `transpose-chars` with a numeric argument drags the character more than
    one place in readline. Only the count of one is written and recorded.
    From: phase 1.

## Found from usql's use

42. A `Session` changes the bytes a program writes through it, even when the
    output is a pipe or a file. `term.appendBuf` drops the control bytes 0 to
    6 and 14 to 26, and with color off, as it is on a pipe, `appendEsc` drops
    every SGR sequence, so `a\e[31mred\e[0mb` comes out as `aredb`. The rule
    is isocline's `term_append_buf`, kept by D4, and it protects a terminal.
    usql writes query results through its `Prompt`, so a value that holds
    these bytes changes in every output format. Measured on 2026-10-22,
    through `os.Pipe`. The fix is a decision for Ken: pass the bytes through
    unchanged when the output is not a terminal, add a writer of raw bytes,
    or have usql write to `os.Stdout` itself. From: usql issue W63 in usql's
    backlog, reported by the usql session.

## Decided for readline and not yet tracked

These come from D41 to D46. They had no item until 2026-10-29, when Ken
asked for the whole list of what readline support still needs.

48. The commands of an application, which a program registers by name so
    that an inputrc file can bind them, as `rl_add_defun` does. A command
    needs the line, the cursor and a way to change them, and the shape is a
    decision for Ken. The reading of the file is done (D50). From: D39,
    D41, D43.

## Found while the keys moved to readline

52. Nothing tests that a command that can do nothing rings the bell.
    `runEditLoop` calls `term.bell` when `readline.Line.Bells` goes up, and
    `term.bell` writes to `os.Stderr` itself, so a test cannot see it.
    Breaking the call fails no test. From: backlog 41.

## Found while completion moved to readline

57. The decoder reads `ESC ?` and `ESC o` as the start of an SS3 sequence,
    as isocline does for Eterm, and waits for the byte after them. So M-?,
    which is readline's `possible-completions`, typed fast with the next
    key, joins the two into one sequence. A person who types leaves a gap
    that ends the wait, and a terminal that sends meta as an escape sends
    the two bytes alone. Whether rline still needs the Eterm form is open.
    From: `TestSessionMatchesReadline`, which pauses between keys for this.
58. `Completion.Add` gives a completion no suffix, so a completer that uses
    it gets no space after a single completion, which readline adds by
    default. `AddCandidate` takes a `Suffix`, and file name completion puts
    a space after a file. Whether `Add` adds a space is Ken's choice, and it
    goes with item 31. From: backlog 46.

## Found while search moved to readline

59. The non-incremental searches, `non-incremental-reverse-search-history`
    and `non-incremental-forward-search-history` on M-p and M-n, read the
    text to search for in a prompt of their own. They ring the bell until
    then, as `execute-named-command` does (item 38). From: backlog 47.
60. In an incremental search, C-w adds the next word of the line to the
    text searched for, and C-y adds the rest of the line. Neither is
    recorded or written, so both end the search and run as commands. From:
    backlog 47.
61. In an incremental search, readline reads Esc as a prefix when more
    input follows within a moment, so that M-f ends the search and moves
    forward a word. The decoder now hands Esc and f over as two keys, so
    the Esc ends the search and the f is typed. An arrow key arrives as one
    key and works. From: backlog 47.

## Found while vi mode was written

63. The vi commands that are not written yet ring the bell: `/` and `?`
    with `n` and `N` (vi-search, vi-search-again), `G` (vi-fetch-history),
    `_` (vi-yank-arg), `m` and the backquote (vi-set-mark, vi-goto-mark),
    `*`, `=` and the backslash (vi-complete), and `~` as vi-tilde-expand.
    `vi-eof-maybe` on a line that is not empty is not recorded. Each needs
    a case in the probe first. From: backlog 50.
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
