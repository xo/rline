package readline

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/xo/rline/key"
)

// Candidate is one completion of the text before the cursor. It replaces the
// bytes from Start to End, and End is at the cursor or after it.
type Candidate struct {
	Start, End int

	// Text goes into the line in place of the bytes from Start to End.
	Text string

	// Display is what a list of completions shows. An empty one shows Text.
	Display string

	// Suffix goes after Text when this is the only completion and the cursor
	// was at the end of the line, as readline adds a space.
	Suffix string
}

// A Completer returns the completions of the text before point in line.
type Completer func(line string, point int) []Candidate

// queryItems is how many completions readline lists without asking first,
// the default of its completion-query-items.
const queryItems = 100

// completerWordBreaks are the characters that end a word for completion,
// readline's default rl_completer_word_break_characters. export-completions
// finds the word with them when there is no completion to say where it is.
const completerWordBreaks = " \t\n\"\\'`@$><=;|&{("

// menu is the state of menu-complete between two presses: the completions,
// which one is in the line, and the stretch of the line that it fills.
type menu struct {
	cands      []Candidate
	index      int // len(cands) stands for the text that was there before
	start, end int
	original   string
	suffix     bool
	single     bool // one completion, which old-menu-complete keeps in place
}

// SetCompleter sets what the completion commands complete with.
func (l *Line) SetCompleter(c Completer) { l.completer = c }

// Listing returns the completions that a command asked to show below the
// line, and forgets them. It is empty when there is nothing to show.
func (l *Line) Listing() []Candidate {
	list := l.listing
	l.listing = nil
	return list
}

// Asking returns the question that the line is waiting for an answer to, such
// as whether to show a long list of completions. It is empty when the line
// is not waiting.
func (l *Line) Asking() string { return l.asking }

// candidates returns the completions at the cursor, without duplicates and
// sorted, as readline sorts them.
func (l *Line) candidates() []Candidate {
	if l.completer == nil {
		return nil
	}
	cands := l.completer(string(l.buf), l.point)
	slices.SortStableFunc(cands, func(a, b Candidate) int { return strings.Compare(a.Text, b.Text) })
	return slices.CompactFunc(cands, func(a, b Candidate) bool { return a.Text == b.Text })
}

// complete completes the word before the cursor. One completion goes in with
// its suffix. Several put in what they all start with. When that adds nothing,
// the bell rings, and a second complete straight after lists them.
// show-all-if-ambiguous lists them at once, and show-all-if-unmodified lists
// them at once when nothing went in. With disable-completion the key is typed
// instead.
func complete(l *Line, c call) {
	if l.vars.disableCompletion {
		l.insertKey(c.key, c.count)
		return
	}
	listNow := l.last == "complete" && !l.compChanged && !l.compFailed
	l.compChanged, l.compFailed = false, false
	cands := l.candidates()
	switch {
	case len(cands) == 0:
		l.compFailed = true
		l.ding()
	case listNow:
		l.list(cands)
	case len(cands) == 1:
		l.insertCandidate(cands[0], cands[0].Text, true)
		l.compChanged = true
	default:
		prefix, ok := sharedPrefix(cands)
		modified := ok && prefix != string(l.buf[cands[0].Start:cands[0].End])
		if modified {
			l.insertCandidate(cands[0], prefix, false)
			l.compChanged = true
		}
		switch {
		case l.vars.showAllIfAmbiguous, l.vars.showAllIfUnmodified && !modified:
			l.list(cands)
		case !modified:
			l.ding()
		}
	}
}

// possibleCompletions lists the completions, and changes nothing.
func possibleCompletions(l *Line, _ call) {
	cands := l.candidates()
	if len(cands) == 0 {
		l.ding()
		return
	}
	l.list(cands)
}

// insertCompletions puts every completion in place of the word, each followed
// by a space.
func insertCompletions(l *Line, _ call) {
	cands := l.candidates()
	if len(cands) == 0 {
		l.ding()
		return
	}
	var sb strings.Builder
	for _, c := range cands {
		sb.WriteString(c.Text)
		sb.WriteByte(' ')
	}
	l.insertCandidate(cands[0], sb.String(), false)
}

// menuComplete puts the next completion in place of the word, dir steps on.
// Pressed again at once, it steps through the completions, and after the last
// one puts back the text that was there before. A single completion goes in
// as complete puts it. With old, as old-menu-complete, a single completion
// stays in place when it is pressed again, where menu-complete completes the
// next word.
func menuComplete(l *Line, dir int, old bool) {
	if m := l.menu; m != nil && (l.last == "menu-complete" || l.last == "menu-complete-backward" ||
		l.last == "old-menu-complete") {
		n := len(m.cands) + 1
		if m.single {
			n = 1
		}
		m.index = ((m.index+dir)%n + n) % n
		l.fillMenu()
		return
	}
	l.menu = nil
	cands := l.candidates()
	switch {
	case len(cands) == 0:
		l.ding()
		return
	case len(cands) == 1 && !old:
		l.insertCandidate(cands[0], cands[0].Text, true)
		return
	}
	c := cands[0]
	m := &menu{
		cands:    cands,
		start:    c.Start,
		end:      c.End,
		original: string(l.buf[c.Start:c.End]),
		suffix:   c.End == len(l.buf),
		single:   len(cands) == 1,
	}
	if dir < 0 {
		m.index = len(cands) - 1
	}
	l.menu = m
	l.fillMenu()
}

// fillMenu puts the completion that the menu is on into the line.
func (l *Line) fillMenu() {
	m := l.menu
	text := m.original
	if m.index < len(m.cands) {
		c := m.cands[m.index]
		text = c.Text
		if m.suffix {
			text += c.Suffix
		}
	}
	l.replace(m.start, m.end, []byte(text))
	m.end = m.start + len(text)
	l.point = m.end
}

// insertCandidate puts text in place of the stretch that c replaces, and
// leaves the cursor after it. With suffix, c's suffix follows when the
// stretch reached the end of the line.
func (l *Line) insertCandidate(c Candidate, text string, suffix bool) {
	if suffix && c.End == len(l.buf) {
		text += c.Suffix
	}
	l.replace(c.Start, c.End, []byte(text))
	l.point = c.Start + len(text)
}

// list asks for the completions to be shown. A list as long as queryItems
// asks first, and waits for y or n.
func (l *Line) list(cands []Candidate) {
	if l.vars.queryItems <= 0 || len(cands) < l.vars.queryItems {
		l.listing = cands
		return
	}
	l.asking = fmt.Sprintf("Display all %d possibilities? (y or n)", len(cands))
	var answer func(k key.Code)
	answer = func(k key.Code) {
		switch k {
		case 'y', 'Y', ' ':
			l.listing = cands
		case 'n', 'N', key.Rubout, key.Bell:
		default:
			l.ding()
			l.readNext = answer
			return
		}
		l.asking = ""
	}
	l.readNext = answer
}

// sharedPrefix returns what the texts of the completions start with, and
// reports false when they do not all replace the same stretch.
func sharedPrefix(cands []Candidate) (string, bool) {
	prefix := cands[0].Text
	for _, c := range cands[1:] {
		if c.Start != cands[0].Start || c.End != cands[0].End {
			return "", false
		}
		n := 0
		for n < len(prefix) && n < len(c.Text) && prefix[n] == c.Text[n] {
			n++
		}
		// The texts can differ inside a character of several bytes, and
		// only whole characters are shared.
		for n > 0 && n < len(prefix) && !utf8.RuneStart(prefix[n]) {
			n--
		}
		prefix = prefix[:n]
	}
	return prefix, true
}
