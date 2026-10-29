package readline

import (
	"unicode"
	"unicode/utf8"

	"github.com/xo/rline/key"
)

// The vi modes of readline: insert mode, command mode, the operators that
// take a motion, and vi's commands. They are written from readline's manual
// and from recordings of readline 8.3, as the rest of this package is.

// editMode is the keymap that the line reads its keys through.
type editMode int

const (
	modeEmacs editMode = iota
	modeViInsert
	modeViCommand
)

// viOperator is an operator, d, c or y, that waits for its motion.
type viOperator struct {
	op       byte // 'd', 'c' or 'y'
	count    int  // the count typed before the operator
	motion   int  // the count typed after it, 0 for none
	start    int  // where the cursor was
	spaceEnd bool // a space was put at the end of the line for f and t
}

// viSearch is the last f, F, t or T, which ; and , repeat.
type viSearch struct {
	kind byte
	char rune
}

// SetKeymaps sets the keymaps of the three editing modes: emacs, vi insert
// and vi command.
func (l *Line) SetKeymaps(emacs, viInsert, viCommand *Keymap) {
	l.keymaps = [3]*Keymap{emacs, viInsert, viCommand}
	l.keymap = l.keymaps[l.mode]
}

// SetEditingMode makes the line edit as vi does, in insert mode, or as emacs
// does, as readline's editing-mode does.
func (l *Line) SetEditingMode(vi bool) {
	if vi {
		l.setMode(modeViInsert)
		l.viInitial = true
		return
	}
	l.setMode(modeEmacs)
}

// Mode returns the keymap that the line reads its keys through: "emacs",
// "vi-insert" or "vi-command".
func (l *Line) Mode() string {
	switch l.mode {
	case modeViInsert:
		return "vi-insert"
	case modeViCommand:
		return "vi-command"
	}
	return "emacs"
}

// setMode changes the editing mode and its keymap.
func (l *Line) setMode(m editMode) {
	l.mode = m
	if km := l.keymaps[m]; km != nil {
		l.keymap = km
	}
}

// startInsert enters vi insert mode for a command, such as i or c. What is
// typed until Esc is one change, for undo and for the redo command.
func (l *Line) startInsert() {
	l.setMode(modeViInsert)
	l.undo.join, l.undo.joined = true, false
	l.viChange = true
}

// viClamp keeps the cursor on a character in vi command mode, where it cannot
// sit after the last one.
func (l *Line) viClamp() {
	if l.mode != modeViCommand || l.done {
		return
	}
	if l.viNoClamp {
		l.viNoClamp = false
		return
	}
	if len(l.buf) > 0 && l.point >= len(l.buf) {
		l.point = l.prevChar(len(l.buf))
	}
}

// viClass returns the kind of the character r for vi's word motions: 0 for a
// blank, 1 for a letter, digit or underscore, and 2 for anything else. With
// big, as W, B and E are, only blanks and the rest are told apart.
func viClass(r rune, big bool) int {
	switch {
	case r == ' ' || r == '\t':
		return 0
	case big, r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return 1
	}
	return 2
}

// viNextWord returns where the next vi word starts after pos, n times.
func (l *Line) viNextWord(pos, n int, big bool) int {
	for ; n > 0 && pos < len(l.buf); n-- {
		if c := viClass(l.runeAt(pos), big); c != 0 {
			for pos < len(l.buf) && viClass(l.runeAt(pos), big) == c {
				pos = l.nextChar(pos)
			}
		}
		for pos < len(l.buf) && viClass(l.runeAt(pos), big) == 0 {
			pos = l.nextChar(pos)
		}
	}
	return pos
}

// viPrevWord returns where the vi word before pos starts, n times.
func (l *Line) viPrevWord(pos, n int, big bool) int {
	for ; n > 0 && pos > 0; n-- {
		pos = l.prevChar(pos)
		for pos > 0 && viClass(l.runeAt(pos), big) == 0 {
			pos = l.prevChar(pos)
		}
		c := viClass(l.runeAt(pos), big)
		for pos > 0 && viClass(l.runeBefore(pos), big) == c {
			pos = l.prevChar(pos)
		}
	}
	return pos
}

// viEndWord returns where the vi word after pos ends, at its last character,
// n times.
func (l *Line) viEndWord(pos, n int, big bool) int {
	for ; n > 0; n-- {
		if l.nextChar(pos) >= len(l.buf) {
			break
		}
		pos = l.nextChar(pos)
		for l.nextChar(pos) < len(l.buf) && viClass(l.runeAt(pos), big) == 0 {
			pos = l.nextChar(pos)
		}
		c := viClass(l.runeAt(pos), big)
		for l.nextChar(pos) < len(l.buf) && viClass(l.runeAt(l.nextChar(pos)), big) == c {
			pos = l.nextChar(pos)
		}
	}
	return pos
}

// viCharSearch returns where a search of the kind f, F, t or T for r lands
// from pos, n times, and reports false when there is no such character.
func (l *Line) viCharSearch(kind byte, r rune, pos, n int) (int, bool) {
	forward := kind == 'f' || kind == 't'
	for ; n > 0; n-- {
		p := pos
		switch kind {
		case 't':
			p = l.nextChar(p)
		case 'T':
			p = l.prevChar(p)
		}
		found := -1
		if forward {
			for q := l.nextChar(p); q < len(l.buf); q = l.nextChar(q) {
				if l.runeAt(q) == r {
					found = q
					break
				}
			}
		} else {
			for q := p; q > 0; {
				q = l.prevChar(q)
				if l.runeAt(q) == r {
					found = q
					break
				}
			}
		}
		if found < 0 {
			return 0, false
		}
		pos = found
		switch kind {
		case 't':
			pos = l.prevChar(found)
		case 'T':
			pos = l.nextChar(found)
		}
	}
	return pos, true
}

// viReverse returns the search that goes the other way, for ,.
func viReverse(kind byte) byte {
	switch kind {
	case 'f':
		return 'F'
	case 'F':
		return 'f'
	case 't':
		return 'T'
	}
	return 't'
}

// viMatch returns where the bracket that matches the first bracket at or
// after pos is, and reports false when there is none.
func (l *Line) viMatch(pos int) (int, bool) {
	const brackets = "()[]{}"
	for pos < len(l.buf) && !containsRune(brackets, l.runeAt(pos)) {
		pos = l.nextChar(pos)
	}
	if pos >= len(l.buf) {
		return 0, false
	}
	open := l.runeAt(pos)
	i := indexRune(brackets, open)
	partner := rune(brackets[i^1])
	dir := 1
	if i%2 == 1 {
		dir = -1
	}
	depth := 0
	for q := pos; q >= 0 && q < len(l.buf); {
		switch l.runeAt(q) {
		case open:
			depth++
		case partner:
			depth--
			if depth == 0 {
				return q, true
			}
		}
		if dir > 0 {
			q = l.nextChar(q)
		} else {
			if q == 0 {
				break
			}
			q = l.prevChar(q)
		}
	}
	return 0, false
}

func containsRune(s string, r rune) bool { return indexRune(s, r) >= 0 }

func indexRune(s string, r rune) int {
	for i, c := range s {
		if c == r {
			return i
		}
	}
	return -1
}

// viColumn returns the position of column n, counting from 1, or the last
// character when the line is shorter.
func (l *Line) viColumn(n int) int {
	pos := 0
	for i := 1; i < n && l.nextChar(pos) < len(l.buf); i++ {
		pos = l.nextChar(pos)
	}
	return pos
}

// viFirstPrint returns where the first character that is not a blank is.
func (l *Line) viFirstPrint() int {
	pos := 0
	for pos < len(l.buf) && isBlank(l.runeAt(pos)) {
		pos = l.nextChar(pos)
	}
	return pos
}

// viMotion returns where the motion bound to the key k takes the cursor from
// pos, n times, and whether the character there is part of what an operator
// acts on. It reports false for a key that is no motion, or a motion that
// cannot move. f, F, t and T, which read a character, are not here.
func (l *Line) viMotion(name string, k key.Code, pos, n int) (int, bool, bool) {
	big := k >= 'A' && k <= 'Z'
	switch name {
	case "backward-char":
		for i := 0; i < n && pos > 0; i++ {
			pos = l.prevChar(pos)
		}
		return pos, false, true
	case "forward-char":
		for i := 0; i < n && pos < len(l.buf); i++ {
			pos = l.nextChar(pos)
		}
		return pos, false, true
	case "vi-next-word":
		return l.viNextWord(pos, n, big), false, true
	case "vi-prev-word":
		return l.viPrevWord(pos, n, big), false, true
	case "vi-end-word":
		return l.viEndWord(pos, n, big), true, true
	case "beginning-of-line":
		return 0, false, true
	case "end-of-line":
		return len(l.buf), false, true
	case "vi-first-print":
		return l.viFirstPrint(), false, true
	case "vi-column":
		return l.viColumn(n), false, true
	case "vi-match":
		q, ok := l.viMatch(pos)
		return q, true, ok
	case "vi-char-search":
		s := l.viLastSearch
		if s.kind == 0 || (k != ';' && k != ',') {
			return 0, false, false
		}
		kind := s.kind
		if k == ',' {
			kind = viReverse(kind)
		}
		q, ok := l.viCharSearch(kind, s.char, pos, n)
		return q, kind == 'f' || kind == 't', ok
	}
	return 0, false, false
}

// viOpKey reads a key while an operator waits for its motion.
func (l *Line) viOpKey(c key.Code) {
	op := l.viOp
	switch {
	case c >= '1' && c <= '9', c == '0' && op.motion > 0:
		op.motion = op.motion*10 + int(c-'0')
		return
	case c == key.Code(op.op):
		// dd, cc and yy act on the whole line.
		l.viOp = nil
		l.viWholeLine(op.op)
		return
	}
	n := max(op.count, 1) * max(op.motion, 1)
	name, _ := l.keymap.lookup([]key.Code{c})
	if name == "vi-char-search" && c != ';' && c != ',' {
		// readline puts a space at the end of the line while it waits for
		// the character, so that a search can reach the end.
		l.buf = append(l.buf, ' ')
		op.spaceEnd = true
		l.readNext = func(r key.Code) {
			l.buf = l.buf[:len(l.buf)-1]
			l.viOp = nil
			l.viLastSearch = viSearch{kind: byte(c), char: rune(r &^ key.ModMask)}
			q, ok := l.viCharSearch(byte(c), l.viLastSearch.char, op.start, n)
			if !ok {
				l.ding()
				return
			}
			l.viApply(op, q, c == 'f' || c == 't')
		}
		return
	}
	l.viOp = nil
	if op.op == 'c' && name == "vi-next-word" {
		// cw changes to the end of the word, as ce does. On a blank it
		// changes the blank.
		if op.start < len(l.buf) && viClass(l.runeAt(op.start), false) != 0 {
			name = "vi-end-word"
		} else {
			l.viApply(op, op.start, true)
			return
		}
	}
	q, inclusive, ok := l.viMotion(name, c, op.start, n)
	if !ok {
		l.ding()
		return
	}
	l.viApply(op, q, inclusive)
}

// viApply runs an operator over the text from where it started to q. With
// inclusive, the character at q is part of it when q is after the start.
func (l *Line) viApply(op *viOperator, q int, inclusive bool) {
	lo, hi := op.start, q
	if q < op.start {
		lo, hi = q, op.start
	} else if inclusive && q < len(l.buf) {
		hi = l.nextChar(q)
	}
	l.undo.begin()
	defer l.undo.end()
	switch op.op {
	case 'd', 'c':
		l.kill(lo, hi)
		l.viChange = true
	case 'y':
		l.copy(lo, hi)
	}
	l.point = lo
	l.mark = min(hi, len(l.buf))
	if op.op == 'c' {
		l.startInsert()
	}
	l.viClamp()
}

// viWholeLine runs an operator over the whole line, as dd, cc and yy do.
func (l *Line) viWholeLine(op byte) {
	l.undo.begin()
	defer l.undo.end()
	switch op {
	case 'd', 'c':
		l.point = 0
		l.kill(0, len(l.buf))
		l.viChange = true
	case 'y':
		l.copy(0, len(l.buf))
	}
	l.mark = 0
	if op == 'c' {
		l.startInsert()
	}
	l.viClamp()
}

// viOperatorCommand starts the operator of key c: d, c or y wait for a motion,
// and D, C and Y act on the rest of the line at once.
func viOperatorCommand(l *Line, c call, op byte) {
	if c.key >= 'A' && c.key <= 'Z' {
		lo := l.point
		switch op {
		case 'd', 'c':
			l.kill(lo, len(l.buf))
			l.viChange = true
		case 'y':
			l.copy(lo, len(l.buf))
		}
		if op == 'c' {
			l.startInsert()
		}
		return
	}
	count := 0
	if c.explicit {
		count = c.count
	}
	l.viOp = &viOperator{op: op, count: count, start: l.point}
	l.mark = l.point
}

// The commands of vi mode.
func init() {
	for name, cmd := range map[string]command{
		"vi-movement-mode": func(l *Line, _ call) {
			if l.mode != modeViInsert {
				l.ding()
				return
			}
			if l.overwrite {
				// After R the cursor stays where the typing left it.
				l.overwrite = false
				l.viNoClamp = true
			} else if l.point > 0 {
				l.point = l.prevChar(l.point)
			}
			l.setMode(modeViCommand)
			l.undo.join = false
			if l.viInitial {
				// What was typed before the first Esc is not one change,
				// and readline's undo cannot take it back.
				l.undo = undoList{}
				l.viInitial = false
			}
		},
		"vi-insertion-mode": func(l *Line, _ call) { l.startInsert() },
		"vi-append-mode": func(l *Line, _ call) {
			if l.point < len(l.buf) {
				l.point = l.nextChar(l.point)
			}
			l.startInsert()
		},
		"vi-append-eol": func(l *Line, _ call) {
			l.point = len(l.buf)
			l.startInsert()
		},
		"vi-insert-beg": func(l *Line, _ call) {
			l.point = 0
			l.startInsert()
		},
		"vi-next-word": func(l *Line, c call) {
			l.point = l.viNextWord(l.point, c.count, c.key == 'W')
		},
		"vi-prev-word": func(l *Line, c call) {
			l.point = l.viPrevWord(l.point, c.count, c.key == 'B')
		},
		"vi-end-word": func(l *Line, c call) {
			l.point = l.viEndWord(l.point, c.count, c.key == 'E')
		},
		"vi-first-print": func(l *Line, _ call) { l.point = l.viFirstPrint() },
		"vi-column":      func(l *Line, c call) { l.point = l.viColumn(c.count) },
		"vi-match": func(l *Line, _ call) {
			q, ok := l.viMatch(l.point)
			if !ok {
				l.ding()
				return
			}
			l.point = q
		},
		"vi-char-search": func(l *Line, c call) {
			search := func(kind byte, r rune) {
				q, ok := l.viCharSearch(kind, r, l.point, c.count)
				if !ok {
					l.ding()
					return
				}
				l.point = q
			}
			switch c.key {
			case ';', ',':
				s := l.viLastSearch
				if s.kind == 0 {
					l.ding()
					return
				}
				kind := s.kind
				if c.key == ',' {
					kind = viReverse(kind)
				}
				search(kind, s.char)
			default:
				l.readNext = func(r key.Code) {
					l.viLastSearch = viSearch{kind: byte(c.key), char: rune(r &^ key.ModMask)}
					search(byte(c.key), l.viLastSearch.char)
				}
			}
		},
		"vi-delete": func(l *Line, c call) {
			if l.point >= len(l.buf) {
				l.ding()
				return
			}
			end := l.point
			for i := 0; i < c.count && end < len(l.buf); i++ {
				end = l.nextChar(end)
			}
			l.killText(l.point, end)
			l.viChange = true
		},
		"vi-rubout": func(l *Line, c call) {
			if l.point == 0 {
				l.ding()
				return
			}
			start := l.point
			for i := 0; i < c.count && start > 0; i++ {
				start = l.prevChar(start)
			}
			l.killText(start, l.point)
			l.viChange = true
		},
		"vi-delete-to": func(l *Line, c call) { viOperatorCommand(l, c, 'd') },
		"vi-change-to": func(l *Line, c call) { viOperatorCommand(l, c, 'c') },
		"vi-yank-to":   func(l *Line, c call) { viOperatorCommand(l, c, 'y') },
		"vi-put": func(l *Line, c call) {
			text, ok := l.kills.current()
			if !ok {
				l.ding()
				return
			}
			pos := l.point
			if c.key == 'p' && pos < len(l.buf) {
				pos = l.nextChar(pos)
			}
			var all []byte
			for range max(c.count, 1) {
				all = append(all, text...)
			}
			l.insert(pos, all)
			l.point = pos + len(all) - 1
			l.mark = pos
			l.viChange = true
		},
		"vi-change-char": func(l *Line, c call) {
			l.readNext = func(r key.Code) {
				end := l.point
				for i := 0; i < c.count; i++ {
					if end >= len(l.buf) {
						l.ding()
						return
					}
					end = l.nextChar(end)
				}
				l.undo.begin()
				ch := string(rune(r &^ key.ModMask))
				n := utf8.RuneCount(l.buf[l.point:end])
				var text []byte
				for range n {
					text = append(text, ch...)
				}
				start := l.point
				l.replace(start, end, text)
				l.point = start + len(text) - len(ch)
				l.undo.end()
				l.viChange = true
				l.viDone()
			}
		},
		"vi-subst": func(l *Line, c call) {
			if c.key == 'S' {
				l.viWholeLine('c')
				return
			}
			end := l.point
			for i := 0; i < c.count && end < len(l.buf); i++ {
				end = l.nextChar(end)
			}
			l.killText(l.point, end)
			l.mark = end
			l.startInsert()
		},
		"vi-change-case": func(l *Line, c call) {
			for i := 0; i < c.count && l.point < len(l.buf); i++ {
				end := l.nextChar(l.point)
				r := l.runeAt(l.point)
				switch {
				case unicode.IsUpper(r):
					r = unicode.ToLower(r)
				case unicode.IsLower(r):
					r = unicode.ToUpper(r)
				}
				l.replace(l.point, end, []byte(string(r)))
				l.point = l.nextChar(l.point)
			}
			l.viChange = true
		},
		"vi-replace": func(l *Line, _ call) {
			l.overwrite = true
			l.startInsert()
		},
		"vi-undo": func(l *Line, c call) {
			for range max(c.count, 1) {
				if !l.undoOne() {
					l.ding()
					return
				}
			}
		},
		"vi-redo": func(l *Line, _ call) {
			keys := l.viRedo
			if keys == nil {
				l.ding()
				return
			}
			l.viReplaying = true
			for _, k := range keys {
				l.Key(k)
			}
			l.viReplaying = false
			// The redo is no change of its own, so the change it repeated
			// stays the one to repeat.
			l.viChange = false
		},
		"vi-arg-digit": func(l *Line, c call) {
			l.arg = c.arg
			l.arg.add(c.key)
			l.this, l.thisKill = l.last, l.lastKill
		},
		"vi-unix-word-rubout": func(l *Line, c call) {
			// As unix-word-rubout, but the mark stays where it was.
			mark := l.mark
			unixWordRubout(l, c)
			l.mark = mark
		},
		"vi-editing-mode":    func(l *Line, _ call) { l.setMode(modeViInsert) },
		"emacs-editing-mode": func(l *Line, _ call) { l.setMode(modeEmacs) },
	} {
		commands[name] = cmd
	}
}

// viDone is called when a command that read a key of its own has finished,
// so that what it did can be repeated and the cursor is kept on the line.
func (l *Line) viDone() {
	l.viClamp()
}
