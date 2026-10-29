package readline

import (
	"bytes"
	"unicode"
	"unicode/utf8"

	rtext "github.com/xo/rline/internal/text"
	"github.com/xo/rline/key"
)

// Has reports whether this package has the command called name.
func Has(name string) bool { return commands[name] != nil }

// command is one of readline's commands, by what it does to the line.
type command func(l *Line, c call)

// commands are the commands this package has, by readline's names. A name
// that a keymap binds and that is missing here does nothing and rings the
// bell, as an unbound key does.
//
// The map is filled in init, because do-lowercase-version runs another
// command, and a map that refers to itself cannot be a package variable.
var commands map[string]command

func init() {
	commands = map[string]command{
		// Moving.
		"beginning-of-line": func(l *Line, _ call) { l.point = 0 },
		"end-of-line":       func(l *Line, _ call) { l.point = len(l.buf) },
		"forward-char":      func(l *Line, c call) { l.moveChars(c.count) },
		"backward-char":     func(l *Line, c call) { l.moveChars(-c.count) },
		"forward-word":      func(l *Line, c call) { l.moveWords(c.count) },
		"backward-word":     func(l *Line, c call) { l.moveWords(-c.count) },

		// Typing.
		"self-insert":   func(l *Line, c call) { l.insertKey(c.key, c.count) },
		"tab-insert":    func(l *Line, c call) { l.insertKey('\t', c.count) },
		"quoted-insert": quotedInsert,

		// Deleting and killing.
		"delete-char":             deleteChar,
		"backward-delete-char":    backwardDeleteChar,
		"kill-line":               killLine,
		"backward-kill-line":      func(l *Line, _ call) { l.kill(0, l.point) },
		"unix-line-discard":       func(l *Line, _ call) { l.kill(0, l.point) },
		"kill-whole-line":         func(l *Line, _ call) { l.point = 0; l.kill(0, len(l.buf)) },
		"kill-word":               killWord,
		"backward-kill-word":      func(l *Line, c call) { killWord(l, call{count: -c.count}) },
		"unix-word-rubout":        unixWordRubout,
		"delete-horizontal-space": deleteHorizontalSpace,

		// Yanking.
		"yank":          yank,
		"yank-pop":      yankPop,
		"yank-last-arg": yankLastArg,
		"yank-nth-arg":  yankNthArg,

		// Changing text.
		"transpose-chars": transposeChars,
		"transpose-words": transposeWords,
		"upcase-word":     func(l *Line, c call) { l.caseWords(c.count, upcase) },
		"downcase-word":   func(l *Line, c call) { l.caseWords(c.count, downcase) },
		"capitalize-word": func(l *Line, c call) { l.caseWords(c.count, capitalize) },

		// The numeric argument.
		"digit-argument": func(l *Line, c call) {
			l.arg = c.arg
			l.arg.add(c.key)
			l.this, l.thisKill = l.last, l.lastKill
		},

		// Undo.
		"undo": func(l *Line, c call) {
			for i := 0; i < max(c.count, 1); i++ {
				if !l.undoOne() {
					l.ding()
					return
				}
			}
		},
		"revert-line": func(l *Line, _ call) {
			for l.undoOne() {
			}
			l.mark = 0
		},

		// The mark.
		"set-mark": func(l *Line, c call) {
			l.mark = l.point
			if c.explicit {
				l.mark = min(max(c.count, 0), len(l.buf))
			}
		},
		"exchange-point-and-mark": func(l *Line, _ call) {
			l.point, l.mark = min(l.mark, len(l.buf)), l.point
		},

		// History.
		"previous-history":     func(l *Line, c call) { l.walkHistory(-c.count) },
		"next-history":         func(l *Line, c call) { l.walkHistory(c.count) },
		"beginning-of-history": func(l *Line, _ call) { l.historyTo(0) },
		"end-of-history":       func(l *Line, _ call) { l.historyTo(len(l.history.entries)) },

		// Incremental search.
		"reverse-search-history": func(l *Line, _ call) { l.startSearch(true) },
		"forward-search-history": func(l *Line, _ call) { l.startSearch(false) },

		// Completion.
		"complete":               complete,
		"possible-completions":   possibleCompletions,
		"insert-completions":     insertCompletions,
		"menu-complete":          func(l *Line, c call) { menuComplete(l, c.count, false) },
		"menu-complete-backward": func(l *Line, c call) { menuComplete(l, -c.count, false) },

		// Ending the line.
		"accept-line": func(l *Line, _ call) { l.done = true },
		"insert-comment": func(l *Line, _ call) {
			l.insert(0, []byte(l.vars.commentBegin))
			l.done = true
		},

		// Commands that act on keys rather than text.
		"abort": func(l *Line, _ call) { l.ding() },
		"do-lowercase-version": func(l *Line, c call) {
			l.Key(key.Code(unicode.ToLower(rune(c.key))))
		},
		// Drawing is the renderer's, so these change nothing in the line.
		"clear-screen":          func(*Line, call) {},
		"redraw-current-line":   func(*Line, call) {},
		"bracketed-paste-begin": func(l *Line, _ call) { l.ding() },
	}
}

// moveChars moves the cursor n characters, forward when n is positive. It
// rings the bell at either end of the line.
func (l *Line) moveChars(n int) {
	for ; n > 0; n-- {
		if l.point >= len(l.buf) {
			l.ding()
			return
		}
		l.point = l.nextChar(l.point)
	}
	for ; n < 0; n++ {
		if l.point <= 0 {
			l.ding()
			return
		}
		l.point = l.prevChar(l.point)
	}
}

// moveWords moves the cursor to the end of the next word, n times, or back to
// the start of the previous word when n is negative.
func (l *Line) moveWords(n int) {
	l.point = l.wordsFrom(l.point, n)
}

// wordsFrom returns where n words from pos end, or start when n is negative.
func (l *Line) wordsFrom(pos, n int) int {
	for ; n > 0; n-- {
		pos = l.wordEnd(pos)
	}
	for ; n < 0; n++ {
		pos = l.wordStart(pos)
	}
	return pos
}

// insertKey types the character of c n times at the cursor. In overwrite
// mode each one replaces the character at the cursor, until the end of the
// line.
func (l *Line) insertKey(c key.Code, n int) {
	if n <= 0 {
		return
	}
	// A byte that was not UTF-8 arrives as a code point of the raw plane, and
	// goes back into the line as that byte, as rline's editor stores it.
	text := bytes.Repeat(rtext.AppendRune(nil, rune(c&^key.ModMask)), n)
	if l.overwrite {
		end := l.point
		for i := 0; i < n && end < len(l.buf); i++ {
			end = l.nextChar(end)
		}
		l.replace(l.point, end, text)
		l.point += len(text)
		return
	}
	l.insert(l.point, text)
	l.point += len(text)
}

// quotedInsert types the next key into the line as it is, whatever it is
// bound to, as many times as the argument says.
func quotedInsert(l *Line, c call) {
	l.readNext = func(k key.Code) {
		l.this, l.thisKill = "quoted-insert", false
		l.undo.begin()
		l.insertKey(k, c.count)
		l.undo.end()
		l.last, l.lastKill = l.this, l.thisKill
	}
}

// deleteChar deletes the character at the cursor. With a numeric argument it
// kills that many characters, which puts them on the kill ring and leaves the
// mark where it was.
func deleteChar(l *Line, c call) {
	if c.count < 0 {
		backwardDeleteChar(l, call{count: -c.count, explicit: c.explicit})
		return
	}
	if l.point >= len(l.buf) {
		l.ding()
		return
	}
	end := l.point
	for i := 0; i < c.count && end < len(l.buf); i++ {
		end = l.nextChar(end)
	}
	if c.explicit {
		l.killText(l.point, end)
		return
	}
	l.remove(l.point, end)
}

// backwardDeleteChar deletes the character before the cursor. With a numeric
// argument it kills that many characters.
func backwardDeleteChar(l *Line, c call) {
	if c.count < 0 {
		deleteChar(l, call{count: -c.count, explicit: c.explicit})
		return
	}
	if l.point <= 0 {
		l.ding()
		return
	}
	start := l.point
	for i := 0; i < c.count && start > 0; i++ {
		start = l.prevChar(start)
	}
	if c.explicit {
		l.killText(start, l.point)
		return
	}
	// In overwrite mode the characters become spaces, and the cursor moves
	// back over them.
	if l.overwrite {
		l.replace(start, l.point, bytes.Repeat([]byte(" "), utf8.RuneCount(l.buf[start:l.point])))
		l.point = start
		return
	}
	l.remove(start, l.point)
	l.point = start
}

// killLine kills from the cursor to the end of the line, or to its start when
// the argument is negative.
func killLine(l *Line, c call) {
	if c.count < 0 {
		l.kill(0, l.point)
		return
	}
	l.kill(l.point, len(l.buf))
}

// killWord kills to the end of the next word, or back to the start of the
// previous word when the argument is negative.
func killWord(l *Line, c call) {
	pos := l.wordsFrom(l.point, c.count)
	if pos < l.point {
		l.kill(pos, l.point)
		return
	}
	l.kill(l.point, pos)
}

// unixWordRubout kills the word before the cursor, where only spaces and tabs
// separate words.
func unixWordRubout(l *Line, c call) {
	start := l.point
	for i := 0; i < max(c.count, 1); i++ {
		start = l.blankWordStart(start)
	}
	if start == l.point {
		l.ding()
		return
	}
	l.kill(start, l.point)
}

// deleteHorizontalSpace deletes the spaces and tabs around the cursor.
func deleteHorizontalSpace(l *Line, _ call) {
	start, end := l.point, l.point
	for start > 0 && isBlank(l.runeBefore(start)) {
		start = l.prevChar(start)
	}
	for end < len(l.buf) && isBlank(l.runeAt(end)) {
		end = l.nextChar(end)
	}
	l.remove(start, end)
	l.point = start
}

// yank inserts the kill at the top of the ring. The mark goes to the start of
// what was inserted and the cursor to its end, so that yank-pop can find it.
func yank(l *Line, _ call) {
	text, ok := l.kills.current()
	if !ok {
		l.ding()
		return
	}
	l.mark = l.point
	l.insert(l.point, text)
	l.point += len(text)
}

// yankPop replaces what the last yank inserted with the next older kill. It
// does nothing unless the command before it was a yank.
func yankPop(l *Line, _ call) {
	if l.last != "yank" && l.last != "yank-pop" {
		l.ding()
		return
	}
	start := min(l.mark, l.point)
	l.remove(start, max(l.mark, l.point))
	l.kills.rotate()
	text, _ := l.kills.current()
	l.insert(start, text)
	l.mark, l.point = start, start+len(text)
}

// yankLastArg inserts the last word of the previous history line. Run again
// at once, it replaces that word with the last word of the line before.
func yankLastArg(l *Line, c call) {
	back := 1
	if l.last == "yank-last-arg" {
		back = l.lastArgBack + 1
		l.remove(l.lastArgStart, l.lastArgEnd)
		l.point = l.lastArgStart
	}
	ws, ok := l.previousWords(back)
	if !ok || len(ws) == 0 {
		l.ding()
		return
	}
	word := ws[len(ws)-1]
	if c.explicit && c.count >= 0 && c.count < len(ws) {
		word = ws[c.count]
	}
	start := l.point
	l.insert(start, []byte(word))
	l.point = start + len(word)
	l.mark = start
	l.lastArgStart, l.lastArgEnd, l.lastArgBack = start, l.point, back
}

// yankNthArg inserts word n of the previous history line, counting from 0,
// where n is the argument and 1 without one.
func yankNthArg(l *Line, c call) {
	ws, ok := l.previousWords(1)
	n := c.count
	if !ok || n < 0 || n >= len(ws) {
		l.ding()
		return
	}
	start := l.point
	l.insert(start, []byte(ws[n]))
	l.point = start + len(ws[n])
}

// transposeChars drags the character before the cursor forward over the one
// at the cursor, and moves the cursor past both. At the end of the line it
// swaps the two characters before the cursor.
func transposeChars(l *Line, _ call) {
	if l.point >= len(l.buf) {
		if l.point == 0 {
			l.ding()
			return
		}
		mid := l.prevChar(l.point)
		start := l.prevChar(mid)
		if start == mid {
			l.ding()
			return
		}
		a := l.remove(start, mid)
		l.insert(start+(l.point-mid), a)
		return
	}
	if l.point == 0 {
		l.ding()
		return
	}
	start := l.prevChar(l.point)
	end := l.nextChar(l.point)
	a := l.remove(start, l.point)
	l.insert(start+(end-l.point), a)
	l.point = end
}

// transposeWords swaps the word before the cursor with the word after it,
// and moves the cursor past both. At the end of the line it swaps the last
// two words.
func transposeWords(l *Line, _ call) {
	w2End := l.wordEnd(l.point)
	w2Start := l.wordStart(w2End)
	w1Start := l.wordStart(w2Start)
	w1End := l.wordEnd(w1Start)
	if w1Start == w2Start || w2Start == w2End || w1End > w2Start {
		l.ding()
		return
	}
	w1 := append([]byte(nil), l.buf[w1Start:w1End]...)
	w2 := append([]byte(nil), l.buf[w2Start:w2End]...)
	between := append([]byte(nil), l.buf[w1End:w2Start]...)
	text := append(append(w2, between...), w1...)
	l.replace(w1Start, w2End, text)
	l.point = w2End
}

// caseWords rewrites the next n words with fn and moves past them. With a
// negative count it rewrites the words before the cursor and leaves the
// cursor where it is.
func (l *Line) caseWords(n int, fn func([]byte) []byte) {
	if n < 0 {
		start := l.wordsFrom(l.point, n)
		l.changeCase(start, l.point, fn)
		return
	}
	pos := l.point
	for i := 0; i < n; i++ {
		end := l.wordEnd(pos)
		l.changeCase(pos, end, fn)
		pos = l.wordEnd(pos)
	}
	l.point = pos
}

// upcase returns text in upper case.
func upcase(text []byte) []byte { return bytes.ToUpper(text) }

// downcase returns text in lower case.
func downcase(text []byte) []byte { return bytes.ToLower(text) }

// capitalize returns text with the first letter or digit of its word in
// upper case and the rest of the word in lower case.
func capitalize(text []byte) []byte {
	var out []byte
	first := true
	for _, r := range string(text) {
		switch {
		case !isWord(r):
		case first:
			r = unicode.ToUpper(r)
			first = false
		default:
			r = unicode.ToLower(r)
		}
		out = utf8.AppendRune(out, r)
	}
	return out
}

// walkHistory moves n lines through the history, back when n is negative.
func (l *Line) walkHistory(n int) {
	if !l.moveTo(l.history.pos + n) {
		target := min(max(l.history.pos+n, 0), len(l.history.entries))
		if !l.moveTo(target) {
			l.ding()
		}
	}
}

// historyTo moves to the history line at pos.
func (l *Line) historyTo(pos int) {
	if !l.moveTo(pos) {
		l.ding()
	}
}
