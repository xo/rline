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
2. Give the terminal to a child process and take it back. Two processes that
   read one terminal share the keys, and the other reader decides the split.
   The reporter's finding that the first pager after startup behaves is not
   yet explained. From: usql issue 508, and "Two programs reading one
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

7. Bracketed paste, so that a paste does not trigger completion, and a
   quoted insert on Ctrl-V, so that a tab can be typed. `key.CtrlV` exists
   and nothing implements it. Neither is in isocline. From: usql issue 472.
8. vi bindings. There is no notion of a mode in the key dispatch. It waits on
   the fifth open question, together with the cursor shape and inputrc. From:
   usql issues 236 and 552.
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
    `example/transit/main.go` does that. From: the transit example, which
    Ken asked for as the test of the transit API.
28. rline sorts the completions before it draws the menu, so a completer
    cannot put the keywords before the other tokens. From: the transit
    example.
29. No test in `example/transit` holds the rule that a node with the same
    range as the node before it keeps the first style. No sample in seven
    languages had two such nodes with different styles, so breaking the rule
    changed nothing. From: the transit example.

## Known and not asked for

These are C functions with no option behind them in rline. The state that
each one sets is already in place. None is built, because nobody asked.

- `ic_enable_history_duplicates`, `ic_enable_completion_preview`,
  `ic_set_tty_esc_delay` and the two prompt marker getters. From: "What the
  C exposes and the port does not" in [PORT.md](PORT.md).
- `ic_enable_color` and `ic_enable_beep`. The options set both when a
  `Session` is made, and nothing changes them afterwards. From: `159465c`.
