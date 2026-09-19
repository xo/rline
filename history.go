package rline

import (
	"bufio"
	"os"
	"strings"
)

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
func (h *history) loadFrom(fname string, maxEntries int) {
	h.clear()
	h.fname = fname
	if maxEntries == 0 {
		h.max = 0
		return
	}
	if maxEntries < 0 || maxEntries > maxHistory {
		maxEntries = maxHistory
	}
	h.max = maxEntries
	h.load()
}

// load reads the file into the list. A file that is not there, or that cannot
// be opened, leaves the list as it is.
func (h *history) load() {
	if h.fname == "" {
		return
	}
	f, err := os.Open(h.fname)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	var buf buffer
	for {
		if _, err := r.Peek(1); err != nil {
			return
		}
		if !h.readEntry(r, &buf) {
			// A line that cannot be read stops the whole file, because
			// anything after it is as likely to be wrong.
			return
		}
	}
}

// save writes the list to its file, oldest first.
//
// The file holds what the user typed, so it is readable only by its owner.
// The C code creates the file and then changes its mode. This asks for the
// mode when it creates the file, and sets it again in case the file was
// already there with a looser one.
func (h *history) save() error {
	if h.fname == "" {
		return nil
	}
	f, err := os.OpenFile(h.fname, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		// The C code gives up without saying anything when it cannot open
		// the file. The caller decides what to do here instead.
		return err //nolint:wrapcheck // the path is already in the error
	}
	defer func() { _ = f.Close() }()
	_ = os.Chmod(h.fname, 0o600)
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
