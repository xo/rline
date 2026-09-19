package rline

import "time"

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
//
//nolint:unused // the key dispatch and the main loop fill in the rest
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
//
//nolint:unused // used by the key dispatch, next slice
func (ev *env) rowCol(e *editor) (int, rowCol) {
	promptw, cpromptw := ev.promptWidth(e, false)
	return e.input.rowColAtPos(e.termW, promptw, cpromptw, e.pos)
}

// setPosAtRowCol moves the cursor to a row and column on screen.
//
//nolint:unused // used by the key dispatch, next slice
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
//
//nolint:unused // used by the key dispatch, next slice
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
	if e.hint.length() > 0 {
		e.attrs.insertAt(e.pos, e.hint.length(), ev.bb.style("ic-hint"))
		e.input.insertAt(e.hint.string(), e.pos)
	}

	// Anything shown below the line, such as a completion menu.
	var extra *buffer
	if e.extra.length() > 0 {
		extra = &buffer{}
		if e.hintHelp.length() > 0 {
			ev.bb.appendTo(e.hintHelp.string(), extra, &e.attrsExtra)
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
	e.input.deleteAt(e.pos, e.hint.length())
	e.extra.deleteAt(0, e.hintHelp.length())
	e.attrs.clear()
	e.attrsExtra.clear()

	e.curRows = rows
	e.curRow = rc.row
}

// clear wipes the rows the line is drawn on.
//
//nolint:unused // used by the key dispatch, next slice
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
//
//nolint:unused // used by the key dispatch, next slice
//nolint:unused // used by the key dispatch, next slice
func (ev *env) clearScreen(e *editor) {
	rows := e.curRows
	e.curRows = ev.term.getHeight() - 1
	ev.clear(e)
	e.curRows = rows
	ev.refresh(e)
}
