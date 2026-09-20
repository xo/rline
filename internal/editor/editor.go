// Package editor holds a line being edited, the operations that change it,
// and the undo stack behind them.
//
// Every operation here changes the text and the cursor and nothing else. The
// C redraws at the end of each one; the port leaves that to the caller, so
// that what an operation does can be checked without a terminal.
//
// Ported from isocline/src/editline.c and undo.c.
package editor

import (
	"strings"

	"github.com/xo/rline/ansi"
	"github.com/xo/rline/internal/text"
)

// --------------------------------------------------------------------------
// editor.go

// The state of a line being edited, and the operations that change it.
//
// Every operation here changes the text and the cursor and nothing else. The
// C redraws at the end of each one; the port leaves that to the caller, so
// that what an operation does can be checked without a terminal.
//
// Ported from isocline/src/editline.c.

// EditOptions are the settings the editing operations read.
type EditOptions struct {
	// MatchPairs are the pairs that the cursor can jump between.
	MatchPairs string

	// AutoPairs are the pairs that close themselves when typed. These are
	// not only braces: the default set holds quotes as well, which is why
	// neither field is named for a brace.
	AutoPairs string

	// MultilineEOL is the character that turns into a line break at the end
	// of a line. Zero turns that off.
	MultilineEOL byte

	// NoAutoPair turns off closing a pair automatically.
	NoAutoPair bool
}

// Editor holds a line being edited.
// noCopy makes `go vet` refuse a copy of whatever embeds it.
//
// An Editor holds two strings.Builder, which panic at run time if they are
// written to after being copied. That was containable while the editor was
// an unexported struct in one package with one set of callers; exporting it
// across a package boundary means any code that can see the type can write
// `c := *e` and get a panic that neither the compiler nor vet would mention.
// Demonstrated rather than supposed: copying an Editor and writing to the
// copy panics with "illegal use of non-zero Builder copied by value", and
// `go vet ./...` says nothing about it.
//
// vet's copylocks check does notice a type with Lock and Unlock, so this
// turns that run-time panic into a build-time complaint. It holds no state
// and costs nothing; an Editor is always used through a pointer.
type noCopy struct{}

// Lock satisfies sync.Locker for vet's benefit. It is never called.
func (*noCopy) Lock() {}

// Unlock satisfies sync.Locker for vet's benefit. It is never called.
func (*noCopy) Unlock() {}

type Editor struct {
	_ noCopy

	// The line and where the cursor is in it.
	Input text.Buffer
	Pos   int

	// Text shown below the line, and the hint shown inside it.
	Extra    text.Buffer
	Hint     strings.Builder
	HintHelp strings.Builder

	// Where the line sits on screen.
	CurRows int
	CurRow  int
	TermW   int

	// Modified says the line changed since it was last put back.
	// disableUndo stops changes being recorded, which history search wants.
	Modified    bool
	DisableUndo bool

	// HistoryIdx is how far back through the history the line has walked.
	HistoryIdx int

	// Undo and redo hold earlier versions of the line.
	Undo EditStack
	Redo EditStack

	// PromptText is the prompt shown in front of the line.
	PromptText string

	// Opts are the settings the operations read.
	Opts EditOptions

	// Reused so that redrawing does not allocate.
	Attrs      ansi.AttrBuf
	AttrsExtra ansi.AttrBuf
}

//-------------------------------------------------------------
// Undo and redo
//-------------------------------------------------------------

// Capture records the line as it is now.
func (e *Editor) Capture(s *EditStack) {
	if e.DisableUndo {
		return
	}
	s.Capture(e.Input.String(), e.Pos)
}

// UndoCapture records the line so that it can be put back.
func (e *Editor) UndoCapture() {
	e.Capture(&e.Undo)
}

// UndoForget drops the most recent recorded version.
func (e *Editor) UndoForget() {
	if e.DisableUndo {
		return
	}
	e.Undo.Restore()
}

// Restore puts back the most recent version from one stack, recording the
// current line on the other first.
func (e *Editor) Restore(from, to *EditStack) {
	if e.DisableUndo || from.Count() == 0 {
		return
	}
	if to != nil {
		e.Capture(to)
	}
	s, pos, ok := from.Restore()
	if !ok {
		return
	}
	e.Pos = pos
	e.Input.Replace(s)
	e.Modified = false
}

// UndoRestore puts back the previous version of the line.
func (e *Editor) UndoRestore(withRedo bool) {
	if withRedo {
		e.Restore(&e.Undo, &e.Redo)
		return
	}
	e.Restore(&e.Undo, nil)
}

// RedoRestore puts back a version that was undone.
func (e *Editor) RedoRestore() {
	e.Restore(&e.Redo, &e.Undo)
	e.Modified = false
}

// StartModify records the line before changing it, and drops anything that
// could have been redone.
func (e *Editor) StartModify() {
	e.UndoCapture()
	e.Redo.Reset()
	e.Modified = true
}

// PosIsAtEnd reports whether the cursor sits after the last character.
func (e *Editor) PosIsAtEnd() bool {
	return e.Pos == e.Input.Length()
}

//-------------------------------------------------------------
// Moving the cursor
//-------------------------------------------------------------

// CursorLeft moves the cursor one character to the left. It reports whether
// it moved.
//
// Every operation here that can find nothing to do says so, because the caller
// only redraws when something changed. The C returns early in the same places,
// before it reaches its own redraw.
func (e *Editor) CursorLeft() bool {
	prev, _ := e.Input.Prev(e.Pos)
	if prev < 0 {
		return false
	}
	e.Pos = prev
	return true
}

// CursorRight moves the cursor one character to the right. It reports whether
// it moved.
func (e *Editor) CursorRight() bool {
	next, _ := e.Input.Next(e.Pos)
	if next < 0 {
		return false
	}
	e.Pos = next
	return true
}

// CursorLineEnd moves the cursor to the end of the line it is on. It reports whether it moved.
func (e *Editor) CursorLineEnd() bool {
	p := e.Input.FindLineEnd(e.Pos)
	if p < 0 {
		return false
	}
	e.Pos = p
	return true
}

// CursorLineStart moves the cursor to the start of the line it is on. It reports whether it moved.
func (e *Editor) CursorLineStart() bool {
	p := e.Input.FindLineStart(e.Pos)
	if p < 0 {
		return false
	}
	e.Pos = p
	return true
}

// CursorNextWord moves the cursor to the end of the word. It reports whether it moved.
func (e *Editor) CursorNextWord() bool {
	p := e.Input.FindWordEnd(e.Pos)
	if p < 0 {
		return false
	}
	e.Pos = p
	return true
}

// CursorPrevWord moves the cursor to the start of the word. It reports whether it moved.
func (e *Editor) CursorPrevWord() bool {
	p := e.Input.FindWordStart(e.Pos)
	if p < 0 {
		return false
	}
	e.Pos = p
	return true
}

// CursorNextWSWord moves the cursor past the next run of non-space. It reports whether it moved.
func (e *Editor) CursorNextWSWord() bool {
	p := e.Input.FindWSWordEnd(e.Pos)
	if p < 0 {
		return false
	}
	e.Pos = p
	return true
}

// CursorPrevWSWord moves the cursor back over the last run of non-space. It reports whether it moved.
func (e *Editor) CursorPrevWSWord() bool {
	p := e.Input.FindWSWordStart(e.Pos)
	if p < 0 {
		return false
	}
	e.Pos = p
	return true
}

// CursorToStart moves the cursor to the start of the whole line. It always
// counts as a move, because the C redraws there without checking.
func (e *Editor) CursorToStart() bool { e.Pos = 0; return true }

// CursorToEnd moves the cursor to the end of the whole line. It always counts
// as a move, for the same reason as cursorToStart.
func (e *Editor) CursorToEnd() bool { e.Pos = e.Input.Length(); return true }

// CursorMatchPair moves the cursor to the partner of the character it is on.
func (e *Editor) CursorMatchPair() bool {
	match, _ := text.FindMatchingBrace(e.Input.String(), e.Pos, e.Opts.MatchPairs)
	if match < 0 {
		return false
	}
	e.Pos = match
	return true
}

//-------------------------------------------------------------
// Deleting
//-------------------------------------------------------------

// Backspace deletes the character before the cursor.
func (e *Editor) Backspace() bool {
	if e.Pos <= 0 {
		return false
	}
	e.StartModify()
	e.Pos = e.Input.DeleteCharBefore(e.Pos)
	return true
}

// DeleteChar deletes the character under the cursor.
func (e *Editor) DeleteChar() bool {
	if e.Pos >= e.Input.Length() {
		return false
	}
	e.StartModify()
	e.Input.DeleteCharAt(e.Pos)
	return true
}

// DeleteAll empties the line.
func (e *Editor) DeleteAll() bool {
	if e.Input.Length() <= 0 {
		return false
	}
	e.StartModify()
	e.Input.Clear()
	e.Pos = 0
	return true
}

// DeleteToLineEnd deletes from the cursor to the end of the line. On an empty
// line it takes the line break as well, so no blank line is left.
func (e *Editor) DeleteToLineEnd() bool {
	start := e.Input.FindLineStart(e.Pos)
	if start < 0 {
		return false
	}
	end := e.Input.FindLineEnd(e.Pos)
	if end < 0 {
		return false
	}
	e.StartModify()
	switch {
	case start == end && e.Input.CharAt(end) == '\n':
		end++
	case start == end && e.Input.CharAt(start-1) == '\n':
		e.Pos--
	}
	e.Input.DeleteFromTo(e.Pos, end)
	return true
}

// DeleteToLineStart deletes from the start of the line to the cursor.
func (e *Editor) DeleteToLineStart() bool {
	start := e.Input.FindLineStart(e.Pos)
	if start < 0 {
		return false
	}
	end := e.Input.FindLineEnd(e.Pos)
	if end < 0 {
		return false
	}
	e.StartModify()
	goRight := false
	if start > 0 && e.Input.CharAt(start-1) == '\n' && start == end {
		start--
		goRight = true
	}
	e.Input.DeleteFromTo(start, e.Pos)
	e.Pos = start
	if goRight {
		e.CursorRight()
	}
	return true
}

// DeleteLine deletes the whole line the cursor is on, and the line break with
// it so that no blank line is left.
func (e *Editor) DeleteLine() bool {
	start := e.Input.FindLineStart(e.Pos)
	if start < 0 {
		return false
	}
	end := e.Input.FindLineEnd(e.Pos)
	if end < 0 {
		return false
	}
	e.StartModify()
	goRight := false
	switch {
	case start > 0 && e.Input.CharAt(start-1) == '\n':
		start--
		goRight = true
	case e.Input.CharAt(end) == '\n':
		end++
	}
	e.Input.DeleteFromTo(start, end)
	e.Pos = start
	if goRight {
		e.CursorRight()
	}
	return true
}

// DeleteToWordStart deletes back to the start of the word.
func (e *Editor) DeleteToWordStart() bool {
	return e.DeleteBackTo(e.Input.FindWordStart(e.Pos))
}

// DeleteToWordEnd deletes forward to the end of the word.
func (e *Editor) DeleteToWordEnd() bool {
	return e.DeleteForwardTo(e.Input.FindWordEnd(e.Pos))
}

// DeleteToWSWordStart deletes back over the last run of non-space.
func (e *Editor) DeleteToWSWordStart() bool {
	return e.DeleteBackTo(e.Input.FindWSWordStart(e.Pos))
}

// DeleteToWSWordEnd deletes forward over the next run of non-space.
func (e *Editor) DeleteToWSWordEnd() bool {
	return e.DeleteForwardTo(e.Input.FindWSWordEnd(e.Pos))
}

// DeleteBackTo deletes from start to the cursor and puts the cursor there.
func (e *Editor) DeleteBackTo(start int) bool {
	if start < 0 {
		return false
	}
	e.StartModify()
	e.Input.DeleteFromTo(start, e.Pos)
	e.Pos = start
	return true
}

// DeleteForwardTo deletes from the cursor up to end.
func (e *Editor) DeleteForwardTo(end int) bool {
	if end < 0 {
		return false
	}
	e.StartModify()
	e.Input.DeleteFromTo(e.Pos, end)
	return true
}

// DeleteWord deletes the whole word the cursor is in.
func (e *Editor) DeleteWord() bool {
	start := e.Input.FindWordStart(e.Pos)
	if start < 0 {
		return false
	}
	end := e.Input.FindWordEnd(e.Pos)
	if end < 0 {
		return false
	}
	e.StartModify()
	e.Input.DeleteFromTo(start, end)
	e.Pos = start
	return true
}

//-------------------------------------------------------------
// Changing the text
//-------------------------------------------------------------

// SwapChar swaps the character before the cursor with the one after it.
func (e *Editor) SwapChar() bool {
	if e.Pos <= 0 || e.Pos == e.Input.Length() {
		return false
	}
	e.StartModify()
	e.Pos = e.Input.SwapChar(e.Pos)
	return true
}

// MultilineEOL turns the line continuation character before the cursor into a
// real line break.
func (e *Editor) MultilineEOL() bool {
	if e.Pos <= 0 || e.Input.CharAt(e.Pos-1) != e.Opts.MultilineEOL {
		return false
	}
	e.StartModify()
	e.Input.DeleteAt(e.Pos-1, 1)
	e.Input.InsertAt("\n", e.Pos-1)
	return true
}

// InsertRune puts one character in at the cursor.
func (e *Editor) InsertRune(r rune) {
	e.StartModify()
	if next := e.Input.InsertRuneAt(r, e.Pos); next >= 0 {
		e.Pos = next
	}
}

// InsertChar puts one byte in at the cursor, closing a brace after it and
// indenting a new line when that applies.
func (e *Editor) InsertChar(c byte) {
	e.StartModify()
	if next := e.Input.InsertByteAt(c, e.Pos); next >= 0 {
		e.Pos = next
	}
	e.AutoPair(c)
	if c == '\n' {
		e.AutoIndent("{", "}")
	}
}

// AutoPair closes a brace that was just opened, or steps over one that was
// closed by hand.
//
// A closing brace is only added when the line stays balanced with it, so
// typing an opening brace in front of text that already closes it adds
// nothing.
func (e *Editor) AutoPair(c byte) {
	if e.Opts.NoAutoPair {
		return
	}
	braces := e.Opts.AutoPairs
	for b := 0; b+1 < len(braces); b += 2 {
		switch c {
		case braces[b]:
			e.Input.InsertByteAt(braces[b+1], e.Pos)
			if _, balanced := text.FindMatchingBrace(e.Input.String(), e.Pos, braces); !balanced {
				e.Input.DeleteCharAt(e.Pos)
			}
			return
		case braces[b+1]:
			// Typing a closing brace over one that is already there moves past
			// it rather than adding a second.
			if e.Input.CharAt(e.Pos) == c {
				e.Input.DeleteCharAt(e.Pos)
			}
			return
		}
	}
}

// AutoIndent indents a new line that was opened between pre and post, and puts
// the closing part on a line of its own.
func (e *Editor) AutoIndent(pre, post string) {
	if pre == "" || e.Pos-1 < len(pre) {
		return
	}
	s := e.Input.String()
	if !StrHasPrefixAt(s, e.Pos-1-len(pre), pre) || !StrHasPrefixAt(s, e.Pos, post) {
		return
	}
	e.Pos = e.Input.InsertAt("  ", e.Pos)
	e.Input.InsertByteAt('\n', e.Pos)
}

// StrHasPrefixAt reports whether s holds prefix at i.
func StrHasPrefixAt(s string, i int, prefix string) bool {
	if i < 0 || i > len(s) {
		return false
	}
	return len(s[i:]) >= len(prefix) && s[i:i+len(prefix)] == prefix
}

// --------------------------------------------------------------------------
// undo.go

// The undo stack: what the line looked like before each change.
//
// The edit loop saves the whole line rather than a description of the change,
// because a line is short and saving it is simpler than working out how to
// reverse an edit.
//
// Ported from isocline/src/undo.c.

// EditState is a line and where the cursor was in it.
type EditState struct {
	// Input is the whole line, and pos the byte the cursor sat on.
	Input string
	Pos   int
}

// EditStack holds the states that undo steps back through. The most recent
// is at the end. The zero value is ready to use.
type EditStack struct {
	states []EditState
}

// Capture saves a line and a cursor position.
func (s *EditStack) Capture(input string, pos int) {
	s.states = append(s.states, EditState{Input: input, Pos: pos})
}

// Restore takes the most recent saved line off the stack. It reports false
// when there is nothing left to step back to.
func (s *EditStack) Restore() (string, int, bool) {
	n := len(s.states)
	if n == 0 {
		return "", 0, false
	}
	state := s.states[n-1]
	s.states = s.states[:n-1]
	return state.Input, state.Pos, true
}

// Reset throws away every saved line.
func (s *EditStack) Reset() {
	s.states = nil
}

// Count returns how many saved lines the stack holds.
func (s *EditStack) Count() int {
	return len(s.states)
}
