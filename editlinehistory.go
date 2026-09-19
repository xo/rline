package rline

import "github.com/xo/rline/key"

// Walking and searching the history from inside the edit loop.
//
// Ported from isocline/src/editline_history.c.

// historyAt replaces the line with the entry ofs steps away, where a positive
// ofs goes back in time.
//
// A line that has been typed into is put back into the newest entry first, so
// that walking away from it and returning finds it again.
func (ev *env) historyAt(e *editor, ofs int) {
	if e.modified {
		ev.history.update(e.input.string())
		e.historyIdx = 0
		e.modified = false
	}
	entry, ok := ev.history.get(e.historyIdx + ofs)
	if !ok {
		ev.term.beep()
		return
	}
	e.historyIdx += ofs
	e.input.replace(entry)
	if ofs > 0 {
		// Going back lands at the end of the first row, so a long entry shows
		// from its start.
		e.pos = max(e.input.findLineEnd(0), 0)
	} else {
		e.pos = e.input.length()
	}
	ev.refresh(e)
}

// historyPrev replaces the line with the previous history entry.
func (ev *env) historyPrev(e *editor) { ev.historyAt(e, 1) }

// historyNext replaces the line with the next history entry.
func (ev *env) historyNext(e *editor) { ev.historyAt(e, -1) }

// hsearchStep is one step of a history search, kept so that backspace can take
// it back.
type hsearchStep struct {
	// Where the search had got to.
	hidx     int
	matchPos int
	matchLen int

	// inserted says this step was a character being typed, which backspace
	// must also take out of the line.
	inserted bool
}

// historySearchWithCurrentWord opens the incremental search, starting from the
// word the cursor is in.
func (ev *env) historySearchWithCurrentWord(e *editor) {
	initial := ""
	if start := e.input.findWordStart(e.pos); start >= 0 {
		b := e.input.bytes()
		next, _ := e.input.next(start)
		// A word that starts with something that is not part of a name, such
		// as a quote, starts after it instead.
		if next > start && !charIsIDLetter(b[start:next]) {
			start = next
		}
		if start >= 0 && start < e.pos {
			initial = string(b[start:e.pos])
		}
	}
	ev.historySearch(e, initial)
}

// historySearch reads the history for a line holding what is typed, and keeps
// reading as more is typed.
//
// It draws its own prompt and reads its own keys, and leaves the line either
// as it was or as the entry that was found.
func (ev *env) historySearch(e *editor, initial string) {
	if ev.history.count() <= 0 {
		ev.term.beep()
		return
	}
	if e.modified {
		ev.history.update(e.input.string())
		e.historyIdx = 0
		e.modified = false
	}
	// The line is put back from the undo stack if the search is abandoned, so
	// nothing else may record while it runs.
	e.undoCapture()
	e.disableUndo = true
	wasNoHint, wasPrompt := ev.noHint, e.promptText
	ev.noHint = true
	e.promptText = "history search"
	defer func() {
		e.disableUndo = false
		ev.noHint, e.promptText = wasNoHint, wasPrompt
		ev.refresh(e)
	}()

	var stack []hsearchStep
	hidx, matchPos, matchLen := 1, 0, 0
	push := func(inserted bool) {
		stack = append(stack, hsearchStep{hidx, matchPos, matchLen, inserted})
	}
	// drop takes the last step off without putting its values back, which is
	// what a search that found nothing wants: the step never happened.
	drop := func() {
		if n := len(stack); n > 0 {
			stack = stack[:n-1]
		}
	}
	undo := func() (hsearchStep, bool) {
		n := len(stack)
		if n == 0 {
			return hsearchStep{}, false
		}
		s := stack[n-1]
		stack = stack[:n-1]
		hidx, matchPos, matchLen = s.hidx, s.matchPos, s.matchLen
		return s, true
	}

	// A search started from a word is played back one character at a time, so
	// that backspace walks out of it the same way it walks out of typing.
	if initial != "" {
		pos := 0
		for pos < len(initial) {
			next, _ := nextOfs([]byte(initial), pos)
			if next <= 0 {
				break
			}
			push(true)
			if idx, mpos, ok := ev.history.search(hidx, initial[:pos+next], true); ok {
				hidx, matchPos = idx, mpos
				matchLen = pos + next
			} else if pos+next >= len(initial) {
				ev.term.beep()
			}
			pos += next
		}
		e.input.replace(initial)
		e.pos = pos
	} else {
		e.input.clear()
		e.pos = 0
	}

	for {
		entry, found := ev.history.get(hidx)
		if found {
			ev.showSearchMatch(e, hidx, entry, matchPos, matchLen)
		}
		ev.refresh(e)

		// With nothing found there is nothing to wait for, so the search ends
		// as though it had been abandoned.
		c := key.Esc
		if found {
			c = ev.tty.read()
		}
		if ev.tty.resizeEvent() {
			ev.resize(e)
		}
		e.extra.clear()

		switch c {
		case key.Esc, key.Bell, key.CtrlC:
			// Abandoned: put the line back as it was.
			e.disableUndo = false
			e.undoRestore(false)
			return
		case key.Enter:
			// Taken: the entry becomes the line.
			e.undoForget()
			e.input.replace(entry)
			e.pos = e.input.length()
			e.modified = false
			e.historyIdx = hidx
			return
		case key.Backspace, key.CtrlZ:
			if s, ok := undo(); ok && s.inserted {
				ev.act(e, e.backspace())
			}
		case key.CtrlR, key.Tab, key.Up:
			push(false)
			if idx, mpos, ok := ev.history.search(hidx+1, e.input.string(), true); ok {
				hidx, matchPos = idx, mpos
			} else {
				drop()
				ev.term.beep()
			}
		case key.CtrlS, key.ShiftTab, key.Down:
			push(false)
			if idx, mpos, ok := ev.history.search(hidx-1, e.input.string(), false); ok {
				hidx, matchPos = idx, mpos
			} else {
				drop()
				ev.term.beep()
			}
		case key.F1:
			ev.showHelp(e)
		default:
			chr, isASCII := c.ASCIIChar()
			r, isRune := c.Unicode()
			switch {
			case isASCII:
				push(true)
				e.insertChar(chr)
				ev.refreshHint(e)
			case isRune:
				push(true)
				e.insertRune(r)
				ev.refreshHint(e)
			default:
				// A key with no place in a search.
				ev.term.beep()
				continue
			}
			if idx, mpos, ok := ev.history.search(hidx, e.input.string(), true); ok {
				hidx, matchPos = idx, mpos
				matchLen = e.input.length()
			} else {
				ev.term.beep()
			}
		}
	}
}

// showSearchMatch fills the area below the line with the entry that was found,
// underlining the part that matched.
func (ev *env) showSearchMatch(e *editor, hidx int, entry string, matchPos, matchLen int) {
	// The entry is written between [!pre] tags, so that a bracket in it is
	// text rather than markup.
	lo := min(max(matchPos, 0), len(entry))
	hi := min(max(lo+matchLen, lo), len(entry))
	e.extra.appendf("[ic-info]%d. [/][ic-diminish][!pre]", hidx)
	e.extra.appendString(entry[:lo])
	e.extra.appendString("[/pre][u ic-emphasis][!pre]")
	e.extra.appendString(entry[lo:hi])
	e.extra.appendString("[/pre][/u][!pre]")
	e.extra.appendString(entry[hi:])
	e.extra.appendString("[/pre][/ic-diminish]")
	if !ev.noHelp {
		e.extra.appendString("\n[ic-info](use tab for the next match)[/]")
	}
	e.extra.appendString("\n")
}
