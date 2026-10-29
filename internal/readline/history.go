package readline

import "strings"

// History is the list of earlier lines that the history commands walk.
//
// An edit to an earlier line is kept while the line is being read, so that
// moving away and back finds the edit, as readline does.
type History struct {
	entries []string

	pos   int            // the line being edited: len(entries) is the new one
	edits map[int]string // what each line was changed to while walking
}

// Add appends a line to the history.
func (h *History) Add(line string) {
	h.entries = append(h.entries, line)
}

// Entry returns the history line at pos, counted from the oldest, and reports
// false when there is none.
func (h *History) Entry(pos int) (string, bool) {
	if pos < 0 || pos >= len(h.entries) {
		return "", false
	}
	return h.entries[pos], true
}

// start puts the history at the new line, before a line is read.
func (h *History) start() {
	h.pos = len(h.entries)
	h.edits = make(map[int]string)
}

// line returns the text of the history line at pos, with its edits.
func (h *History) line(pos int) string {
	if text, ok := h.edits[pos]; ok {
		return text
	}
	if pos < len(h.entries) {
		return h.entries[pos]
	}
	return ""
}

// moveTo leaves the line being edited for the one at pos, keeping what was
// typed into the line it leaves. It reports false when pos is outside the
// history.
func (l *Line) moveTo(pos int) bool {
	h := l.history
	if pos < 0 || pos > len(h.entries) || pos == h.pos {
		return false
	}
	h.edits[h.pos] = string(l.buf)
	h.pos = pos
	l.set(h.line(pos))
	return true
}

// words splits an earlier line into its words, at spaces and tabs.
func words(line string) []string {
	return strings.Fields(line)
}

// previousWords returns the words of the history line back lines before the
// line being edited.
func (l *Line) previousWords(back int) ([]string, bool) {
	pos := l.history.pos - back
	if pos < 0 || pos >= len(l.history.entries) {
		return nil, false
	}
	return words(l.history.entries[pos]), true
}
