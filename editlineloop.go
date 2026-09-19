package rline

import (
	"fmt"

	"github.com/xo/rline/key"
)

// Reading a line: the hint, resizing, the key dispatch and the loop.
//
// Ported from isocline/src/editline.c.
//
// Nothing here has a caller yet. The public API in step 12 is what starts the
// loop, and until then every function below carries a nolint saying so.

// appendHintHelp puts the help text that goes with a hint below the line, or
// clears it when there is none.
//
//nolint:unused // started by the public API, step 12
func (e *editor) appendHintHelp(help string) {
	e.hintHelp.clear()
	if help == "" {
		return
	}
	e.hintHelp.replace("[ic-info]")
	e.hintHelp.appendString(help)
	e.hintHelp.appendString("[/ic-info]\n")
}

// refreshHint draws the line and works out the hint to show inside it.
//
// A hint is the rest of the only completion that fits. When more than one
// fits there is nothing to hint at.
//
//nolint:unused // started by the public API, step 12
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
		e.hint.replace(hint)
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
//
//nolint:unused // started by the public API, step 12
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
		e.hint.appendString(extra)
		hint = extra
	}
}

// resize works out the new layout after the terminal changed size, and
// reports whether it did change.
//
//nolint:unused // started by the public API, step 12
func (ev *env) resize(e *editor) bool {
	ev.term.updateDim()
	newW := ev.term.getWidth()
	if e.termW == newW {
		return false
	}
	promptw, cpromptw := ev.promptWidth(e, false)
	// The hint counts as part of the line while the rows are measured.
	e.input.insertAt(e.hint.string(), e.pos)
	var extra *buffer
	if e.extra.length() > 0 {
		extra = &buffer{}
		if e.hintHelp.length() > 0 {
			ev.bb.appendTo(e.hintHelp.string(), extra, nil)
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
	e.input.deleteAt(e.pos, e.hint.length())
	return true
}

// readKey waits for the next key, showing the hint if one is pending and the
// user pauses long enough.
//
//nolint:unused // started by the public API, step 12
func (ev *env) readKey(e *editor) key.Code {
	if ev.hintDelay <= 0 || e.hint.length() == 0 {
		return ev.tty.read()
	}
	c, ok := ev.tty.readTimeout(ev.hintDelay)
	if ok {
		// Something was typed before the delay ran out, so the hint is stale.
		e.hint.clear()
		e.hintHelp.clear()
		return c
	}
	if e.hint.length() > 0 {
		ev.refresh(e)
	}
	return ev.tty.read()
}

// act redraws when an operation changed something. The C redraws inside each
// operation, after the early return that leaves when there is nothing to do,
// so an operation that finds nothing writes no bytes at all.
//
//nolint:unused // started by the public API, step 12
func (ev *env) act(e *editor, changed bool) {
	if changed {
		ev.refresh(e)
	}
}

// handleKey acts on one key and reports whether the line is finished.
//
//nolint:unused // started by the public API, step 12
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
//
//nolint:unused // started by the public API, step 12
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
//
//nolint:unused // started by the public API, step 12
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
//
//nolint:unused // started by the public API, step 12
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
		hadHint := e.hint.length() > 0
		e.hint.clear()
		e.hintHelp.clear()
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
//
//nolint:unused // started by the public API, step 12
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
