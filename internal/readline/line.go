// Package readline edits one line with the commands of GNU readline: the
// cursor and the mark, the kill ring, the undo list, the numeric argument and
// the history (D44, D45, D46).
//
// It is written from readline's manual and from recordings of what readline
// 8.3 does, which tools/probe-readline.c makes. It translates none of
// readline's C, which is under the GPL. Every position counts bytes of UTF-8,
// as readline counts them, and every move steps over whole characters.
package readline

import (
	"bytes"
	"unicode"
	"unicode/utf8"

	"github.com/xo/rline/key"
)

// Line is one line being edited, and the state that readline's commands read
// and change while it is.
type Line struct {
	buf   []byte
	point int
	mark  int

	keymap  *Keymap
	history *History
	kills   killRing
	undo    undoList

	// The state of the dispatch between two keys.
	pending  []key.Code // the keys of a sequence that is not complete yet
	seqLen   int        // how many keys the sequence of the running command has
	arg      argument   // the numeric argument being typed
	readNext func(c key.Code)

	// The command that ran last and the one that is running, by name, and
	// whether each killed text. A kill right after a kill joins it.
	last, this         string
	lastKill, thisKill bool

	// The insertion of the last yank-last-arg, and how far back it reached,
	// so that the next one can replace it.
	lastArgStart, lastArgEnd, lastArgBack int

	// The text a history search looks for, kept while the searches follow
	// one another.
	search string

	// overwrite says typed characters replace the ones at the cursor.
	overwrite bool

	// The keyboard macro: the keys being recorded, and the last one.
	recording bool
	macro     []key.Code
	lastMacro []key.Code

	// home returns the home directory for tilde expansion: the user's own for
	// an empty name. It is a field so that a test can fix it.
	home func(name string) (string, bool)

	// Completion: what completes, the list to show and the question being
	// asked, the state of menu-complete, and whether the last complete
	// changed the line or found nothing.
	completer   Completer
	listing     []Candidate
	asking      string
	menu        *menu
	compChanged bool
	compFailed  bool

	// The variables of readline that an inputrc file can set, by what they
	// change.
	vars settings

	// vi mode: the keymap of each editing mode and the mode in use, the
	// operator waiting for a motion, the last f or t, and the state of the
	// redo command, which repeats the keys of the last change.
	keymaps      [3]*Keymap
	mode         editMode
	viOp         *viOperator
	viLastSearch viSearch
	viInitial    bool // the insert mode that the line started in
	viNoClamp    bool // the cursor may stay after the last character once
	viChange     bool // the command being recorded changed the line
	viRecording  bool
	viReplaying  bool
	viRec        []key.Code
	viRedo       []key.Code
	depth        int // how deep Key runs inside itself

	// searching is the incremental search under way, and lastSearch what the
	// last one looked for.
	searching  *isearch
	lastSearch string

	// reading is the text being read in a prompt of its own, as the
	// non-incremental searches and execute-named-command read it, and
	// readSearch what the last non-incremental search looked for.
	reading    *reader
	readSearch string

	// next is the history line that the next line starts with, which
	// operate-and-get-next sets.
	next int

	// output is what the commands that print, such as dump-variables, wrote
	// for the program to show.
	output []byte

	// The width of the screen and of the prompt in columns, which the
	// commands that move by screen rows need.
	screenWidth, promptWidth int

	// The marks of vi, a to z, the original characters that vi's replace
	// mode overwrote, so that Rubout can put them back, and how long the
	// last put was, for vi-yank-pop.
	viMarks    [26]int
	viReplaced []string
	viPutLen   int

	// allVars holds every variable of readline with its value, for
	// dump-variables, and extra names the commands of the program, for
	// dump-functions.
	allVars map[string]any
	extra   []string

	// handler runs the commands of the program, and may take over any
	// command of this package. cmd and call are the command that is running
	// and what it was given, so that the handler can run it with Default.
	handler Handler
	cmd     command
	call    call

	done bool
	eof  bool
	bell int
}

// New returns an empty line that reads its keys through km and walks h.
func New(km *Keymap, h *History) *Line {
	if h == nil {
		h = &History{}
	}
	h.start()
	return &Line{keymap: km, history: h, home: homeDir, vars: defaultSettings(), next: -1, screenWidth: 80}
}

// A Handler runs a command for the program that reads the line. It is given
// every command before this package runs it, by name, with the key that ran
// it, and reports whether it ran it. A command that it does not run is run by
// this package, or rings the bell when this package does not have it.
//
// Completion, drawing and searching belong to the program, and a handler is
// how they reach the line: it changes the line with Edit, and can run the
// command of this package as well with Default.
type Handler func(name string, c key.Code) bool

// SetHandler sets the handler for the commands of the program.
func (l *Line) SetHandler(h Handler) { l.handler = h }

// Default runs the command of this package that is running, as it would have
// run without the handler. It is for a handler that adds to a command rather
// than replacing it. It does nothing when this package has no such command.
func (l *Line) Default() {
	if l.cmd != nil {
		l.cmd(l, l.call)
	}
}

// Count returns the numeric argument of the command that is running, and
// whether one was typed.
func (l *Line) Count() (int, bool) { return l.call.count, l.call.explicit }

// Last returns the name of the command that ran last.
func (l *Line) Last() string { return l.last }

// Edit makes the line text and puts the cursor at point. It records the
// change for undo, as part of the command that is running, or as a change of
// its own outside a command. Only the part of the line that differs is
// recorded.
func (l *Line) Edit(text string, point int) {
	if l.undo.open == nil {
		l.undo.begin()
		defer l.undo.end()
	}
	old := l.buf
	start := 0
	for start < len(old) && start < len(text) && old[start] == text[start] {
		start++
	}
	oldEnd, newEnd := len(old), len(text)
	for oldEnd > start && newEnd > start && old[oldEnd-1] == text[newEnd-1] {
		oldEnd--
		newEnd--
	}
	if oldEnd > start || newEnd > start {
		l.replace(start, oldEnd, []byte(text[start:newEnd]))
	}
	l.point = min(max(point, 0), len(l.buf))
	l.mark = min(l.mark, len(l.buf))
}

// Run runs the command called name, as a key bound to it would, with c as
// the key that ran it.
func (l *Line) Run(name string, c key.Code) {
	if l.done {
		return
	}
	l.run(name, c)
}

// Text returns the line.
func (l *Line) Text() string { return string(l.buf) }

// Point returns the cursor, in bytes.
func (l *Line) Point() int { return l.point }

// Mark returns the mark, in bytes.
func (l *Line) Mark() int { return l.mark }

// Done reports whether the line was handed back, by accept-line or by the
// end of the input.
func (l *Line) Done() bool { return l.done }

// EOF reports whether the input ended: Ctrl-D on an empty line.
func (l *Line) EOF() bool { return l.eof }

// Bells returns how many times a command rang the bell because it could not
// do what it was asked.
func (l *Line) Bells() int { return l.bell }

// ding records that a command could do nothing, as readline rings the bell.
func (l *Line) ding() { l.bell++ }

// Ding rings the bell, as a command does when it can do nothing.
func (l *Line) Ding() { l.ding() }

// insert puts text into the line at pos, and records it for undo.
func (l *Line) insert(pos int, text []byte) {
	if len(text) == 0 {
		return
	}
	l.buf = append(l.buf[:pos], append(append([]byte(nil), text...), l.buf[pos:]...)...)
	l.undo.recordInsert(pos, len(text), l.this == "self-insert")
}

// remove takes the bytes from start to end out of the line, records them for
// undo, and returns them.
func (l *Line) remove(start, end int) []byte {
	if start >= end {
		return nil
	}
	text := append([]byte(nil), l.buf[start:end]...)
	l.buf = append(l.buf[:start], l.buf[end:]...)
	l.undo.recordDelete(start, text)
	return text
}

// replace swaps the bytes from start to end for text, as one change to undo.
func (l *Line) replace(start, end int, text []byte) {
	l.remove(start, end)
	l.insert(start, text)
}

// set replaces the whole line, outside the undo list, as moving through the
// history does. The cursor goes to the end and the mark to the start.
func (l *Line) set(text string) {
	l.buf = []byte(text)
	l.point = len(l.buf)
	l.mark = 0
	l.undo = undoList{}
}

// nextChar returns where the character at pos ends.
func (l *Line) nextChar(pos int) int {
	if pos >= len(l.buf) {
		return len(l.buf)
	}
	_, n := utf8.DecodeRune(l.buf[pos:])
	return pos + n
}

// prevChar returns where the character before pos starts.
func (l *Line) prevChar(pos int) int {
	if pos <= 0 {
		return 0
	}
	_, n := utf8.DecodeLastRune(l.buf[:pos])
	return pos - n
}

// isWord says whether r is part of a word, which for readline is a letter or
// a digit. An underscore and a hyphen are not.
func isWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isBlank says whether r is a space or a tab, which is what separates words
// for unix-word-rubout.
func isBlank(r rune) bool { return r == ' ' || r == '\t' }

// runeAt returns the character at pos.
func (l *Line) runeAt(pos int) rune {
	r, _ := utf8.DecodeRune(l.buf[pos:])
	return r
}

// runeBefore returns the character before pos.
func (l *Line) runeBefore(pos int) rune {
	r, _ := utf8.DecodeLastRune(l.buf[:pos])
	return r
}

// wordEnd returns where the next word ends after pos: past anything that is
// not a word, then past the word.
func (l *Line) wordEnd(pos int) int {
	for pos < len(l.buf) && !isWord(l.runeAt(pos)) {
		pos = l.nextChar(pos)
	}
	for pos < len(l.buf) && isWord(l.runeAt(pos)) {
		pos = l.nextChar(pos)
	}
	return pos
}

// wordStart returns where the word before pos starts: back past anything
// that is not a word, then back past the word.
func (l *Line) wordStart(pos int) int {
	for pos > 0 && !isWord(l.runeBefore(pos)) {
		pos = l.prevChar(pos)
	}
	for pos > 0 && isWord(l.runeBefore(pos)) {
		pos = l.prevChar(pos)
	}
	return pos
}

// blankWordStart returns where the word before pos starts when only spaces
// and tabs separate words.
func (l *Line) blankWordStart(pos int) int {
	for pos > 0 && isBlank(l.runeBefore(pos)) {
		pos = l.prevChar(pos)
	}
	for pos > 0 && !isBlank(l.runeBefore(pos)) {
		pos = l.prevChar(pos)
	}
	return pos
}

// kill removes the bytes from start to end, puts them on the kill ring, and
// leaves the cursor where they were. The kill commands move the mark there
// too. A kill right after another kill joins it: text killed after the
// cursor goes after it, and text killed before the cursor goes in front.
func (l *Line) kill(start, end int) {
	l.killText(start, end)
	if start < end {
		l.mark = start
	}
}

// killText kills the bytes from start to end and leaves the mark alone, as
// deleting characters with a numeric argument does.
func (l *Line) killText(start, end int) {
	if start >= end {
		return
	}
	before := end <= l.point
	text := l.remove(start, end)
	l.kills.add(text, l.lastKill, before)
	l.point = start
	l.thisKill = true
}

// changeCase rewrites the text from start to end with fn, which maps one
// word to its new form.
func (l *Line) changeCase(start, end int, fn func([]byte) []byte) {
	if start >= end {
		return
	}
	text := fn(l.buf[start:end])
	if bytes.Equal(text, l.buf[start:end]) {
		return
	}
	l.replace(start, end, text)
}
