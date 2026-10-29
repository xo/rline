// Reading one line: drawing it, the hint, resizing, the key dispatch, the
// loop, and the help screen it can open.
//
// Ported from isocline/src/editline.c and editline_help.c.

package rline

import (
	"fmt"
	"time"

	"github.com/xo/rline/internal/editor"
	"github.com/xo/rline/internal/readline"
	"github.com/xo/rline/internal/text"
	"github.com/xo/rline/key"

	"github.com/xo/rline/ansi"
)

// --------------------------------------------------------------------------
// editline.go

// Drawing the line being edited.
//
// The editor redraws the whole line after every change, rather than tracking
// what moved. Output is collected while it draws and written in one go, so the
// cursor does not flicker.
//
// Ported from isocline/src/editline.c.

// env holds everything the editor needs that is not the line itself.
//
// This is ic_env_t, minus the fields that belong to starting up and shutting
// down, which the public API owns.
type env struct {
	// Where input comes from and output goes.
	term *term
	keys *keyDecoder
	bb   *bbCode

	// What the editor draws from.
	history     *history
	completions *completions

	// The prompt, and the one used for the lines after the first.
	promptMarker  string
	cpromptMarker string

	// highlighter marks up the line, and may be nil.
	highlighter Highlighter

	// keymap turns keys into commands. A nil keymap is readline's default
	// emacs keymap.
	keymap *readline.Keymap

	// lastSearch is what the last incremental search looked for, which a
	// search that starts with nothing looks for again.
	lastSearch string

	// vars are the variables of readline that an inputrc file set, and
	// notices what in the file rline does not support.
	vars    map[string]any
	notices []string

	// The keymaps of vi's insert and command modes, nil for readline's
	// defaults, and whether the next line is read in vi mode. A line that
	// switches mode leaves the next one in that mode, as readline does.
	viInsert, viCommand *readline.Keymap
	viMode              bool

	// isIncomplete reports whether a line is unfinished, so that Enter starts
	// another row rather than handing the line back. It may be nil, which
	// makes Enter always finish, as the C does.
	isIncomplete func(string) bool

	// Editing settings, shared with the operations in editor.go.
	opts editor.EditOptions

	// Settings that turn parts of the editor off.
	multilineIndent bool
	braceMatching   bool
	hints           bool
	inlineHelp      bool
	multiline       bool

	// completeAutoTab keeps completing while there is only one answer.
	completeAutoTab bool

	// completionMenu binds Tab to rline-menu-complete, and menu is the menu
	// while it is open (D51). menuAutoTab says that the EventAutoTab pushed
	// back came from the menu's way of completing, which opens the menu again
	// when there are several answers.
	completionMenu bool
	menu           *completionMenu
	menuAutoTab    bool

	// completePreview shows in the line what picking the selected entry would
	// do, and takes it straight back out again. It is on by default. The C
	// names its flag for what it turns off; this does not, so that every
	// switch in this struct reads the same way.
	completePreview bool

	// hintDelay is how long to wait before showing a hint. Zero shows it at
	// once.
	hintDelay time.Duration
}

// promptWidth returns how wide the prompt is on the first row, and on the rows
// after it.
//
// The continuation prompt is padded out to line up under the first one, unless
// that is turned off or it is already wider.
func (ev *env) promptWidth(e *editor.Editor, inExtra bool) (int, int) {
	if inExtra {
		return 0, 0
	}
	textw := ev.bb.columnWidth(e.PromptText)
	markerw := ev.bb.columnWidth(ev.promptMarker)
	cmarkerw := ev.bb.columnWidth(ev.cpromptMarker)
	promptw := markerw + textw
	cpromptw := promptw
	if !ev.multilineIndent || promptw < cmarkerw {
		cpromptw = cmarkerw
	}
	return promptw, cpromptw
}

// rowCol returns how many rows the line takes and where the cursor sits.
func (ev *env) rowCol(e *editor.Editor) (int, text.RowCol) {
	promptw, cpromptw := ev.promptWidth(e, false)
	return e.Input.RowColAtPos(e.TermW, promptw, cpromptw, e.Pos)
}

// posIsAtRowEnd reports whether the cursor is at the end of a screen row.
func (ev *env) posIsAtRowEnd(e *editor.Editor) bool {
	_, rc := ev.rowCol(e)
	return rc.LastOnRow
}

// writePrompt draws the prompt in front of one row.
func (ev *env) writePrompt(e *editor.Editor, row int, inExtra bool) {
	if inExtra {
		return
	}
	ev.bb.styleOpen("rline-prompt")
	switch {
	case row == 0:
		ev.bb.print(e.PromptText)
	case ev.multilineIndent:
		// Pad the continuation marker out so the text lines up under the
		// first row.
		textw := ev.bb.columnWidth(e.PromptText)
		markerw := ev.bb.columnWidth(ev.promptMarker)
		cmarkerw := ev.bb.columnWidth(ev.cpromptMarker)
		if cmarkerw < markerw+textw {
			ev.term.writeRepeat(" ", markerw+textw-cmarkerw)
		}
	}
	marker := ev.cpromptMarker
	if row == 0 {
		marker = ev.promptMarker
	}
	ev.bb.print(marker)
	ev.bb.styleClose("")
}

// refreshRows draws the rows of text between firstRow and lastRow.
func (ev *env) refreshRows(e *editor.Editor, input *text.Buffer, attrs *ansi.AttrBuf,
	promptw, cpromptw int, inExtra bool, firstRow, lastRow int,
) {
	input.ForEachRow(e.TermW, promptw, cpromptw, func(s []byte, row, rowStart, rowLen, _ int, isWrap bool) bool {
		if row < firstRow {
			return false
		}
		if row > lastRow {
			return true
		}
		ev.writePrompt(e, row, inExtra)
		text := string(s[rowStart : rowStart+rowLen])
		if attrs == nil || (ev.highlighter == nil && !ev.braceMatching) {
			ev.term.write(text)
		} else {
			all := attrs.Extend(rowStart + rowLen)
			ev.term.writeFormatted(text, all[rowStart:rowStart+rowLen])
		}
		if row < lastRow {
			if isWrap && ev.ttyIsUTF8() {
				// A row that ended because it filled the terminal is marked,
				// so that it reads differently from a row the user ended.
				ev.bb.print(wrapMark)
			}
			ev.term.clearToEndOfLine()
			ev.term.writeln("")
		} else {
			ev.term.clearToEndOfLine()
		}
		return row >= lastRow
	})
}

// ttyIsUTF8 reports whether the terminal reads UTF-8. No terminal at all
// counts as UTF-8, which is what the C answers for a null one.
func (ev *env) ttyIsUTF8() bool {
	return ev.keys == nil || ev.keys.isUTF8
}

// refresh draws the whole line, the hint inside it and anything shown below
// it, and puts the cursor back where it belongs.
func (ev *env) refresh(e *editor.Editor) {
	promptw, cpromptw := ev.promptWidth(e, false)

	runHighlight(ev.bb, e.Input.String(), &e.Attrs, ev.highlighter)
	if ev.braceMatching {
		highlightMatchBraces(e.Input.String(), &e.Attrs, e.Pos, ev.opts.MatchPairs,
			ev.bb.style("rline-bracematch"), ev.bb.style("rline-error"))
	}

	// The hint goes into the line itself while it is drawn, and comes back out
	// at the end, so that everything below measures it as part of the text.
	if e.Hint.Len() > 0 {
		e.Attrs.InsertAt(e.Pos, e.Hint.Len(), ev.bb.style("rline-hint"))
		e.Input.InsertAt(e.Hint.String(), e.Pos)
	}

	// Anything shown below the line, such as a completion menu.
	var extra *text.Buffer
	if e.Extra.Length() > 0 {
		extra = &text.Buffer{}
		if e.HintHelp.Len() > 0 {
			ev.bb.appendTo(e.HintHelp.String(), extra, &e.AttrsExtra)
		}
		ev.bb.appendTo(e.Extra.String(), extra, &e.AttrsExtra)
	}

	rowsInput, rc := e.Input.RowColAtPos(e.TermW, promptw, cpromptw, e.Pos)
	rowsExtra := 0
	if extra != nil {
		rowsExtra, _ = extra.RowColAtPos(e.TermW, 0, 0, 0)
	}
	rows := rowsInput + rowsExtra

	// Draw at most a screen full, keeping the cursor in view.
	termh := ev.term.height
	firstRow, lastRow := 0, rows-1
	if rows > termh {
		firstRow = max(rc.Row-termh+1, 0)
		lastRow = firstRow + termh - 1
	}

	was := ev.term.setBufferMode(buffered)

	// Go back to the first row of what was drawn last time.
	ev.term.startOfLine()
	ev.term.up(min(e.CurRow, termh-1))

	ev.refreshRows(e, &e.Input, &e.Attrs, promptw, cpromptw, false, firstRow, lastRow)
	if rowsExtra > 0 {
		ev.refreshRows(e, extra, &e.AttrsExtra, 0, 0, true,
			max(firstRow-rowsInput, 0), lastRow-rowsInput)
	}

	// Wipe any rows that were used last time and are not needed now.
	drawn := lastRow - firstRow + 1
	if drawn < termh && rows < e.CurRows {
		for clear := e.CurRows - rows; drawn < termh && clear > 0; clear-- {
			drawn++
			ev.term.writeln("")
			ev.term.clearLine()
		}
	}

	// Put the cursor back where the text says it should be.
	ev.term.startOfLine()
	ev.term.up(firstRow + drawn - 1 - rc.Row)
	pw := cpromptw
	if rc.Row == 0 {
		pw = promptw
	}
	ev.term.right(rc.Col + pw)
	ev.term.flush()
	ev.term.setBufferMode(was)

	// Take the hint back out, so the line is what the user typed again.
	e.Input.DeleteAt(e.Pos, e.Hint.Len())
	e.Extra.DeleteAt(0, e.HintHelp.Len())
	e.Attrs.Clear()
	e.AttrsExtra.Clear()

	e.CurRows = rows
	e.CurRow = rc.Row
}

// clear wipes the rows the line is drawn on.
func (ev *env) clear(e *editor.Editor) {
	ev.term.attrReset()
	ev.term.up(e.CurRow)
	for range e.CurRows {
		ev.term.clearLine()
		ev.term.writeln("")
	}
	ev.term.up(e.CurRows - e.CurRow)
}

// clearScreen wipes the screen and draws the line again.
func (ev *env) clearScreen(e *editor.Editor) {
	rows := e.CurRows
	e.CurRows = ev.term.height - 1
	ev.clear(e)
	e.CurRows = rows
	ev.refresh(e)
}

// --------------------------------------------------------------------------
// editlineloop.go

// Reading a line: the hint, resizing, the key dispatch and the loop.
//
// Ported from isocline/src/editline.c.

// appendHintHelp puts the help text that goes with a hint below the line, or
// clears it when there is none.
//
// It is a function rather than a method on the editor because the markup it
// writes is rline's vocabulary, which the editor does not share.
func appendHintHelp(e *editor.Editor, help string) {
	e.HintHelp.Reset()
	if help == "" {
		return
	}
	e.HintHelp.WriteString("[rline-info]")
	e.HintHelp.WriteString(help)
	e.HintHelp.WriteString("[/rline-info]\n")
}

// refreshHint draws the line and works out the hint to show inside it.
//
// A hint is the rest of the only completion that fits. When more than one
// fits there is nothing to hint at.
func (ev *env) refreshHint(e *editor.Editor) {
	if !ev.hints || ev.hintDelay > 0 {
		// Draw without the hint first, so the line appears at once and the
		// hint follows when it is ready.
		ev.refresh(e)
		if !ev.hints {
			return
		}
	}
	// Asking for two answers is enough: one means there is a hint, and more
	// than one means there is not.
	if ev.completions.generate(e.Input.String(), e.Pos, 2) != 1 {
		if ev.hintDelay <= 0 {
			ev.refresh(e)
		}
		return
	}
	hint, help, ok := ev.completions.hintAt(0)
	if ok {
		e.Hint.Reset()
		e.Hint.WriteString(hint)
		appendHintHelp(e, help)
		if ev.completeAutoTab {
			ev.extendHint(e, hint)
		}
	}
	if ev.hintDelay <= 0 {
		ev.refresh(e)
	}
}

// extendHint keeps completing past the hint while each step has exactly one
// answer, so that a chain of forced completions shows as one hint.
func (ev *env) extendHint(e *editor.Editor, hint string) {
	var sb text.Buffer
	sb.Replace(e.Input.String())
	pos := e.Pos
	for {
		next := sb.InsertAt(hint, pos)
		if next <= pos {
			return
		}
		pos = next
		if ev.completions.generate(sb.String(), pos, 2) != 1 {
			return
		}
		extra, help, ok := ev.completions.hintAt(0)
		if !ok {
			return
		}
		appendHintHelp(e, help)
		e.Hint.WriteString(extra)
		hint = extra
	}
}

// resize works out the new layout after the terminal changed size, and
// reports whether it did change.
func (ev *env) resize(e *editor.Editor) bool {
	ev.term.updateDim()
	newW := ev.term.width
	if e.TermW == newW {
		return false
	}
	promptw, cpromptw := ev.promptWidth(e, false)
	// The hint counts as part of the line while the rows are measured.
	e.Input.InsertAt(e.Hint.String(), e.Pos)
	var extra *text.Buffer
	if e.Extra.Length() > 0 {
		extra = &text.Buffer{}
		if e.HintHelp.Len() > 0 {
			ev.bb.appendTo(e.HintHelp.String(), extra, nil)
		}
		ev.bb.appendTo(e.Extra.String(), extra, nil)
	}
	rowsInput, rc := e.Input.WrappedRowColAtPos(e.TermW, newW, promptw, cpromptw, e.Pos)
	rowsExtra := 0
	if extra != nil {
		rowsExtra, _ = extra.WrappedRowColAtPos(e.TermW, newW, 0, 0, 0)
	}
	rows := rowsInput + rowsExtra
	e.CurRow = rc.Row
	if rows > e.CurRows {
		e.CurRows = rows
	}
	e.TermW = newW
	ev.refresh(e)
	e.Input.DeleteAt(e.Pos, e.Hint.Len())
	return true
}

// readKey waits for the next key, showing the hint if one is pending and the
// user pauses long enough.
func (ev *env) readKey(e *editor.Editor) key.Code {
	if ev.hintDelay <= 0 || e.Hint.Len() == 0 {
		return ev.keys.read()
	}
	c, ok := ev.keys.readTimeout(ev.hintDelay)
	if ok {
		// Something was typed before the delay ran out, so the hint is stale.
		e.Hint.Reset()
		e.HintHelp.Reset()
		return c
	}
	if e.Hint.Len() > 0 {
		ev.refresh(e)
	}
	return ev.keys.read()
}

// readLine puts the terminal into raw mode, reads one line, and puts the
// terminal back.
//
// Raw mode is what stops the line discipline doing the editing itself. Without
// it the Enter key arrives as a line feed rather than a carriage return,
// because the terminal rewrites it on the way in, and the editor never sees
// the key that ends a line.
func (ev *env) readLine(promptText string) (string, bool, error) {
	if err := ev.keys.startRaw(); err != nil {
		return "", false, fmt.Errorf("switching the terminal to raw mode: %w", err)
	}
	ev.term.startRaw()
	line, ok, c := ev.editLine(promptText)
	ev.term.endRaw(false)
	ev.keys.endRaw()
	// The finished line stays on screen and the cursor moves below it.
	ev.term.writeln("")
	ev.term.flush()
	if c == key.CtrlC {
		// The user abandoned the line rather than finishing it. See
		// ErrInterrupted for why this is not what the C answers. Ctrl-G is
		// readline's abort, and no longer gives the line up (D45).
		return "", false, ErrInterrupted
	}
	return line, ok, nil
}

// runEditLoop reads keys until the line is finished, and returns the key that
// finished it: Enter for a line, Ctrl-D for the end of the input, Ctrl-C for
// a line given up, and EventStop for a read that was stopped.
//
// Each key goes through the keymap to a command of GNU readline, which a
// readline.Line runs (D44). The commands that draw, complete or search are
// rline's, and ev.command runs them. The line is copied into the editor after
// each key, and the editor draws it.
//
// This is the loop itself, apart from the terminal being put into raw mode and
// the history being kept, so that a test can drive it over a fixed set of keys
// and look at the line afterwards.
func (ev *env) runEditLoop(e *editor.Editor) key.Code {
	if ev.keymap == nil || ev.viInsert == nil {
		km, vi, vc := ev.defaultKeymaps()
		if ev.keymap == nil {
			ev.keymap = km
		}
		if ev.viInsert == nil {
			ev.viInsert, ev.viCommand = vi, vc
		}
	}
	ev.menu, ev.menuAutoTab = nil, false
	l := readline.New(ev.keymap, ev.readlineHistory())
	l.SetKeymaps(ev.keymap, ev.viInsert, ev.viCommand)
	l.SetHandler(func(name string, c key.Code) bool { return ev.command(e, l, name, c) })
	l.SetCompleter(ev.candidates)
	l.Configure(ev.vars)
	l.SetEditingMode(ev.viMode)
	l.SetSearchString(ev.lastSearch)
	defer func() {
		ev.lastSearch = l.SearchString()
		ev.viMode = l.Mode() != "emacs"
	}()
	bells := 0
	asked := false
	// A prompt of the line's own, such as that of incremental search, is
	// drawn in place of the program's (D45).
	promptText, marker, shownPrompt := e.PromptText, ev.promptMarker, ""
	defer func() { e.PromptText, ev.promptMarker = promptText, marker }()
	for {
		ev.term.flush()
		c := ev.readKey(e)
		if ev.keys.resizeEvent() {
			ev.resize(e)
		}
		// The hint is dropped after a possible resize, so that the resize
		// measures the rows the hint was drawn on.
		hadHint := e.Hint.Len() > 0
		e.Hint.Reset()
		e.HintHelp.Reset()
		// A command of rline's copies the line into the editor itself, so
		// what changed is measured against the line before the key.
		before, beforePoint := l.Text(), l.Point()
		switch {
		case ev.menu != nil:
			// The open menu takes every key, as the C's menu reads every key
			// itself, and pushes back a key that closes it without being
			// used, so that it is read again here (D51).
			ev.menuKey(e, c)
			l.Edit(e.Input.String(), e.Pos)
			continue
		case c == key.CtrlC:
			// Ctrl-C gives the line up (D13). In readline the terminal turns
			// it into a signal, and it is no key. The line is emptied, as
			// isocline empties it, so that the history does not keep it.
			e.Input.Clear()
			e.Pos = 0
			return c
		case c == key.EventStop:
			return c
		case c == key.None:
			continue
		case c == key.EventResize:
			ev.resize(e)
			continue
		case c == key.EventAutoTab:
			if ev.menuAutoTab {
				ev.menuAutoTab = false
				ev.generateCompletions(e, true)
			} else {
				ev.acceptHint(e)
			}
			l.Edit(e.Input.String(), e.Pos)
			continue
		case c == key.Right && hadHint:
			// Right takes a hint that is shown, and moves the cursor when
			// none is (D46).
			l.Run("rline-accept-hint", c)
		default:
			l.Key(c)
		}
		showLine(e, l)
		changed := l.Text() != before || l.Point() != beforePoint
		// A question, such as whether to list many completions, is shown
		// below the line while the line waits for the answer.
		if q := l.Asking(); q != "" || asked {
			e.Extra.Clear()
			if q != "" {
				e.Extra.AppendString("[rline-info][!pre]" + q + "[/pre][/]")
			}
			asked = q != ""
			changed = true
		}
		if list := l.Listing(); len(list) > 0 {
			ev.showListing(e, list)
		}
		if p := l.Prompt(); p != shownPrompt {
			shownPrompt = p
			e.PromptText, ev.promptMarker = promptText, marker
			if p != "" {
				e.PromptText, ev.promptMarker = "[!pre]"+p+"[/pre]", ""
			}
			changed = true
		}
		switch {
		case l.Last() == "self-insert" && changed:
			ev.refreshHint(e)
		case changed, hadHint:
			ev.refresh(e)
		}
		if n := l.Bells(); n > bells {
			bells = n
			ev.term.bell()
		}
		if l.Done() {
			if l.EOF() {
				return key.CtrlD
			}
			return key.Enter
		}
	}
}

// editLine reads one line from the terminal. It returns the line, false when
// the user ended the input rather than finishing a line, and the key that
// finished it, which is how the caller tells an abandoned line from an empty
// one.
func (ev *env) editLine(promptText string) (string, bool, key.Code) {
	e := &editor.Editor{
		Opts:       ev.opts,
		TermW:      ev.term.width,
		CurRows:    1,
		PromptText: promptText,
	}
	ev.writePrompt(e, 0, false)

	// The line being typed is always the newest history entry, so that walking
	// back and returning lands on it again.
	ev.history.push("")

	c := ev.runEditLoop(e)

	e.CursorToEnd()
	// One last draw, without brace matching, so no brace is left highlighted
	// on the finished line.
	was := ev.braceMatching
	ev.braceMatching = false
	ev.refresh(e)
	ev.braceMatching = was

	line, ok := e.Input.String(), true
	if (c == key.CtrlD && e.Input.Length() == 0) || c == key.EventStop {
		line, ok = "", false
	} else if !ev.ttyIsUTF8() {
		line = string(e.Input.DecodeFromLocale())
	}

	ev.history.update(e.Input.String())
	if !ok || e.Input.Length() <= 1 {
		// Neither an empty line nor a single character is worth keeping.
		ev.history.removeLast()
	}
	_ = ev.history.save()
	return line, ok, c
}

// --------------------------------------------------------------------------
// editlinehelp.go

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
	return "[rline-info]" +
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
		"[/ansi-lightgray][/rline-info]\n"
}

// helpLines returns the table as the lines it is printed as, with the markup
// still in. Keeping this apart from the printing is what lets the whole
// table be checked against the C without a terminal.
func helpLines() []string {
	rows := helpRows()
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.keys == "" {
			lines = append(lines, fmt.Sprintf("[rline-info]%s[/]\n", row.text))
			continue
		}
		separator := ": "
		if row.text == "" {
			separator = ""
		}
		lines = append(lines, fmt.Sprintf("  [rline-emphasis]%-*s[/][ansi-lightgray]%s%s[/]\n",
			helpKeyWidth, row.keys, separator, row.text))
	}
	return lines
}

// showHelp draws the list of keys below the line.
//
// The line is taken down first and drawn again afterwards, because the help
// is printed where the line was and the editor has to be told the screen
// moved under it.
func (ev *env) showHelp(e *editor.Editor) {
	ev.clear(e)
	ev.bb.println(helpBanner())
	for _, line := range helpLines() {
		ev.bb.print(line)
	}
	// The help has scrolled the line off where the editor thought it was, so
	// the next redraw has to start afresh rather than move a cursor that is
	// no longer there.
	e.CurRows = 0
	e.CurRow = 0
	ev.refresh(e)
}
