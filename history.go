// The history: the lines the user typed before, and the file they are kept
// in. readline's commands walk and search them, in internal/readline.
//
// Ported from isocline/src/history.c.

package rline

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/xo/rline/internal/text"
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

	// mode is the permission a history file is created with. Zero means
	// DefaultHistoryFileMode.
	mode fs.FileMode

	// partial says the file was not read to the end, so what is held is
	// less than what is in it. Saving would then write the shorter list
	// over the longer file and destroy the rest. See save.
	partial bool

	// max is how many entries fit. Zero means the list is not usable yet,
	// because loadFrom has not been called, and every push is refused.
	max int

	// name is the file the list is kept in. An empty name means it is not
	// kept anywhere.
	name string

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

// loadFrom points the list at name, makes room for maxEntries lines, and
// reads what is already in the file.
//
// A maxEntries of zero leaves the list unusable, and every push is refused. A
// negative number, or one above maxHistory, is held at maxHistory.
func (h *history) loadFrom(name string, maxEntries int) error {
	h.clear()
	h.name = name
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
	h.partial = false
	if h.name == "" {
		return nil
	}
	f, err := os.Open(h.name)
	if err != nil {
		// A history file that is not there yet is the ordinary state of a
		// program on its first run, and is not a failure. Anything else is.
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		h.partial = true
		return fmt.Errorf("opening the history file %s: %w", h.name, err)
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var buf text.Buffer
	for {
		if _, err := r.Peek(1); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			h.partial = true
			return fmt.Errorf("reading the history file %s: %w", h.name, err)
		}
		if !h.readEntry(r, &buf) {
			// A line that cannot be read stops the whole file, because
			// anything after it is as likely to be wrong. What was read
			// before it is kept, and the rest is still in the file, which
			// is why saving is refused until it is read or replaced.
			h.partial = true
			return fmt.Errorf("reading the history file %s: a line could not be read", h.name)
		}
	}
}

// save writes the list to its file, oldest first.
//
// Nothing here restricts who can read the file. The C code creates it and
// then changes its mode to owner only, which this used to follow, and the
// mode is deliberately left out now: it cannot be expressed on Windows at
// all, and the history is going to be rewritten, so guarding it here would
// be work thrown away twice. docs/PORT.md records what that leaves open.
func (h *history) save() error {
	if h.name == "" {
		return nil
	}
	if h.partial {
		// Saving rewrites the file from the list held in memory. When the
		// file was not read to the end, that list is shorter than the file,
		// and writing it would destroy the part that was never read.
		//
		// The C does exactly that. It truncates on open and writes what it
		// has, so one line it cannot parse costs a user every line after
		// it — measured at fifty bytes becoming nineteen. This refuses
		// instead, which is a departure and the reason for it.
		return fmt.Errorf("not saving the history file %s: it was not read in full, "+
			"so saving would write over the part that was not read", h.name)
	}
	mode := h.mode
	if mode == 0 {
		mode = DefaultHistoryFileMode
	}
	// Written beside the file and renamed over it, rather than truncating
	// the file and writing into it.
	//
	// Truncating first is what the C does, and it means a failure part way
	// through leaves a file shorter than it was: the history is gone and
	// nothing says so. A rename is atomic on every system this builds for,
	// so the file is either the old one or the new one and never a
	// half-written one. The temporary file is in the same directory
	// because a rename across filesystems is not allowed.
	dir, base := filepath.Split(h.name)
	if dir == "" {
		dir = "."
	}
	f, err := os.CreateTemp(dir, "."+base+".")
	if err != nil {
		// The C gives up without saying anything when it cannot open the
		// file. The caller decides what to do here instead.
		return fmt.Errorf("making a temporary file beside the history file %s: %w", h.name, err)
	}
	tmp := f.Name()
	defer func() {
		// Whatever happens, the temporary file does not outlive this call.
		// A successful rename has already taken it away, and the remove
		// then does nothing.
		_ = os.Remove(tmp)
	}()
	// CreateTemp makes the file 0600 whatever was asked for, so the mode is
	// set here. Doing it before the file has anything in it means the
	// contents are never readable under a wider mode than was asked for,
	// even for an instant.
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return fmt.Errorf("setting the mode of the history file %s: %w", h.name, err)
	}
	// A write error can appear at Close rather than before it, because the
	// last of the buffer goes out there and some filesystems only report a
	// failure once the file is closed. The close below the writes is the
	// one that matters; this one only runs when something went wrong first,
	// and its error is dropped because the first error is the one to report.
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
	}()

	// bufio keeps the first write error and hands it back from Flush, so
	// the writes below are not checked one at a time. That is the ordinary
	// way to use it, and checking each one would add a branch per line that
	// can only repeat what Flush is about to say.
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
	if err := w.Flush(); err != nil {
		return fmt.Errorf("writing the history file %s: %w", h.name, err)
	}
	// The close has to happen before the rename rather than in the deferred
	// function, because Windows will not rename a file that is still open.
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing the history file %s: %w", h.name, err)
	}
	closed = true
	if err := os.Rename(tmp, h.name); err != nil {
		return fmt.Errorf("replacing the history file %s: %w", h.name, err)
	}
	return nil
}

// readEntry reads one line and adds it to the list. It reports false when the
// line holds an escape it cannot read, which stops the whole file.
//
// An empty line, and a line that starts with '#', are skipped without being
// added.
func (h *history) readEntry(r *bufio.Reader, buf *text.Buffer) bool {
	buf.Clear()
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
			buf.AppendByte(c)
		}
	}
	if buf.Length() == 0 || buf.CharAt(0) == '#' {
		return true
	}
	return h.push(buf.String())
}

// readEscape reads what follows a backslash. It reports false for an escape
// that is not one of the four, or for a hexadecimal escape without two digits
// after it.
func readEscape(r *bufio.Reader, buf *text.Buffer) bool {
	c, err := r.ReadByte()
	if err != nil {
		return false
	}
	switch c {
	case 'n':
		buf.AppendString("\n")
	case 'r':
		// Dropped. Nothing writes this, because a carriage return is dropped
		// on the way out as well.
	case 't':
		buf.AppendString("\t")
	case '\\':
		buf.AppendString("\\")
	case 'x':
		c1, err1 := r.ReadByte()
		c2, err2 := r.ReadByte()
		if err1 != nil || err2 != nil || !isHexDigit(c1) || !isHexDigit(c2) {
			return false
		}
		// A zero byte appends nothing, because a buffer cannot hold one, so
		// "\x00" on a line by itself reads as an empty line.
		buf.AppendByte(fromHexDigit(c1)*16 + fromHexDigit(c2))
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
			b.WriteByte(toHexDigit(c / 16))
			b.WriteByte(toHexDigit(c % 16))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// isHexDigit reports whether c is a hexadecimal digit.
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// fromHexDigit returns the value of one hexadecimal digit, and zero for a byte
// that is not one.
func fromHexDigit(c byte) byte {
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

// toHexDigit returns the hexadecimal digit for a value from 0 to 15, in upper
// case, and '0' for anything else.
func toHexDigit(c byte) byte {
	switch {
	case c <= 9:
		return c + '0'
	case c <= 15:
		return c - 10 + 'A'
	}
	return '0'
}
