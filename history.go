// The history: the lines the user typed before, the file they are kept in,
// and walking or searching them from inside the edit loop.
//
// Ported from isocline/src/history.c and editline_history.c.

package rline

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/xo/rline/key"
)

// --------------------------------------------------------------------------
// history.go

// The list of lines the user typed before, and the file it is kept in.
//
// The list is held oldest first. Callers count from the other end, because
// they walk back through what was typed, so index 0 is the most recent entry.
//
// Ported from isocline/src/history.c.

// maxHistory is the most entries the list will hold, whatever is asked for.
const maxHistory = 200

// history is the list of lines the user typed before.
type history struct {
	// entries holds the lines, oldest first.
	entries []string

	// max is how many entries fit. Zero means the list is not usable yet,
	// because loadFrom has not been called, and every push is refused.
	max int

	// fname is the file the list is kept in. An empty name means it is not
	// kept anywhere.
	fname string

	// allowDuplicates says whether the same line may appear twice.
	allowDuplicates bool
}

// count returns how many entries the list holds.
func (h *history) count() int {
	return len(h.entries)
}

// enableDuplicates says whether the same line may appear twice, and returns
// what the setting was before.
func (h *history) enableDuplicates(enable bool) bool {
	prev := h.allowDuplicates
	h.allowDuplicates = enable
	return prev
}

// clear empties the list. It does not change how many entries fit, nor the
// file the list is kept in.
func (h *history) clear() {
	h.entries = nil
}

// get returns the entry n back from the most recent one. get(0) is the last
// line the user typed. It reports false when n is outside the list.
func (h *history) get(n int) (string, bool) {
	if n < 0 || n >= len(h.entries) {
		return "", false
	}
	return h.entries[len(h.entries)-n-1], true
}

// all returns the entries, newest first, in a slice of their own so that a
// caller cannot change the list by holding on to it.
func (h *history) all() []string {
	if len(h.entries) == 0 {
		return nil
	}
	out := make([]string, len(h.entries))
	for i := range h.entries {
		out[i] = h.entries[len(h.entries)-i-1]
	}
	return out
}

// deleteAt removes the entry at idx, counting from the oldest.
func (h *history) deleteAt(idx int) {
	if idx < 0 || idx >= len(h.entries) {
		return
	}
	h.entries = append(h.entries[:idx], h.entries[idx+1:]...)
}

// push adds entry as the most recent line. It reports false when the list
// cannot hold anything, which is the case until loadFrom has been called.
//
// The oldest entry goes when the list is full.
func (h *history) push(entry string) bool {
	if h.max <= 0 {
		return false
	}
	if !h.allowDuplicates {
		// This walk does not step back after it removes something, so when
		// two neighbours both match, only the first one goes. The C code
		// walks the same way, and the port keeps it so that the two agree.
		// Reaching it needs duplicates already in the list, which only
		// happens when they were allowed earlier or came from the file.
		for i := 0; i < len(h.entries); i++ {
			if h.entries[i] == entry {
				h.deleteAt(i)
			}
		}
	}
	if len(h.entries) == h.max {
		h.deleteAt(0)
	}
	h.entries = append(h.entries, entry)
	return true
}

// update replaces the most recent entry with entry.
func (h *history) update(entry string) bool {
	h.removeLast()
	h.push(entry)
	return true
}

// removeLastN removes the n most recent entries.
func (h *history) removeLastN(n int) {
	if n <= 0 {
		return
	}
	n = min(n, len(h.entries))
	h.entries = h.entries[:len(h.entries)-n]
}

// removeLast removes the most recent entry.
func (h *history) removeLast() {
	h.removeLastN(1)
}

// search looks for needle in the entries, starting at from and counting from
// the most recent. Going backward walks towards older entries, which is
// towards a higher index. It returns the index of the entry and where in it
// the text was found.
//
// The C code hands the entry straight to strstr without checking that the
// index names one, so a starting index outside the list reads through a null
// pointer. This stops instead, and answers that it found nothing.
func (h *history) search(from int, needle string, backward bool) (int, int, bool) {
	step := -1
	if backward {
		step = 1
	}
	for i := from; i >= 0 && i < len(h.entries); i += step {
		entry, ok := h.get(i)
		if !ok {
			continue
		}
		if pos := strings.Index(entry, needle); pos >= 0 {
			return i, pos, true
		}
	}
	return 0, 0, false
}

// loadFrom points the list at fname, makes room for maxEntries lines, and
// reads what is already in the file.
//
// A maxEntries of zero leaves the list unusable, and every push is refused. A
// negative number, or one above maxHistory, is held at maxHistory.
func (h *history) loadFrom(fname string, maxEntries int) error {
	h.clear()
	h.fname = fname
	if maxEntries == 0 {
		h.max = 0
		return nil
	}
	if maxEntries < 0 || maxEntries > maxHistory {
		maxEntries = maxHistory
	}
	h.max = maxEntries
	return h.load()
}

// load reads the file into the list. A file that is not there, or that cannot
// be opened, leaves the list as it is.
func (h *history) load() error {
	if h.fname == "" {
		return nil
	}
	f, err := os.Open(h.fname)
	if err != nil {
		// A history file that is not there yet is the ordinary state of a
		// program on its first run, and is not a failure. Anything else is.
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("opening the history file %s: %w", h.fname, err)
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	var buf buffer
	for {
		if _, err := r.Peek(1); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("reading the history file %s: %w", h.fname, err)
		}
		if !h.readEntry(r, &buf) {
			// A line that cannot be read stops the whole file, because
			// anything after it is as likely to be wrong. What was read
			// before it is kept.
			return fmt.Errorf("reading the history file %s: a line could not be read", h.fname)
		}
	}
}

// save writes the list to its file, oldest first.
//
// Nothing here restricts who can read the file. The C code creates it and
// then changes its mode to owner only, which this used to follow, and the
// mode is deliberately left out now: it cannot be expressed on Windows at
// all, and the history is going to be rewritten, so guarding it here would
// be work thrown away twice. PLAN.md records what that leaves open.
func (h *history) save() error {
	if h.fname == "" {
		return nil
	}
	// 0666 is what the C gets from fopen, with the umask applied by the
	// system.
	f, err := os.OpenFile(h.fname, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		// The C code gives up without saying anything when it cannot open
		// the file. The caller decides what to do here instead.
		return err //nolint:wrapcheck // the path is already in the error
	}
	defer func() { _ = f.Close() }()
	w := bufio.NewWriter(f)
	for _, entry := range h.entries {
		line := escapeEntry(entry)
		if line == "" {
			// An entry with nothing left after escaping writes no line at
			// all, not even an empty one.
			continue
		}
		_, _ = w.WriteString(line)
		_ = w.WriteByte('\n')
	}
	return w.Flush() //nolint:wrapcheck // nothing to add to a write error
}

// readEntry reads one line and adds it to the list. It reports false when the
// line holds an escape it cannot read, which stops the whole file.
//
// An empty line, and a line that starts with '#', are skipped without being
// added.
func (h *history) readEntry(r *bufio.Reader, buf *buffer) bool {
	buf.clear()
	for {
		c, err := r.ReadByte()
		if err != nil || c == '\n' {
			break
		}
		switch c {
		case '\\':
			if !readEscape(r, buf) {
				return false
			}
		case '\r':
			// Dropped, so that a file written on Windows still reads.
		default:
			buf.appendByte(c)
		}
	}
	if buf.length() == 0 || buf.charAt(0) == '#' {
		return true
	}
	return h.push(buf.string())
}

// readEscape reads what follows a backslash. It reports false for an escape
// that is not one of the four, or for a hexadecimal escape without two digits
// after it.
func readEscape(r *bufio.Reader, buf *buffer) bool {
	c, err := r.ReadByte()
	if err != nil {
		return false
	}
	switch c {
	case 'n':
		buf.appendString("\n")
	case 'r':
		// Dropped. Nothing writes this, because a carriage return is dropped
		// on the way out as well.
	case 't':
		buf.appendString("\t")
	case '\\':
		buf.appendString("\\")
	case 'x':
		c1, err1 := r.ReadByte()
		c2, err2 := r.ReadByte()
		if err1 != nil || err2 != nil || !isXDigit(c1) || !isXDigit(c2) {
			return false
		}
		// A zero byte appends nothing, because a buffer cannot hold one, so
		// "\x00" on a line by itself reads as an empty line.
		buf.appendByte(fromXDigit(c1)*16 + fromXDigit(c2))
	default:
		return false
	}
	return true
}

// escapeEntry returns entry in the form the file holds it in.
//
// A newline, a tab and a backslash have their own escape. A carriage return
// is dropped. Everything a terminal cannot show goes out as two hexadecimal
// digits, and so does '#', because a line that starts with one is a comment.
func escapeEntry(entry string) string {
	var b strings.Builder
	b.Grow(len(entry))
	for i := range len(entry) {
		c := entry[i]
		switch {
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			// Dropped.
		case c == '\t':
			b.WriteString(`\t`)
		case c < ' ' || c > '~' || c == '#':
			b.WriteString(`\x`)
			b.WriteByte(toXDigit(c / 16))
			b.WriteByte(toXDigit(c % 16))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// isXDigit reports whether c is a hexadecimal digit.
func isXDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// fromXDigit returns the value of one hexadecimal digit, and zero for a byte
// that is not one.
func fromXDigit(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'A' && c <= 'F':
		return 10 + (c - 'A')
	case c >= 'a' && c <= 'f':
		return 10 + (c - 'a')
	}
	return 0
}

// toXDigit returns the hexadecimal digit for a value from 0 to 15, in upper
// case, and '0' for anything else.
func toXDigit(c byte) byte {
	switch {
	case c <= 9:
		return c + '0'
	case c <= 15:
		return c - 10 + 'A'
	}
	return '0'
}

// --------------------------------------------------------------------------
// editlinehistory.go

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
