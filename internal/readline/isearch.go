package readline

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/xo/rline/key"
)

// isearch is the state of an incremental search: what is searched for, which
// way, where the last match is, and the line as it was before the search.
type isearch struct {
	query   string
	reverse bool
	failed  bool

	// The history line that holds the match, and where in it the search
	// stands. pos counts like History.pos: len(entries) is the line that
	// was being edited.
	pos, index int

	// The line, the cursor and the mark before the search, and where in the
	// history the line was.
	origText         string
	origPoint, origM int
	origPos          int

	// found is the text of the last match, which the search skips when it
	// moves on to other lines, so that the same text is not found twice, and
	// foundPos is its history line. foundPos is -1 before a match.
	found    string
	foundPos int
}

// Prompt returns the prompt that the line shows in place of the program's
// own, such as the prompt of an incremental search. It is empty when the
// program's prompt is shown.
//
// readline shows one while a numeric argument is typed as well, such as
// "(arg: 3) ".
func (l *Line) Prompt() string {
	s := l.searching
	if s == nil {
		if r := l.reading; r != nil {
			return r.prompt
		}
		if l.arg.shown() {
			n, _ := l.arg.count()
			return fmt.Sprintf("(arg: %d) ", n)
		}
		return ""
	}
	var sb strings.Builder
	sb.WriteByte('(')
	if s.failed {
		sb.WriteString("failed ")
	}
	if s.reverse {
		sb.WriteString("reverse-")
	}
	sb.WriteString("i-search)`")
	sb.WriteString(s.query)
	sb.WriteString("': ")
	return sb.String()
}

// SearchString returns the text that the last incremental search looked for,
// which a search that starts with nothing looks for again.
func (l *Line) SearchString() string { return l.lastSearch }

// SetSearchString sets the text that a search that starts with nothing looks
// for, so that a program can carry it from one line to the next.
func (l *Line) SetSearchString(s string) { l.lastSearch = s }

// startSearch starts an incremental search, backward when reverse is set.
func (l *Line) startSearch(reverse bool) {
	l.searching = &isearch{
		reverse:   reverse,
		pos:       l.history.pos,
		index:     l.point,
		origText:  string(l.buf),
		origPoint: l.point,
		origM:     l.mark,
		origPos:   l.history.pos,
		foundPos:  -1,
	}
}

// searchLine returns the text of the history line at pos as the search sees
// it. The line being edited is the text from before the search.
func (l *Line) searchLine(pos int) string {
	if pos == l.searching.origPos {
		return l.searching.origText
	}
	return l.history.line(pos)
}

// searchKey reads one key during an incremental search.
func (l *Line) searchKey(c key.Code) {
	seq := append(append([]key.Code(nil), l.pending...), c)
	name, prefix := l.keymap.lookup(seq)
	if prefix && c != key.Esc {
		l.pending = seq
		return
	}
	l.pending = nil
	s := l.searching
	switch {
	case len(seq) == 1 && containsKey(l.vars.isearchTerminators, c):
		l.endSearch()
	case name == "reverse-search-history" || name == "forward-search-history":
		reverse := name == "reverse-search-history"
		if reverse != s.reverse {
			// A change of direction searches again from the match, the
			// other way, so it can stay on the same match.
			s.reverse = reverse
			l.runSearch()
			return
		}
		if s.query == "" {
			if l.lastSearch != "" {
				s.query = l.lastSearch
				l.runSearch()
			}
			return
		}
		switch {
		case s.reverse && s.index >= 0:
			s.index--
		case !s.reverse && s.index != len(l.searchLine(s.pos)):
			s.index++
		default:
			l.ding()
		}
		l.runSearch()
	case name == "backward-delete-char":
		if s.query == "" {
			l.ding()
		} else {
			_, n := utf8.DecodeLastRuneInString(s.query)
			s.query = s.query[:len(s.query)-n]
		}
		if s.query == "" {
			l.ding()
		}
		l.runSearch()
	case name == "abort" || c == key.Bell:
		l.buf = []byte(s.origText)
		l.point, l.mark = s.origPoint, s.origM
		l.searching = nil
	case name == "self-insert" || name == "" && len(seq) == 1 && c >= 0x80 && c <= key.UnicodeMax:
		s.query += string(rtextRune(c))
		l.runSearch()
	default:
		// Any other key ends the search where it stands, and then runs as
		// it would have.
		l.endSearch()
		for _, k := range seq {
			l.Key(k)
		}
	}
}

// rtextRune returns the character that typing c gives.
func rtextRune(c key.Code) rune { return rune(c &^ key.ModMask) }

// containsKey reports whether keys holds c.
func containsKey(keys []key.Code, c key.Code) bool {
	for _, k := range keys {
		if k == c {
			return true
		}
	}
	return false
}

// runSearch looks for the query from where the search stands, through the
// history in the direction of the search, and shows what it finds. A search
// that finds nothing rings the bell and leaves the last match in the line.
func (l *Line) runSearch() {
	s := l.searching
	s.failed = false
	if s.query == "" {
		s.failed = true
		return
	}
	line := l.searchLine(s.pos)
	for {
		if i, ok := s.match(line); ok {
			s.index = i
			s.found, s.foundPos = line, s.pos
			l.buf = []byte(line)
			l.point = i
			l.mark = i + len(s.query)
			return
		}
		// The next line, past lines too short to hold the query and past
		// the line of the last match.
		for {
			if s.reverse {
				s.pos--
			} else {
				s.pos++
			}
			if s.pos < 0 || s.pos > len(l.history.entries) {
				s.failed = true
				s.pos = s.foundPos
				if s.pos < 0 {
					s.pos = s.origPos
				}
				l.ding()
				return
			}
			line = l.searchLine(s.pos)
			if (s.foundPos < 0 || line != s.found) && len(line) >= len(s.query) {
				break
			}
		}
		if s.reverse {
			s.index = len(line) - len(s.query)
		} else {
			s.index = 0
		}
	}
}

// match looks for the query in line, from the index of the search, in its
// direction.
func (s *isearch) match(line string) (int, bool) {
	if s.reverse {
		for i := min(s.index, len(line)); i >= 0; i-- {
			if strings.HasPrefix(line[i:], s.query) {
				return i, true
			}
		}
		return 0, false
	}
	for i := max(s.index, 0); i+len(s.query) <= len(line); i++ {
		if strings.HasPrefix(line[i:], s.query) {
			return i, true
		}
	}
	return 0, false
}

// endSearch ends the search and leaves the line at the last match, as the
// line of the history it came from, with the cursor where the match starts.
// A search that found nothing leaves the line as it was.
func (l *Line) endSearch() {
	s := l.searching
	l.searching = nil
	if s.query != "" {
		l.lastSearch = s.query
	}
	if s.foundPos < 0 {
		return
	}
	point := l.point
	// Moving to the history line keeps what was typed into the line being
	// edited, which is the text from before the search.
	l.buf = []byte(s.origText)
	if s.foundPos != l.history.pos {
		l.moveTo(s.foundPos)
	} else {
		l.set(s.found)
	}
	l.point = min(point, len(l.buf))
}
