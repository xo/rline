package rline

import "fmt"

// The help screen: the list of keys the editor understands, drawn below the
// line when F1 is pressed.
//
// Three of the rows name a different key on macOS, and those live in
// helpkeys_darwin.go and helpkeys_other.go, because the C chooses them at
// compile time.
//
// Ported from isocline/src/editline_help.c.

// helpRow is one line of the help. An empty keys field makes the text a
// heading rather than a description.
type helpRow struct {
	keys string
	text string
}

// helpKeyWidth is how wide the key column is. Anything longer pushes its
// description along rather than being cut.
const helpKeyWidth = 13

// helpRows is the whole table, in the order it is drawn.
func helpRows() []helpRow {
	rows := []helpRow{
		{"", "Navigation:"},
		{"left,^b", "go one character to the left"},
		{"right,^f", "go one character to the right"},
		{"up", "go one row up, or back in the history"},
		{"down", "go one row down, or forward in the history"},
		{helpPrevWord, "go to the start of the previous word"},
		// The missing "of" is in the C.
		{helpNextWord, "go to the end the current word"},
		{"home,^a", "go to the start of the current line"},
		{"end,^e", "go to the end of the current line"},
		{"pgup,^home", "go to the start of the current input"},
		{"pgdn,^end", "go to the end of the current input"},
		{"alt-m", "jump to matching brace"},
		{"^p", "go back in the history"},
		{"^n", "go forward in the history"},
		{"^r,^s", "search the history starting with the current word"},
		{"", ""},

		{"", "Deletion:"},
		{"del,^d", "delete the current character"},
		{"backsp,^h", "delete the previous character"},
		{"^w", "delete to preceding white space"},
		{"alt-backsp", "delete to the start of the current word"},
		{"alt-d", "delete to the end of the current word"},
		{"^u", "delete to the start of the current line"},
		{"^k", "delete to the end of the current line"},
		{"esc", "delete the current input, or done with empty input"},
		{"", ""},

		{"", "Editing:"},
		{"enter", "accept current input"},
	}
	rows = append(rows, helpNewlineRows...)
	return append(rows,
		helpRow{"^l", "clear screen"},
		helpRow{"^t", "swap with previous character (move character backward)"},
		helpRow{"^z,^_", "undo"},
		helpRow{"^y", "redo"},
		helpRow{"tab", "try to complete the current input"},
		helpRow{"", ""},

		helpRow{"", "In the completion menu:"},
		helpRow{"enter,left", "use the currently selected completion"},
		helpRow{"1 - 9", "use completion N from the menu"},
		helpRow{"tab,down", "select the next completion"},
		helpRow{"shift-tab,up", "select the previous completion"},
		helpRow{"esc", "exit menu without completing"},
		helpRow{"pgdn,^j", "show all further possible completions"},
		helpRow{"", ""},

		helpRow{"", "In incremental history search:"},
		helpRow{"enter", "use the currently found history entry"},
		helpRow{"backsp,^z", "go back to the previous match (undo)"},
		helpRow{"tab,^r", "find the next match"},
		helpRow{"shift-tab,^s", "find an earlier match"},
		helpRow{"esc", "exit search"},
		// A space rather than nothing, so this draws a key row with no
		// description rather than a heading. That is what the C has.
		helpRow{" ", ""},
	)
}

// helpBanner is what is printed above the table: what this is, and a drawing
// of which key reaches which part of the line.
func helpBanner() string {
	return "[ic-info]" +
		"Isocline v1.1, copyright (c) 2021-2026 Daan Leijen.\n" +
		"This is free software; you can redistribute it and/or\n" +
		"modify it under the terms of the MIT License.\n" +
		"See <[url]https://github.com/daanx/isocline[/url]> for further information.\n" +
		"We use ^<key> as a shorthand for ctrl-<key>.\n" +
		"\n" +
		"Overview:\n" +
		"\n[ansi-lightgray]" +
		"       home,ctrl-a      cursor     end,ctrl-e\n" +
		"         ┌────────────────┼───────────────┐    (navigate)\n" +
		helpOverviewArrows +
		"         │        ┌───────┼──────┐        │    ctrl-r   : search history\n" +
		"         ▼        ▼       ▼      ▼        ▼    tab      : complete word\n" +
		"  prompt> [ansi-darkgray]it's the quintessential language[/]     shift-tab: insert new line\n" +
		"         ▲        ▲              ▲        ▲    esc      : delete input, done\n" +
		"         │        └──────────────┘        │    ctrl-z   : undo\n" +
		"         │   alt-backsp        alt-d      │\n" +
		"         └────────────────────────────────┘    (delete)\n" +
		"       ctrl-u                          ctrl-k\n" +
		"[/ansi-lightgray][/ic-info]\n"
}

// helpLines returns the table as the lines it is printed as, with the markup
// still in. Keeping this apart from the printing is what lets the whole
// table be checked against the C without a terminal.
func helpLines() []string {
	rows := helpRows()
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.keys == "" {
			lines = append(lines, fmt.Sprintf("[ic-info]%s[/]\n", row.text))
			continue
		}
		separator := ": "
		if row.text == "" {
			separator = ""
		}
		lines = append(lines, fmt.Sprintf("  [ic-emphasis]%-*s[/][ansi-lightgray]%s%s[/]\n",
			helpKeyWidth, row.keys, separator, row.text))
	}
	return lines
}

// showHelp draws the list of keys below the line.
//
// The line is taken down first and drawn again afterwards, because the help
// is printed where the line was and the editor has to be told the screen
// moved under it.
func (ev *env) showHelp(e *editor) {
	ev.clear(e)
	ev.bb.println(helpBanner())
	for _, line := range helpLines() {
		ev.bb.print(line)
	}
	// The help has scrolled the line off where the editor thought it was, so
	// the next redraw has to start afresh rather than move a cursor that is
	// no longer there.
	e.curRows = 0
	e.curRow = 0
	ev.refresh(e)
}
