package rline

import "strings"

// The state of a line being edited, and the operations that change it.
//
// Every operation here changes the text and the cursor and nothing else. The
// C redraws at the end of each one; the port leaves that to the caller, so
// that what an operation does can be checked without a terminal.
//
// Ported from isocline/src/editline.c.

// editOptions are the settings the editing operations read.
type editOptions struct {
	// MatchBraces are the brace pairs that the cursor can jump between.
	MatchBraces string

	// AutoBraces are the brace pairs that close themselves when typed.
	AutoBraces string

	// MultilineEOL is the character that turns into a line break at the end
	// of a line. Zero turns that off.
	MultilineEOL byte

	// NoAutoBrace turns off closing a brace automatically.
	NoAutoBrace bool
}

// editor holds a line being edited.
//
//nolint:unused // the redraw and the key dispatch fill in the rest, next slice
type editor struct {
	// The line and where the cursor is in it.
	input buffer
	pos   int

	// Text shown below the line, and the hint shown inside it.
	extra    buffer
	hint     strings.Builder
	hintHelp strings.Builder

	// Where the line sits on screen.
	curRows int
	curRow  int
	termW   int

	// modified says the line changed since it was last put back.
	// disableUndo stops changes being recorded, which history search wants.
	modified    bool
	disableUndo bool

	// historyIdx is how far back through the history the line has walked.
	historyIdx int

	// undo and redo hold earlier versions of the line.
	undo editStack
	redo editStack

	// promptText is the prompt shown in front of the line.
	promptText string

	// opts are the settings the operations read.
	opts editOptions

	// Reused so that redrawing does not allocate.
	attrs      attrBuf
	attrsExtra attrBuf
}

//-------------------------------------------------------------
// Undo and redo
//-------------------------------------------------------------

// capture records the line as it is now.
func (e *editor) capture(s *editStack) {
	if e.disableUndo {
		return
	}
	s.capture(e.input.string(), e.pos)
}

// undoCapture records the line so that it can be put back.
func (e *editor) undoCapture() {
	e.capture(&e.undo)
}

// undoForget drops the most recent recorded version.
//
//nolint:unused // used by the key dispatch, next slice
func (e *editor) undoForget() {
	if e.disableUndo {
		return
	}
	e.undo.restore()
}

// restore puts back the most recent version from one stack, recording the
// current line on the other first.
func (e *editor) restore(from, to *editStack) {
	if e.disableUndo || from.count() == 0 {
		return
	}
	if to != nil {
		e.capture(to)
	}
	s, pos, ok := from.restore()
	if !ok {
		return
	}
	e.pos = pos
	e.input.replace(s)
	e.modified = false
}

// undoRestore puts back the previous version of the line.
func (e *editor) undoRestore(withRedo bool) {
	if withRedo {
		e.restore(&e.undo, &e.redo)
		return
	}
	e.restore(&e.undo, nil)
}

// redoRestore puts back a version that was undone.
func (e *editor) redoRestore() {
	e.restore(&e.redo, &e.undo)
	e.modified = false
}

// startModify records the line before changing it, and drops anything that
// could have been redone.
func (e *editor) startModify() {
	e.undoCapture()
	e.redo.reset()
	e.modified = true
}

// posIsAtEnd reports whether the cursor sits after the last character.
//
//nolint:unused // used by the key dispatch, next slice
func (e *editor) posIsAtEnd() bool {
	return e.pos == e.input.length()
}

//-------------------------------------------------------------
// Moving the cursor
//-------------------------------------------------------------

// cursorLeft moves the cursor one character to the left. It reports whether
// it moved.
//
// Every operation here that can find nothing to do says so, because the caller
// only redraws when something changed. The C returns early in the same places,
// before it reaches its own redraw.
func (e *editor) cursorLeft() bool {
	prev, _ := e.input.prev(e.pos)
	if prev < 0 {
		return false
	}
	e.pos = prev
	return true
}

// cursorRight moves the cursor one character to the right. It reports whether
// it moved.
func (e *editor) cursorRight() bool {
	next, _ := e.input.next(e.pos)
	if next < 0 {
		return false
	}
	e.pos = next
	return true
}

// cursorLineEnd moves the cursor to the end of the line it is on. It reports whether it moved.
func (e *editor) cursorLineEnd() bool {
	p := e.input.findLineEnd(e.pos)
	if p < 0 {
		return false
	}
	e.pos = p
	return true
}

// cursorLineStart moves the cursor to the start of the line it is on. It reports whether it moved.
func (e *editor) cursorLineStart() bool {
	p := e.input.findLineStart(e.pos)
	if p < 0 {
		return false
	}
	e.pos = p
	return true
}

// cursorNextWord moves the cursor to the end of the word. It reports whether it moved.
func (e *editor) cursorNextWord() bool {
	p := e.input.findWordEnd(e.pos)
	if p < 0 {
		return false
	}
	e.pos = p
	return true
}

// cursorPrevWord moves the cursor to the start of the word. It reports whether it moved.
func (e *editor) cursorPrevWord() bool {
	p := e.input.findWordStart(e.pos)
	if p < 0 {
		return false
	}
	e.pos = p
	return true
}

// cursorNextWSWord moves the cursor past the next run of non-space. It reports whether it moved.
func (e *editor) cursorNextWSWord() bool {
	p := e.input.findWSWordEnd(e.pos)
	if p < 0 {
		return false
	}
	e.pos = p
	return true
}

// cursorPrevWSWord moves the cursor back over the last run of non-space. It reports whether it moved.
func (e *editor) cursorPrevWSWord() bool {
	p := e.input.findWSWordStart(e.pos)
	if p < 0 {
		return false
	}
	e.pos = p
	return true
}

// cursorToStart moves the cursor to the start of the whole line. It always
// counts as a move, because the C redraws there without checking.
func (e *editor) cursorToStart() bool { e.pos = 0; return true }

// cursorToEnd moves the cursor to the end of the whole line. It always counts
// as a move, for the same reason as cursorToStart.
func (e *editor) cursorToEnd() bool { e.pos = e.input.length(); return true }

// cursorMatchBrace moves the cursor to the brace that goes with the one it is
// on.
func (e *editor) cursorMatchBrace() bool {
	match, _ := findMatchingBrace(e.input.string(), e.pos, e.opts.MatchBraces)
	if match < 0 {
		return false
	}
	e.pos = match
	return true
}

//-------------------------------------------------------------
// Deleting
//-------------------------------------------------------------

// backspace deletes the character before the cursor.
func (e *editor) backspace() bool {
	if e.pos <= 0 {
		return false
	}
	e.startModify()
	e.pos = e.input.deleteCharBefore(e.pos)
	return true
}

// deleteChar deletes the character under the cursor.
func (e *editor) deleteChar() bool {
	if e.pos >= e.input.length() {
		return false
	}
	e.startModify()
	e.input.deleteCharAt(e.pos)
	return true
}

// deleteAll empties the line.
func (e *editor) deleteAll() bool {
	if e.input.length() <= 0 {
		return false
	}
	e.startModify()
	e.input.clear()
	e.pos = 0
	return true
}

// deleteToLineEnd deletes from the cursor to the end of the line. On an empty
// line it takes the line break as well, so no blank line is left.
func (e *editor) deleteToLineEnd() bool {
	start := e.input.findLineStart(e.pos)
	if start < 0 {
		return false
	}
	end := e.input.findLineEnd(e.pos)
	if end < 0 {
		return false
	}
	e.startModify()
	switch {
	case start == end && e.input.charAt(end) == '\n':
		end++
	case start == end && e.input.charAt(start-1) == '\n':
		e.pos--
	}
	e.input.deleteFromTo(e.pos, end)
	return true
}

// deleteToLineStart deletes from the start of the line to the cursor.
func (e *editor) deleteToLineStart() bool {
	start := e.input.findLineStart(e.pos)
	if start < 0 {
		return false
	}
	end := e.input.findLineEnd(e.pos)
	if end < 0 {
		return false
	}
	e.startModify()
	goRight := false
	if start > 0 && e.input.charAt(start-1) == '\n' && start == end {
		start--
		goRight = true
	}
	e.input.deleteFromTo(start, e.pos)
	e.pos = start
	if goRight {
		e.cursorRight()
	}
	return true
}

// deleteLine deletes the whole line the cursor is on, and the line break with
// it so that no blank line is left.
func (e *editor) deleteLine() bool {
	start := e.input.findLineStart(e.pos)
	if start < 0 {
		return false
	}
	end := e.input.findLineEnd(e.pos)
	if end < 0 {
		return false
	}
	e.startModify()
	goRight := false
	switch {
	case start > 0 && e.input.charAt(start-1) == '\n':
		start--
		goRight = true
	case e.input.charAt(end) == '\n':
		end++
	}
	e.input.deleteFromTo(start, end)
	e.pos = start
	if goRight {
		e.cursorRight()
	}
	return true
}

// deleteToWordStart deletes back to the start of the word.
func (e *editor) deleteToWordStart() bool {
	return e.deleteBackTo(e.input.findWordStart(e.pos))
}

// deleteToWordEnd deletes forward to the end of the word.
func (e *editor) deleteToWordEnd() bool {
	return e.deleteForwardTo(e.input.findWordEnd(e.pos))
}

// deleteToWSWordStart deletes back over the last run of non-space.
func (e *editor) deleteToWSWordStart() bool {
	return e.deleteBackTo(e.input.findWSWordStart(e.pos))
}

// deleteToWSWordEnd deletes forward over the next run of non-space.
func (e *editor) deleteToWSWordEnd() bool {
	return e.deleteForwardTo(e.input.findWSWordEnd(e.pos))
}

// deleteBackTo deletes from start to the cursor and puts the cursor there.
func (e *editor) deleteBackTo(start int) bool {
	if start < 0 {
		return false
	}
	e.startModify()
	e.input.deleteFromTo(start, e.pos)
	e.pos = start
	return true
}

// deleteForwardTo deletes from the cursor up to end.
func (e *editor) deleteForwardTo(end int) bool {
	if end < 0 {
		return false
	}
	e.startModify()
	e.input.deleteFromTo(e.pos, end)
	return true
}

// deleteWord deletes the whole word the cursor is in.
func (e *editor) deleteWord() bool {
	start := e.input.findWordStart(e.pos)
	if start < 0 {
		return false
	}
	end := e.input.findWordEnd(e.pos)
	if end < 0 {
		return false
	}
	e.startModify()
	e.input.deleteFromTo(start, end)
	e.pos = start
	return true
}

//-------------------------------------------------------------
// Changing the text
//-------------------------------------------------------------

// swapChar swaps the character before the cursor with the one after it.
func (e *editor) swapChar() bool {
	if e.pos <= 0 || e.pos == e.input.length() {
		return false
	}
	e.startModify()
	e.pos = e.input.swapChar(e.pos)
	return true
}

// multilineEOL turns the line continuation character before the cursor into a
// real line break.
func (e *editor) multilineEOL() bool {
	if e.pos <= 0 || e.input.charAt(e.pos-1) != e.opts.MultilineEOL {
		return false
	}
	e.startModify()
	e.input.deleteAt(e.pos-1, 1)
	e.input.insertAt("\n", e.pos-1)
	return true
}

// insertRune puts one character in at the cursor.
//
//nolint:unused // used by the key dispatch, next slice
func (e *editor) insertRune(r rune) {
	e.startModify()
	if next := e.input.insertRuneAt(r, e.pos); next >= 0 {
		e.pos = next
	}
}

// insertChar puts one byte in at the cursor, closing a brace after it and
// indenting a new line when that applies.
func (e *editor) insertChar(c byte) {
	e.startModify()
	if next := e.input.insertByteAt(c, e.pos); next >= 0 {
		e.pos = next
	}
	e.autoBrace(c)
	if c == '\n' {
		e.autoIndent("{", "}")
	}
}

// autoBrace closes a brace that was just opened, or steps over one that was
// closed by hand.
//
// A closing brace is only added when the line stays balanced with it, so
// typing an opening brace in front of text that already closes it adds
// nothing.
func (e *editor) autoBrace(c byte) {
	if e.opts.NoAutoBrace {
		return
	}
	braces := e.opts.AutoBraces
	for b := 0; b+1 < len(braces); b += 2 {
		switch c {
		case braces[b]:
			e.input.insertByteAt(braces[b+1], e.pos)
			if _, balanced := findMatchingBrace(e.input.string(), e.pos, braces); !balanced {
				e.input.deleteCharAt(e.pos)
			}
			return
		case braces[b+1]:
			// Typing a closing brace over one that is already there moves past
			// it rather than adding a second.
			if e.input.charAt(e.pos) == c {
				e.input.deleteCharAt(e.pos)
			}
			return
		}
	}
}

// autoIndent indents a new line that was opened between pre and post, and puts
// the closing part on a line of its own.
func (e *editor) autoIndent(pre, post string) {
	if pre == "" || e.pos-1 < len(pre) {
		return
	}
	s := e.input.string()
	if !strHasPrefixAt(s, e.pos-1-len(pre), pre) || !strHasPrefixAt(s, e.pos, post) {
		return
	}
	e.pos = e.input.insertAt("  ", e.pos)
	e.input.insertByteAt('\n', e.pos)
}

// strHasPrefixAt reports whether s holds prefix at i.
func strHasPrefixAt(s string, i int, prefix string) bool {
	if i < 0 || i > len(s) {
		return false
	}
	return len(s[i:]) >= len(prefix) && s[i:i+len(prefix)] == prefix
}
