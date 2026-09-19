// Reading one line: drawing it, the hint, resizing, the key dispatch, the
// loop, and the help screen it can open.
//
// Ported from isocline/src/editline.c and editline_help.c.

package rline

import (
	"fmt"
	"time"

	"github.com/xo/rline/key"
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
	tty  *tty
	bb   *bbCode

	// What the editor draws from.
	history     *history
	completions *completions

	// The prompt, and the one used for the lines after the first.
	promptMarker  string
	cpromptMarker string

	// highlighter marks up the line, and may be nil.
	highlighter Highlighter

	// isIncomplete reports whether a line is unfinished, so that Enter starts
	// another row rather than handing the line back. It may be nil, which
	// makes Enter always finish, as the C does.
	isIncomplete func(string) bool

	// Editing settings, shared with the operations in editor.go.
	opts editOptions

	// Settings that turn parts of the editor off.
	noMultilineIndent bool
	noHighlight       bool
	noBraceMatch      bool
	noHint            bool
	noHelp            bool
	singlelineOnly    bool

	// completeAutoTab keeps completing while there is only one answer.
	completeAutoTab bool

	// completeNoPreview stops the completion menu from showing what picking
	// the selected entry would do. It is named for what it turns off because
	// the C is, and because the preview is what happens by default.
	completeNoPreview bool

	// hintDelay is how long to wait before showing a hint. Zero shows it at
	// once.
	hintDelay time.Duration
}

// promptWidth returns how wide the prompt is on the first row, and on the rows
// after it.
//
// The continuation prompt is padded out to line up under the first one, unless
// that is turned off or it is already wider.
func (ev *env) promptWidth(e *editor, inExtra bool) (int, int) {
	if inExtra {
		return 0, 0
	}
	textw := ev.bb.columnWidth(e.promptText)
	markerw := ev.bb.columnWidth(ev.promptMarker)
	cmarkerw := ev.bb.columnWidth(ev.cpromptMarker)
	promptw := markerw + textw
	cpromptw := promptw
	if ev.noMultilineIndent || promptw < cmarkerw {
		cpromptw = cmarkerw
	}
	return promptw, cpromptw
}

// rowCol returns how many rows the line takes and where the cursor sits.
func (ev *env) rowCol(e *editor) (int, rowCol) {
	promptw, cpromptw := ev.promptWidth(e, false)
	return e.input.rowColAtPos(e.termW, promptw, cpromptw, e.pos)
}

// setPosAtRowCol moves the cursor to a row and column on screen.
func (ev *env) setPosAtRowCol(e *editor, row, col int) bool {
	promptw, cpromptw := ev.promptWidth(e, false)
	pos := e.input.posAtRowCol(e.termW, promptw, cpromptw, row, col)
	if pos < 0 {
		return false
	}
	e.pos = pos
	return true
}

// posIsAtRowEnd reports whether the cursor is at the end of a screen row.
func (ev *env) posIsAtRowEnd(e *editor) bool {
	_, rc := ev.rowCol(e)
	return rc.lastOnRow
}

// writePrompt draws the prompt in front of one row.
func (ev *env) writePrompt(e *editor, row int, inExtra bool) {
	if inExtra {
		return
	}
	ev.bb.styleOpen("ic-prompt")
	switch {
	case row == 0:
		ev.bb.print(e.promptText)
	case !ev.noMultilineIndent:
		// Pad the continuation marker out so the text lines up under the
		// first row.
		textw := ev.bb.columnWidth(e.promptText)
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
func (ev *env) refreshRows(e *editor, input *buffer, attrs *attrBuf,
	promptw, cpromptw int, inExtra bool, firstRow, lastRow int,
) {
	input.forEachRow(e.termW, promptw, cpromptw, func(s []byte, row, rowStart, rowLen, _ int, isWrap bool) bool {
		if row < firstRow {
			return false
		}
		if row > lastRow {
			return true
		}
		ev.writePrompt(e, row, inExtra)
		text := string(s[rowStart : rowStart+rowLen])
		if attrs == nil || (ev.noHighlight && ev.noBraceMatch) {
			ev.term.write(text)
		} else {
			all := attrs.slice(rowStart + rowLen)
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
	return ev.tty == nil || ev.tty.isUTF8
}

// refresh draws the whole line, the hint inside it and anything shown below
// it, and puts the cursor back where it belongs.
func (ev *env) refresh(e *editor) {
	promptw, cpromptw := ev.promptWidth(e, false)

	fn := ev.highlighter
	if ev.noHighlight {
		fn = nil
	}
	runHighlight(ev.bb, e.input.string(), &e.attrs, fn)
	if !ev.noBraceMatch {
		highlightMatchBraces(e.input.string(), &e.attrs, e.pos, ev.opts.MatchBraces,
			ev.bb.style("ic-bracematch"), ev.bb.style("ic-error"))
	}

	// The hint goes into the line itself while it is drawn, and comes back out
	// at the end, so that everything below measures it as part of the text.
	if e.hint.Len() > 0 {
		e.attrs.insertAt(e.pos, e.hint.Len(), ev.bb.style("ic-hint"))
		e.input.insertAt(e.hint.String(), e.pos)
	}

	// Anything shown below the line, such as a completion menu.
	var extra *buffer
	if e.extra.length() > 0 {
		extra = &buffer{}
		if e.hintHelp.Len() > 0 {
			ev.bb.appendTo(e.hintHelp.String(), extra, &e.attrsExtra)
		}
		ev.bb.appendTo(e.extra.string(), extra, &e.attrsExtra)
	}

	rowsInput, rc := e.input.rowColAtPos(e.termW, promptw, cpromptw, e.pos)
	rowsExtra := 0
	if extra != nil {
		rowsExtra, _ = extra.rowColAtPos(e.termW, 0, 0, 0)
	}
	rows := rowsInput + rowsExtra

	// Draw at most a screen full, keeping the cursor in view.
	termh := ev.term.getHeight()
	firstRow, lastRow := 0, rows-1
	if rows > termh {
		firstRow = max(rc.row-termh+1, 0)
		lastRow = firstRow + termh - 1
	}

	was := ev.term.setBufferMode(buffered)

	// Go back to the first row of what was drawn last time.
	ev.term.startOfLine()
	ev.term.up(min(e.curRow, termh-1))

	ev.refreshRows(e, &e.input, &e.attrs, promptw, cpromptw, false, firstRow, lastRow)
	if rowsExtra > 0 {
		ev.refreshRows(e, extra, &e.attrsExtra, 0, 0, true,
			max(firstRow-rowsInput, 0), lastRow-rowsInput)
	}

	// Wipe any rows that were used last time and are not needed now.
	drawn := lastRow - firstRow + 1
	if drawn < termh && rows < e.curRows {
		for clear := e.curRows - rows; drawn < termh && clear > 0; clear-- {
			drawn++
			ev.term.writeln("")
			ev.term.clearLine()
		}
	}

	// Put the cursor back where the text says it should be.
	ev.term.startOfLine()
	ev.term.up(firstRow + drawn - 1 - rc.row)
	pw := cpromptw
	if rc.row == 0 {
		pw = promptw
	}
	ev.term.right(rc.col + pw)
	ev.term.flush()
	ev.term.setBufferMode(was)

	// Take the hint back out, so the line is what the user typed again.
	e.input.deleteAt(e.pos, e.hint.Len())
	e.extra.deleteAt(0, e.hintHelp.Len())
	e.attrs.clear()
	e.attrsExtra.clear()

	e.curRows = rows
	e.curRow = rc.row
}

// clear wipes the rows the line is drawn on.
func (ev *env) clear(e *editor) {
	ev.term.attrReset()
	ev.term.up(e.curRow)
	for range e.curRows {
		ev.term.clearLine()
		ev.term.writeln("")
	}
	ev.term.up(e.curRows - e.curRow)
}

// clearScreen wipes the screen and draws the line again.
func (ev *env) clearScreen(e *editor) {
	rows := e.curRows
	e.curRows = ev.term.getHeight() - 1
	ev.clear(e)
	e.curRows = rows
	ev.refresh(e)
}

// --------------------------------------------------------------------------
// editlineloop.go

// Reading a line: the hint, resizing, the key dispatch and the loop.
//
// Ported from isocline/src/editline.c.

// appendHintHelp puts the help text that goes with a hint below the line, or
// clears it when there is none.
func (e *editor) appendHintHelp(help string) {
	e.hintHelp.Reset()
	if help == "" {
		return
	}
	e.hintHelp.WriteString("[ic-info]")
	e.hintHelp.WriteString(help)
	e.hintHelp.WriteString("[/ic-info]\n")
}

// refreshHint draws the line and works out the hint to show inside it.
//
// A hint is the rest of the only completion that fits. When more than one
// fits there is nothing to hint at.
func (ev *env) refreshHint(e *editor) {
	if ev.noHint || ev.hintDelay > 0 {
		// Draw without the hint first, so the line appears at once and the
		// hint follows when it is ready.
		ev.refresh(e)
		if ev.noHint {
			return
		}
	}
	// Asking for two answers is enough: one means there is a hint, and more
	// than one means there is not.
	if ev.completions.generate(e.input.string(), e.pos, 2) != 1 {
		if ev.hintDelay <= 0 {
			ev.refresh(e)
		}
		return
	}
	hint, help, ok := ev.completions.hintAt(0)
	if ok {
		e.hint.Reset()
		e.hint.WriteString(hint)
		e.appendHintHelp(help)
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
func (ev *env) extendHint(e *editor, hint string) {
	var sb buffer
	sb.replace(e.input.string())
	pos := e.pos
	for {
		next := sb.insertAt(hint, pos)
		if next <= pos {
			return
		}
		pos = next
		if ev.completions.generate(sb.string(), pos, 2) != 1 {
			return
		}
		extra, help, ok := ev.completions.hintAt(0)
		if !ok {
			return
		}
		e.appendHintHelp(help)
		e.hint.WriteString(extra)
		hint = extra
	}
}

// resize works out the new layout after the terminal changed size, and
// reports whether it did change.
func (ev *env) resize(e *editor) bool {
	ev.term.updateDim()
	newW := ev.term.getWidth()
	if e.termW == newW {
		return false
	}
	promptw, cpromptw := ev.promptWidth(e, false)
	// The hint counts as part of the line while the rows are measured.
	e.input.insertAt(e.hint.String(), e.pos)
	var extra *buffer
	if e.extra.length() > 0 {
		extra = &buffer{}
		if e.hintHelp.Len() > 0 {
			ev.bb.appendTo(e.hintHelp.String(), extra, nil)
		}
		ev.bb.appendTo(e.extra.string(), extra, nil)
	}
	rowsInput, rc := e.input.wrappedRowColAtPos(e.termW, newW, promptw, cpromptw, e.pos)
	rowsExtra := 0
	if extra != nil {
		rowsExtra, _ = extra.wrappedRowColAtPos(e.termW, newW, 0, 0, 0)
	}
	rows := rowsInput + rowsExtra
	e.curRow = rc.row
	if rows > e.curRows {
		e.curRows = rows
	}
	e.termW = newW
	ev.refresh(e)
	e.input.deleteAt(e.pos, e.hint.Len())
	return true
}

// readKey waits for the next key, showing the hint if one is pending and the
// user pauses long enough.
func (ev *env) readKey(e *editor) key.Code {
	if ev.hintDelay <= 0 || e.hint.Len() == 0 {
		return ev.tty.read()
	}
	c, ok := ev.tty.readTimeout(ev.hintDelay)
	if ok {
		// Something was typed before the delay ran out, so the hint is stale.
		e.hint.Reset()
		e.hintHelp.Reset()
		return c
	}
	if e.hint.Len() > 0 {
		ev.refresh(e)
	}
	return ev.tty.read()
}

// act redraws when an operation changed something. The C redraws inside each
// operation, after the early return that leaves when there is nothing to do,
// so an operation that finds nothing writes no bytes at all.
func (ev *env) act(e *editor, changed bool) {
	if changed {
		ev.refresh(e)
	}
}

// handleKey acts on one key and reports whether the line is finished.
func (ev *env) handleKey(e *editor, c key.Code) bool {
	switch c {
	case key.Enter:
		// A line continuation character at the end of a row turns into a real
		// line break rather than finishing the line.
		if !ev.singlelineOnly && e.pos > 0 &&
			e.input.charAt(e.pos-1) == ev.opts.MultilineEOL && ev.posIsAtRowEnd(e) {
			ev.act(e, e.multilineEOL())
			return false
		}
		// The caller may say the line is not finished, which starts another
		// row instead of handing it back. That is how a prompt keeps reading
		// until a statement is closed.
		if !ev.singlelineOnly && ev.isIncomplete != nil && ev.isIncomplete(e.input.string()) {
			e.insertChar('\n')
			ev.refreshHint(e)
			return false
		}
		return true
	case key.CtrlD:
		// On an empty line this ends the input. Anywhere else it deletes.
		if e.pos == 0 && e.posIsAtEnd() {
			return true
		}
		ev.act(e, e.deleteChar())
		return false
	case key.EventStop:
		return true
	case key.Esc:
		if e.pos == 0 && e.posIsAtEnd() {
			return true
		}
		ev.act(e, e.deleteAll())
		return false
	case key.Bell, key.CtrlC:
		ev.act(e, e.deleteAll())
		return true
	}
	ev.editKey(e, c)
	return false
}

// editKey acts on a key that does not finish the line.
func (ev *env) editKey(e *editor, c key.Code) {
	switch c {
	case key.EventResize:
		ev.resize(e)
	case key.EventAutoTab:
		ev.generateCompletions(e, true)

	// Completion, history, help, undo.
	case key.Tab, '?' | key.ModAlt:
		ev.generateCompletions(e, false)
	case key.CtrlR, key.CtrlS:
		ev.historySearchWithCurrentWord(e)
	case key.CtrlP:
		ev.historyPrev(e)
	case key.CtrlN:
		ev.historyNext(e)
	case key.CtrlL:
		ev.clearScreen(e)
	case key.CtrlZ, '_' | key.ModCtrl:
		e.undoRestore(true)
		ev.refresh(e)
	case key.CtrlY:
		e.redoRestore()
		ev.refresh(e)
	case key.F1:
		ev.showHelp(e)

	// Moving about.
	case key.Left, key.CtrlB:
		ev.act(e, e.cursorLeft())
	case key.Right, key.CtrlF:
		// At the end of the line there is nothing to move over, so this asks
		// for a completion instead.
		if e.pos == e.input.length() {
			ev.generateCompletions(e, false)
			return
		}
		ev.act(e, e.cursorRight())
	case key.Up:
		ev.cursorRowUp(e)
	case key.Down:
		ev.cursorRowDown(e)
	case key.Home, key.CtrlA:
		ev.act(e, e.cursorLineStart())
	case key.End, key.CtrlE:
		ev.act(e, e.cursorLineEnd())
	case key.Left | key.ModCtrl, key.Left | key.ModShift, 'b' | key.ModAlt:
		ev.act(e, e.cursorPrevWord())
	case key.Right | key.ModCtrl, key.Right | key.ModShift, 'f' | key.ModAlt:
		if e.pos == e.input.length() {
			ev.generateCompletions(e, false)
			return
		}
		ev.act(e, e.cursorNextWord())
	case key.Home | key.ModCtrl, key.Home | key.ModShift, key.PageUp, '<' | key.ModAlt:
		ev.act(e, e.cursorToStart())
	case key.End | key.ModCtrl, key.End | key.ModShift, key.PageDown, '>' | key.ModAlt:
		ev.act(e, e.cursorToEnd())
	case 'm' | key.ModAlt:
		ev.act(e, e.cursorMatchBrace())

	// Deleting.
	case key.Backspace:
		ev.act(e, e.backspace())
	case key.Del:
		ev.act(e, e.deleteChar())
	case 'd' | key.ModAlt:
		ev.act(e, e.deleteToWordEnd())
	case key.CtrlW:
		ev.act(e, e.deleteToWSWordStart())
	case key.Del | key.ModAlt, key.Backspace | key.ModAlt:
		ev.act(e, e.deleteToWordStart())
	case key.CtrlU:
		ev.act(e, e.deleteToLineStart())
	case key.CtrlK:
		ev.act(e, e.deleteToLineEnd())
	case key.CtrlT:
		ev.act(e, e.swapChar())

	// Typing.
	case key.ShiftTab, key.Linefeed:
		if !ev.singlelineOnly {
			e.insertChar('\n')
			ev.refreshHint(e)
		}
	default:
		if chr, ok := c.ASCIIChar(); ok {
			e.insertChar(chr)
			ev.refreshHint(e)
			return
		}
		if r, ok := c.Unicode(); ok {
			e.insertRune(r)
			ev.refreshHint(e)
		}
		// Anything else is a key this editor has no use for.
	}
}

// cursorRowUp moves the cursor one screen row up, or walks back through the
// history when it is already on the first row.
func (ev *env) cursorRowUp(e *editor) {
	_, rc := ev.rowCol(e)
	if rc.row == 0 {
		ev.historyPrev(e)
		return
	}
	if ev.setPosAtRowCol(e, rc.row-1, rc.col) {
		ev.refresh(e)
	}
}

// cursorRowDown moves the cursor one screen row down, or walks forward
// through the history when it is already on the last row.
func (ev *env) cursorRowDown(e *editor) {
	rows, rc := ev.rowCol(e)
	if rc.row+1 >= rows {
		ev.historyNext(e)
		return
	}
	if ev.setPosAtRowCol(e, rc.row+1, rc.col) {
		ev.refresh(e)
	}
}

// readLine puts the terminal into raw mode, reads one line, and puts the
// terminal back.
//
// Raw mode is what stops the line discipline doing the editing itself. Without
// it the Enter key arrives as a line feed rather than a carriage return,
// because the terminal rewrites it on the way in, and the editor never sees
// the key that ends a line.
func (ev *env) readLine(promptText string) (string, bool, error) {
	if err := ev.tty.startRaw(); err != nil {
		return "", false, fmt.Errorf("switching the terminal to raw mode: %w", err)
	}
	ev.term.startRaw()
	line, ok, c := ev.editLine(promptText)
	ev.term.endRaw(false)
	ev.tty.endRaw()
	// The finished line stays on screen and the cursor moves below it.
	ev.term.writeln("")
	ev.term.flush()
	if c == key.CtrlC || c == key.Bell {
		// The user abandoned the line rather than finishing it. See
		// ErrInterrupted for why this is not what the C answers.
		return "", false, ErrInterrupted
	}
	return line, ok, nil
}

// runEditLoop reads keys until the line is finished, and returns the key that
// finished it.
//
// This is the loop itself, apart from the terminal being put into raw mode and
// the history being kept, so that a test can drive it over a fixed set of keys
// and look at the line afterwards. Reading the C, this is the body of the
// while inside edit_line.
func (ev *env) runEditLoop(e *editor) key.Code {
	for {
		ev.term.flush()
		c := ev.readKey(e)
		if ev.tty.resizeEvent() {
			ev.resize(e)
		}
		// The hint is dropped after a possible resize, so that the resize
		// measures the rows the hint was drawn on.
		hadHint := e.hint.Len() > 0
		e.hint.Reset()
		e.hintHelp.Reset()
		// Moving into a hint accepts it rather than stepping over nothing.
		if (c == key.Right || c == key.End) && hadHint {
			ev.generateCompletions(e, true)
			c = key.None
		}
		if ev.handleKey(e, c) {
			return c
		}
	}
}

// editLine reads one line from the terminal. It returns the line, false when
// the user ended the input rather than finishing a line, and the key that
// finished it, which is how the caller tells an abandoned line from an empty
// one.
func (ev *env) editLine(promptText string) (string, bool, key.Code) {
	e := &editor{
		opts:       ev.opts,
		termW:      ev.term.getWidth(),
		curRows:    1,
		promptText: promptText,
	}
	ev.writePrompt(e, 0, false)

	// The line being typed is always the newest history entry, so that walking
	// back and returning lands on it again.
	ev.history.push("")

	c := ev.runEditLoop(e)

	e.cursorToEnd()
	// One last draw, without brace matching, so no brace is left highlighted
	// on the finished line.
	was := ev.noBraceMatch
	ev.noBraceMatch = true
	ev.refresh(e)
	ev.noBraceMatch = was

	line, ok := e.input.string(), true
	if (c == key.CtrlD && e.input.length() == 0) || c == key.EventStop {
		line, ok = "", false
	} else if !ev.ttyIsUTF8() {
		line = string(e.input.decodeFromLocale())
	}

	ev.history.update(e.input.string())
	if !ok || e.input.length() <= 1 {
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
