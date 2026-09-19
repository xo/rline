package rline

import (
	"fmt"
	"slices"
)

// A growable buffer that edits in place and moves by character, not by byte.
// The edit loop keeps the line the user is typing in one of these.
//
// The allocator and the growth policy of the C code are gone, because a Go
// slice grows on demand and the garbage collector frees it. What is left is
// the edit operations.
//
// Ported from isocline/src/stringbuf.c.

// buffer holds text that is edited in place. The zero value is ready to use.
type buffer struct {
	buf []byte
}

// bytes returns the contents of b. The result aliases the buffer, so an edit
// after this call can change it.
func (b *buffer) bytes() []byte {
	return b.buf
}

// string returns the contents of b as a string.
func (b *buffer) string() string {
	return string(b.buf)
}

// length returns the number of bytes in b.
func (b *buffer) length() int {
	return len(b.buf)
}

// charAt returns the byte at pos. A pos outside the buffer returns 0, and so
// does pos at the end, where the C code reads its terminating zero.
func (b *buffer) charAt(pos int) byte {
	if pos < 0 || pos >= len(b.buf) {
		return 0
	}
	return b.buf[pos]
}

// limitToLength returns the length of s up to its first zero byte.
//
// Every insert goes through this, so a buffer can never hold a zero byte.
// Several other functions count on that: they treat the end of the slice and
// a zero byte as the same thing, because in C they are.
func limitToLength(s string) int {
	for i := range len(s) {
		if s[i] == 0 {
			return i
		}
	}
	return len(s)
}

// insertAt inserts s at pos and returns the position after what it inserted.
// A pos outside the buffer inserts nothing and returns pos.
func (b *buffer) insertAt(s string, pos int) int {
	if pos < 0 || pos > len(b.buf) {
		return pos
	}
	n := limitToLength(s)
	if n <= 0 {
		return pos
	}
	b.buf = slices.Insert(b.buf, pos, []byte(s[:n])...)
	return pos + n
}

// insertByteAt inserts the single byte c at pos and returns the position
// after it. A zero byte inserts nothing, by the rule in limitToLength.
func (b *buffer) insertByteAt(c byte, pos int) int {
	return b.insertAt(string([]byte{c}), pos)
}

// insertRuneAt inserts r at pos, encoded as QUTF-8, and returns the position
// after it.
func (b *buffer) insertRuneAt(r rune, pos int) int {
	return b.insertAt(string(appendRune(nil, r)), pos)
}

// appendString appends s and returns the new length.
func (b *buffer) appendString(s string) int {
	return b.insertAt(s, len(b.buf))
}

// appendByte appends the single byte c and returns the new length.
func (b *buffer) appendByte(c byte) int {
	return b.insertByteAt(c, len(b.buf))
}

// appendf formats its arguments and appends the result, then returns the new
// length.
//
// The format string is the one that the fmt package reads, not the one that C
// printf reads. The C function passes the string straight to vsnprintf, so
// every caller of it has to be read and rewritten as it is ported.
func (b *buffer) appendf(format string, args ...any) int {
	return b.appendString(fmt.Sprintf(format, args...))
}

// deleteAt removes count bytes at pos. A pos outside the buffer removes
// nothing, and so does a pos at the end.
func (b *buffer) deleteAt(pos, count int) {
	if pos < 0 || pos >= len(b.buf) || count <= 0 {
		return
	}
	count = min(count, len(b.buf)-pos)
	b.buf = append(b.buf[:pos], b.buf[pos+count:]...)
}

// deleteFromTo removes the bytes from pos up to but not including end.
func (b *buffer) deleteFromTo(pos, end int) {
	if end <= pos {
		return
	}
	b.deleteAt(pos, end-pos)
}

// deleteFrom removes everything from pos onwards.
func (b *buffer) deleteFrom(pos int) {
	b.deleteAt(pos, len(b.buf)-pos)
}

// clear empties the buffer.
func (b *buffer) clear() {
	b.deleteAt(0, len(b.buf))
}

// replace empties the buffer and puts s in it.
func (b *buffer) replace(s string) {
	b.clear()
	b.appendString(s)
}

// splitAt cuts b at pos and returns a new buffer holding what came after. A
// negative pos returns nil and changes nothing.
//
// The C code sets the new length of the left buffer but never writes the
// terminating zero, so the left buffer stops being a valid C string. Nothing
// in isocline calls this function, so that bug never reaches a recorded
// session. This port does what the function means to do.
func (b *buffer) splitAt(pos int) *buffer {
	if pos < 0 {
		return nil
	}
	rest := &buffer{}
	if pos < len(b.buf) {
		rest.buf = append(rest.buf, b.buf[pos:]...)
		b.buf = b.buf[:pos]
	}
	return rest
}

// nextOfs returns the bytes from pos to the next character, and the column
// width of the character at pos.
func (b *buffer) nextOfs(pos int) (int, int) {
	return nextOfs(b.buf, pos)
}

// prevOfs returns the bytes from pos back to the previous character, and the
// column width of that character.
func (b *buffer) prevOfs(pos int) (int, int) {
	return prevOfs(b.buf, pos)
}

// next returns the position of the character after pos and its width, or -1
// when there is none.
func (b *buffer) next(pos int) (int, int) {
	ofs, w := b.nextOfs(pos)
	if ofs <= 0 {
		return -1, w
	}
	return pos + ofs, w
}

// prev returns the position of the character before pos and its width, or -1
// when there is none.
func (b *buffer) prev(pos int) (int, int) {
	ofs, w := b.prevOfs(pos)
	if ofs <= 0 {
		return -1, w
	}
	return pos - ofs, w
}

// deleteCharBefore removes the character before pos and returns the position
// it left the cursor at.
func (b *buffer) deleteCharBefore(pos int) int {
	n, _ := b.prevOfs(pos)
	if n <= 0 {
		return 0
	}
	b.deleteAt(pos-n, n)
	return pos - n
}

// deleteCharAt removes the character at pos.
func (b *buffer) deleteCharAt(pos int) {
	n, _ := b.nextOfs(pos)
	if n <= 0 {
		return
	}
	b.deleteAt(pos, n)
}

// swapCharLimit is the longest character that swapChar moves. The C code
// copies the earlier character into a 64 byte buffer on the stack and refuses
// anything that does not fit. A run of continuation bytes that long is not
// valid UTF-8, but prevOfs walks over it, so the limit is reachable.
const swapCharLimit = 63

// swapChar exchanges the character before pos with the character at pos, and
// returns the position where the pair now starts. It returns 0 when there is
// no character on one of the two sides.
func (b *buffer) swapChar(pos int) int {
	next, _ := b.nextOfs(pos)
	if next <= 0 {
		return 0
	}
	prev, _ := b.prevOfs(pos)
	if prev <= 0 || prev >= swapCharLimit {
		return 0
	}
	saved := make([]byte, prev)
	copy(saved, b.buf[pos-prev:pos])
	copy(b.buf[pos-prev:], b.buf[pos:pos+next])
	copy(b.buf[pos-prev+next:], saved)
	return pos - prev
}

// findLineStart returns the start of the line that pos is on.
func (b *buffer) findLineStart(pos int) int { return findLineStart(b.buf, pos) }

// findLineEnd returns the end of the line that pos is on.
func (b *buffer) findLineEnd(pos int) int { return findLineEnd(b.buf, pos) }

// findWordStart returns the start of the word before pos.
func (b *buffer) findWordStart(pos int) int { return findWordStart(b.buf, pos) }

// findWordEnd returns the end of the word after pos.
func (b *buffer) findWordEnd(pos int) int { return findWordEnd(b.buf, pos) }

// findWSWordStart returns the start of the run of non-whitespace before pos.
func (b *buffer) findWSWordStart(pos int) int { return findWSWordStart(b.buf, pos) }

// findWSWordEnd returns the end of the run of non-whitespace after pos.
func (b *buffer) findWSWordEnd(pos int) int { return findWSWordEnd(b.buf, pos) }

// forEachRow calls fn once for each screen row and returns the number of rows.
func (b *buffer) forEachRow(termWidth, promptWidth, contWidth int, fn rowFunc) int {
	return forEachRow(b.buf, termWidth, promptWidth, contWidth, fn)
}

// rowColAtPos returns the number of rows and where pos sits.
func (b *buffer) rowColAtPos(termWidth, promptWidth, contWidth, pos int) (int, rowCol) {
	return rowColAtPos(b.buf, termWidth, promptWidth, contWidth, pos)
}

// posAtRowCol returns the position at row and col, or -1 when there is no
// such row.
func (b *buffer) posAtRowCol(termWidth, promptWidth, contWidth, row, col int) int {
	return posAtRowCol(b.buf, termWidth, promptWidth, contWidth, row, col)
}

// wrappedRowColAtPos returns where pos sits once the terminal is resized to
// newTermWidth, and how many rows the text then takes.
func (b *buffer) wrappedRowColAtPos(termWidth, newTermWidth, promptWidth, contWidth, pos int) (int, rowCol) {
	return wrappedRowColAtPos(b.buf, termWidth, newTermWidth, promptWidth, contWidth, pos)
}

// decodeFromLocale returns the contents of b with everything a terminal that
// does not read UTF-8 cannot show taken out.
//
// A byte that stands on its own is kept. An escape sequence is dropped. A
// character of more than one byte is kept only when it is a raw byte from the
// QUTF-8 raw plane, or when it decodes to ASCII. Anything else is dropped,
// because there is nothing to turn it into without knowing the encoding the
// terminal wants.
//
// The C code allocates one byte too few for its result and then writes the
// terminating zero one byte past the end. Any buffer that holds only single
// byte characters overruns. The port has no terminator to write.
func (b *buffer) decodeFromLocale() []byte {
	if len(b.buf) == 0 {
		return nil
	}
	out := make([]byte, 0, len(b.buf))
	for i := 0; i < len(b.buf); {
		ofs, _ := b.nextOfs(i)
		switch {
		case ofs <= 0:
			return out
		case ofs == 1:
			out = append(out, b.buf[i])
		case b.buf[i] == '\x1B':
			// An escape sequence shows nothing, so drop it.
		default:
			r, _ := decodeRune(b.buf[i : i+ofs])
			if c, ok := rawByte(r); ok {
				out = append(out, c)
			} else if r <= 0x7F {
				out = append(out, byte(r))
			}
		}
		i += ofs
	}
	return out
}
