// Package text holds the buffer a line is edited in, and everything that
// measures or walks over it.
//
// The buffer holds bytes rather than runes, because a byte the terminal sent
// that UTF-8 cannot read is kept as the byte it was, so that it survives a
// round trip. Every position in this package, and every position the public
// API takes, is a byte offset for the same reason.
//
// Ported from stringbuf.c, common.c and parts of editline.c.
package text

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/mattn/go-runewidth"
)

// --------------------------------------------------------------------------
// stringbuf.go

// A growable buffer that edits in place and moves by character, not by byte.
// The edit loop keeps the line the user is typing in one of these.
//
// The allocator and the growth policy of the C code are gone, because a Go
// slice grows on demand and the garbage collector frees it. What is left is
// the edit operations.
//
// Ported from isocline/src/stringbuf.c.

// Buffer holds text that is edited in place. The zero value is ready to use.
type Buffer struct {
	buf []byte
}

// Bytes returns the contents of b. The result aliases the buffer, so an edit
// after this call can change it.
func (b *Buffer) Bytes() []byte {
	return b.buf
}

// String returns the contents of b as a String.
func (b *Buffer) String() string {
	return string(b.buf)
}

// StringFrom returns the contents of b from pos onwards, or nothing when pos
// is outside it.
func (b *Buffer) StringFrom(pos int) string {
	if pos < 0 || pos > len(b.buf) {
		return ""
	}
	return string(b.buf[pos:])
}

// Length returns the number of bytes in b.
func (b *Buffer) Length() int {
	return len(b.buf)
}

// CharAt returns the byte at pos. A pos outside the buffer returns 0, and so
// does pos at the end, where the C code reads its terminating zero.
func (b *Buffer) CharAt(pos int) byte {
	if pos < 0 || pos >= len(b.buf) {
		return 0
	}
	return b.buf[pos]
}

// LimitToLength returns the length of s up to its first zero byte.
//
// Every insert goes through this, so a buffer can never hold a zero byte.
// Several other functions count on that: they treat the end of the slice and
// a zero byte as the same thing, because in C they are.
func LimitToLength(s string) int {
	for i := range len(s) {
		if s[i] == 0 {
			return i
		}
	}
	return len(s)
}

// InsertAt inserts s at pos and returns the position after what it inserted.
// A pos outside the buffer inserts nothing and returns pos.
func (b *Buffer) InsertAt(s string, pos int) int {
	if pos < 0 || pos > len(b.buf) {
		return pos
	}
	n := LimitToLength(s)
	if n <= 0 {
		return pos
	}
	b.buf = slices.Insert(b.buf, pos, []byte(s[:n])...)
	return pos + n
}

// InsertByteAt inserts the single byte c at pos and returns the position
// after it. A zero byte inserts nothing, by the rule in limitToLength.
func (b *Buffer) InsertByteAt(c byte, pos int) int {
	return b.InsertAt(string([]byte{c}), pos)
}

// InsertRuneAt inserts r at pos, encoded as QUTF-8, and returns the position
// after it.
func (b *Buffer) InsertRuneAt(r rune, pos int) int {
	return b.InsertAt(string(AppendRune(nil, r)), pos)
}

// AppendString appends s and returns the new length.
func (b *Buffer) AppendString(s string) int {
	return b.InsertAt(s, len(b.buf))
}

// AppendByte appends the single byte c and returns the new length.
func (b *Buffer) AppendByte(c byte) int {
	return b.InsertByteAt(c, len(b.buf))
}

// Appendf formats its arguments and appends the result, then returns the new
// length.
//
// The format string is the one that the fmt package reads, not the one that C
// printf reads. The C function passes the string straight to vsnprintf, so
// every caller of it has to be read and rewritten as it is ported.
func (b *Buffer) Appendf(format string, args ...any) int {
	return b.AppendString(fmt.Sprintf(format, args...))
}

// DeleteAt removes count bytes at pos. A pos outside the buffer removes
// nothing, and so does a pos at the end.
func (b *Buffer) DeleteAt(pos, count int) {
	if pos < 0 || pos >= len(b.buf) || count <= 0 {
		return
	}
	count = min(count, len(b.buf)-pos)
	b.buf = append(b.buf[:pos], b.buf[pos+count:]...)
}

// DeleteFromTo removes the bytes from pos up to but not including end.
func (b *Buffer) DeleteFromTo(pos, end int) {
	if end <= pos {
		return
	}
	b.DeleteAt(pos, end-pos)
}

// DeleteFrom removes everything from pos onwards.
func (b *Buffer) DeleteFrom(pos int) {
	b.DeleteAt(pos, len(b.buf)-pos)
}

// Clear empties the buffer.
func (b *Buffer) Clear() {
	b.DeleteAt(0, len(b.buf))
}

// Replace empties the buffer and puts s in it.
func (b *Buffer) Replace(s string) {
	b.Clear()
	b.AppendString(s)
}

// SplitAt cuts b at pos and returns a new buffer holding what came after. A
// negative pos returns nil and changes nothing.
//
// The C code sets the new length of the left buffer but never writes the
// terminating zero, so the left buffer stops being a valid C string. Nothing
// in isocline calls this function, so that bug never reaches a recorded
// session. This port does what the function means to do.
func (b *Buffer) SplitAt(pos int) *Buffer {
	if pos < 0 {
		return nil
	}
	rest := &Buffer{}
	if pos < len(b.buf) {
		rest.buf = append(rest.buf, b.buf[pos:]...)
		b.buf = b.buf[:pos]
	}
	return rest
}

// NextOfs returns the bytes from pos to the next character, and the column
// width of the character at pos.
func (b *Buffer) NextOfs(pos int) (int, int) {
	return NextOfs(b.buf, pos)
}

// PrevOfs returns the bytes from pos back to the previous character, and the
// column width of that character.
func (b *Buffer) PrevOfs(pos int) (int, int) {
	return PrevOfs(b.buf, pos)
}

// Next returns the position of the character after pos and its width, or -1
// when there is none.
func (b *Buffer) Next(pos int) (int, int) {
	ofs, w := b.NextOfs(pos)
	if ofs <= 0 {
		return -1, w
	}
	return pos + ofs, w
}

// Prev returns the position of the character before pos and its width, or -1
// when there is none.
func (b *Buffer) Prev(pos int) (int, int) {
	ofs, w := b.PrevOfs(pos)
	if ofs <= 0 {
		return -1, w
	}
	return pos - ofs, w
}

// DeleteCharBefore removes the character before pos and returns the position
// it left the cursor at.
func (b *Buffer) DeleteCharBefore(pos int) int {
	n, _ := b.PrevOfs(pos)
	if n <= 0 {
		return 0
	}
	b.DeleteAt(pos-n, n)
	return pos - n
}

// DeleteCharAt removes the character at pos.
func (b *Buffer) DeleteCharAt(pos int) {
	n, _ := b.NextOfs(pos)
	if n <= 0 {
		return
	}
	b.DeleteAt(pos, n)
}

// swapCharLimit is the longest character that swapChar moves. The C code
// copies the earlier character into a 64 byte buffer on the stack and refuses
// anything that does not fit. A run of continuation bytes that long is not
// valid UTF-8, but prevOfs walks over it, so the limit is reachable.
const swapCharLimit = 63

// SwapChar exchanges the character before pos with the character at pos, and
// returns the position where the pair now starts. It returns 0 when there is
// no character on one of the two sides.
func (b *Buffer) SwapChar(pos int) int {
	next, _ := b.NextOfs(pos)
	if next <= 0 {
		return 0
	}
	prev, _ := b.PrevOfs(pos)
	if prev <= 0 || prev >= swapCharLimit {
		return 0
	}
	saved := make([]byte, prev)
	copy(saved, b.buf[pos-prev:pos])
	copy(b.buf[pos-prev:], b.buf[pos:pos+next])
	copy(b.buf[pos-prev+next:], saved)
	return pos - prev
}

// FindLineStart returns the start of the line that pos is on.
func (b *Buffer) FindLineStart(pos int) int { return FindLineStart(b.buf, pos) }

// FindLineEnd returns the end of the line that pos is on.
func (b *Buffer) FindLineEnd(pos int) int { return FindLineEnd(b.buf, pos) }

// FindWordStart returns the start of the word before pos.
func (b *Buffer) FindWordStart(pos int) int { return FindWordStart(b.buf, pos) }

// FindWordEnd returns the end of the word after pos.
func (b *Buffer) FindWordEnd(pos int) int { return FindWordEnd(b.buf, pos) }

// FindWSWordStart returns the start of the run of non-whitespace before pos.
func (b *Buffer) FindWSWordStart(pos int) int { return FindWSWordStart(b.buf, pos) }

// FindWSWordEnd returns the end of the run of non-whitespace after pos.
func (b *Buffer) FindWSWordEnd(pos int) int { return FindWSWordEnd(b.buf, pos) }

// ForEachRow calls fn once for each screen row and returns the number of rows.
func (b *Buffer) ForEachRow(termWidth, promptWidth, contWidth int, fn rowFunc) int {
	return ForEachRow(b.buf, termWidth, promptWidth, contWidth, fn)
}

// RowColAtPos returns the number of rows and where pos sits.
func (b *Buffer) RowColAtPos(termWidth, promptWidth, contWidth, pos int) (int, RowCol) {
	return RowColAtPos(b.buf, termWidth, promptWidth, contWidth, pos)
}

// PosAtRowCol returns the position at row and col, or -1 when there is no
// such row.
func (b *Buffer) PosAtRowCol(termWidth, promptWidth, contWidth, row, col int) int {
	return PosAtRowCol(b.buf, termWidth, promptWidth, contWidth, row, col)
}

// WrappedRowColAtPos returns where pos sits once the terminal is resized to
// newTermWidth, and how many rows the text then takes.
func (b *Buffer) WrappedRowColAtPos(termWidth, newTermWidth, promptWidth, contWidth, pos int) (int, RowCol) {
	return WrappedRowColAtPos(b.buf, termWidth, newTermWidth, promptWidth, contWidth, pos)
}

// DecodeFromLocale returns the contents of b with everything a terminal that
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
func (b *Buffer) DecodeFromLocale() []byte {
	if len(b.buf) == 0 {
		return nil
	}
	out := make([]byte, 0, len(b.buf))
	for i := 0; i < len(b.buf); {
		ofs, _ := b.NextOfs(i)
		switch {
		case ofs <= 0:
			return out
		case ofs == 1:
			out = append(out, b.buf[i])
		case b.buf[i] == '\x1B':
			// An escape sequence shows nothing, so drop it.
		default:
			r, _ := DecodeRune(b.buf[i : i+ofs])
			if c, ok := RawByte(r); ok {
				out = append(out, c)
			} else if r <= 0x7F {
				out = append(out, byte(r))
			}
		}
		i += ofs
	}
	return out
}

// --------------------------------------------------------------------------
// strwidth.go

// Column width and navigation over a byte slice.
//
// These functions move by character, not by byte, and they treat an escape
// sequence as a single character of width zero. They do not validate UTF-8.
// A byte that cannot start a sequence, or a sequence that runs off the end of
// the slice, counts as one byte of width one.
//
// Ported from isocline/src/stringbuf.c.

// CharSetHas reports whether c is in set, with the rule that C's strchr uses:
// the terminating zero of the set counts as a member, so a zero byte is
// always found. The C code relies on this in more than one place, and the
// results differ without it.
func CharSetHas(set string, c byte) bool {
	if c == 0 {
		return true
	}
	for i := range len(set) {
		if set[i] == c {
			return true
		}
	}
	return false
}

// utf8CharWidth returns the column width of the character that starts s.
//
// It does not check the continuation bytes, so an invalid sequence still
// decodes and the width comes out of whatever code point that gives. The C
// code does the same, and the comment there says the check is not necessary
// because it does not validate.
func utf8CharWidth(s []byte) int {
	if len(s) == 0 {
		return 0
	}
	b := s[0]
	switch {
	case b < ' ':
		return 0
	case b <= 0x7F:
		// DEL is width 1 here, although runeWidth gives it -1. The C code
		// never asks runeWidth about a single byte.
		return 1
	case b <= 0xC1:
		// A continuation byte with nothing in front of it, or one of the two
		// bytes that can only start an overlong sequence.
		return 1
	case b <= 0xDF && len(s) >= 2:
		return runeWidth(rune(b&0x1F)<<6 | rune(s[1]&0x3F))
	case b <= 0xEF && len(s) >= 3:
		return runeWidth(rune(b&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F))
	case b <= 0xF4 && len(s) >= 4:
		return runeWidth(rune(b&0x07)<<18 | rune(s[1]&0x3F)<<12 |
			rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F))
	}
	return 1
}

// charWidth returns the column width of the character that starts s.
//
// The C code can return -1 here, for a control character written as more than
// one byte, such as U+0080 written as 0xC2 0x80. Nothing in the C code guards
// against that, so widths are added up without a floor and a string of those
// has a negative width. runeWidth returns 0 for the same characters, because
// go-runewidth does, so this port returns 0 and cannot go negative.
// testdata/wcwidth-delta.txt records the ranges, and the first two lines of
// that file are exactly this difference.
//
// The C code raises the result to 1 on Windows. Windows is not a target yet,
// and the recorded sessions come from a build that does not take that branch.
func charWidth(s []byte) int {
	if len(s) == 0 || s[0] < ' ' {
		return 0
	}
	return utf8CharWidth(s)
}

// StrWidth returns the column width of s.
func StrWidth(s []byte) int {
	width := 0
	for pos := 0; pos < len(s); {
		ofs, w := NextOfs(s, pos)
		if ofs <= 0 {
			break
		}
		width += w
		pos += ofs
	}
	return width
}

// skipEsc reports whether s starts an escape sequence, and returns the length
// of that sequence.
//
// Two quirks of the C code survive here, because the escape decoder and the
// terminal writer both depend on them.
//
// The first is that this says yes to every byte that follows an escape. A
// sequence that names a terminator but never reaches one, such as "\x1b[31"
// at the end of a slice, falls through and counts as two bytes. So does any
// byte the C code has no rule for. The only answers of no are a slice of one
// byte or less, and a slice that does not start with an escape.
//
// The second is that a zero byte after the escape enters the branch for
// "[PX^_]", because C's strchr finds the terminating zero of that set.
func skipEsc(s []byte) (int, bool) {
	if len(s) <= 1 || s[0] != '\x1B' {
		return 0, false
	}
	if CharSetHas("[PX^_]", s[1]) {
		// CSI (ESC [), DCS (ESC P), SOS (ESC X), PM (ESC ^), APC (ESC _) and
		// OSC (ESC ]) run until a terminator. CSI ends on a byte from 0x40 to
		// 0x7F. The rest end on a bell, or on ESC backslash.
		//
		// ESC ] is not in the set, so OSC reaches this branch only through
		// the zero byte rule above. The C code has the same gap.
		finalCSI := s[1] == '['
		for n := 2; n < len(s); {
			c := s[n]
			n++
			switch {
			case finalCSI && c >= 0x40 && c <= 0x7F,
				!finalCSI && c == '\x07',
				c == '\x02':
				return n, true
			case !finalCSI && c == '\x1B' && n < len(s) && s[n] == '\\':
				return n + 1, true
			}
		}
	}
	// Every other escape counts as two bytes. The C code tests the set
	// " #%()*+" here and then does the same thing in both branches, so the
	// test changes nothing.
	return 2, true
}

// NextOfs returns the number of bytes from pos to the next character, and the
// column width of the character at pos. An escape sequence counts as one
// character. A pos at or past the end returns zero and zero.
func NextOfs(s []byte, pos int) (int, int) {
	ofs := 0
	if pos >= 0 && pos < len(s) {
		if n, ok := skipEsc(s[pos:]); ok {
			ofs = n
		} else {
			ofs = 1
			for pos+ofs < len(s) && IsCont(s[pos+ofs]) {
				ofs++
			}
		}
	}
	if ofs == 0 {
		return 0, 0
	}
	return ofs, charWidth(s[pos : pos+ofs])
}

// PrevOfs returns the number of bytes from pos back to the previous
// character, and the column width of that character.
//
// This does not step back over an escape sequence, because reading backwards
// cannot tell one from ordinary text. The C code says the same.
// A pos past the end is not clamped, because the C code does not clamp it
// either. C reads the terminating zero there and measures a character of
// width zero, so byteAt supplies that zero and the width comes out the same.
func PrevOfs(s []byte, pos int) (int, int) {
	if s == nil {
		// The C code tests its pointer against null here and answers zero.
		// An empty buffer holds a null pointer, while an empty string does
		// not, so the two give different answers and the port keeps that.
		return 0, 0
	}
	ofs := 0
	if pos > 0 {
		ofs = 1
		for pos > ofs && IsCont(ByteAt(s, pos-ofs)) {
			ofs++
		}
	}
	if ofs == 0 {
		return 0, 0
	}
	lo, hi := pos-ofs, min(pos, len(s))
	if lo >= hi {
		return ofs, 0
	}
	return ofs, charWidth(s[lo:hi])
}

// ByteAt returns the byte at i, or zero when i is outside s. A C string ends
// in a zero byte, so reading at or past its length gives zero there too.
func ByteAt(s []byte, i int) byte {
	if i < 0 || i >= len(s) {
		return 0
	}
	return s[i]
}

// SkipUntilFit returns the offset of the longest tail of s whose width is at
// most maxWidth.
func SkipUntilFit(s []byte, maxWidth int) int {
	width := StrWidth(s)
	pos := 0
	for width > maxWidth {
		ofs, w := NextOfs(s, pos)
		if ofs <= 0 {
			break
		}
		width -= w
		pos += ofs
	}
	return pos
}

// TakeWhileFit returns the length of the longest head of s whose width is at
// most maxWidth.
func TakeWhileFit(s []byte, maxWidth int) int {
	pos, width := 0, 0
	for {
		ofs, w := NextOfs(s, pos)
		if ofs <= 0 || width+w > maxWidth {
			return pos
		}
		width += w
		pos += ofs
	}
}

// --------------------------------------------------------------------------
// strfind.go

// Search for the start and the end of a line, a word, or a run of
// non-whitespace. These move by character, so a multi byte character is one
// step.
//
// Ported from isocline/src/stringbuf.c.

// findBackward returns the position of the first character at or before pos
// that belongs to class, or -1 when there is none.
//
// The returned position is the one after that character, not the position of
// the character itself, because the search walks backwards and reports where
// it stood when the test passed.
//
// When skipMatches is set, the search first steps over every character that
// belongs to class. The word search needs that, so that a cursor already
// sitting on white space finds the word in front of the white space rather
// than stopping at once.
func findBackward(s []byte, pos int, class CharClass, skipMatches bool) int {
	pos = min(max(pos, 0), len(s))
	i := pos
	if skipMatches {
		for {
			prev, _ := PrevOfs(s, i)
			if prev <= 0 || !class(s[i-prev:i]) {
				break
			}
			i -= prev
			if i <= 0 {
				break
			}
		}
	}
	for {
		prev, _ := PrevOfs(s, i)
		if prev <= 0 {
			break
		}
		if class(s[i-prev : i]) {
			return i
		}
		i -= prev
		if i <= 0 {
			break
		}
	}
	return -1
}

// findForward returns the position of the first character at or after pos
// that belongs to class, or -1 when there is none.
func findForward(s []byte, pos int, class CharClass, skipMatches bool) int {
	pos = min(max(pos, 0), len(s))
	i := pos
	if skipMatches {
		for {
			next, _ := NextOfs(s, i)
			if next <= 0 || !class(s[i:i+next]) {
				break
			}
			i += next
			if i >= len(s) {
				break
			}
		}
	}
	for {
		next, _ := NextOfs(s, i)
		if next <= 0 {
			break
		}
		if class(s[i : i+next]) {
			return i
		}
		i += next
		if i >= len(s) {
			break
		}
	}
	return -1
}

// charIsLineFeed reports whether s is a newline.
//
// A zero byte counts as one. The C code cannot reach that, because a C string
// ends at the first zero byte and the search never looks at the terminator.
// The port keeps the test, because a Go slice can hold a zero byte and the C
// rule is the one the corpus records.
func charIsLineFeed(s []byte) bool {
	return len(s) == 1 && (s[0] == '\n' || s[0] == 0)
}

// FindLineStart returns the position just after the newline that precedes
// pos, or 0 when pos is on the first line.
func FindLineStart(s []byte, pos int) int {
	return max(findBackward(s, pos, charIsLineFeed, false), 0)
}

// FindLineEnd returns the position of the newline at or after pos, or the
// length of s when there is none.
func FindLineEnd(s []byte, pos int) int {
	if end := findForward(s, pos, charIsLineFeed, false); end >= 0 {
		return end
	}
	return len(s)
}

// FindWordStart returns the position of the start of the word before pos, or
// 0 when there is none. A word is a run of identifier letters.
func FindWordStart(s []byte, pos int) int {
	return max(findBackward(s, pos, CharIsIDLetter, true), 0)
}

// FindWordEnd returns the position of the end of the word after pos, or the
// length of s when there is none.
func FindWordEnd(s []byte, pos int) int {
	if end := findForward(s, pos, CharIsIDLetter, true); end >= 0 {
		return end
	}
	return len(s)
}

// FindWSWordStart returns the position of the start of the run of
// non-whitespace before pos, or 0 when there is none.
func FindWSWordStart(s []byte, pos int) int {
	return max(findBackward(s, pos, charIsWhite, true), 0)
}

// FindWSWordEnd returns the position of the end of the run of non-whitespace
// after pos, or the length of s when there is none.
func FindWSWordEnd(s []byte, pos int) int {
	if end := findForward(s, pos, charIsWhite, true); end >= 0 {
		return end
	}
	return len(s)
}

// --------------------------------------------------------------------------
// rowcol.go

// Rows and columns. The edit loop draws a line that can be longer than the
// terminal is wide and that can hold newlines, so it needs to know which
// screen row and column a position in the buffer lands on.
//
// Ported from isocline/src/stringbuf.c.

// RowCol says where a position sits on the screen, and how long its row is.
type RowCol struct {
	Row      int
	Col      int
	RowStart int
	RowLen   int

	FirstOnRow bool
	LastOnRow  bool
}

// rowFunc is called once for each screen row. s is the whole text, rowStart
// and rowLen give the row inside it, and startWidth is the width of the
// prompt in front of the row. isWrap says the row ended because it filled the
// terminal, not because of a newline. Returning true stops the walk.
type rowFunc func(s []byte, row, rowStart, rowLen, startWidth int, isWrap bool) bool

// ForEachRow calls fn once for each screen row of s and returns the number of
// rows. termWidth of 0 means do not wrap. promptWidth is the width of the
// prompt on the first row, and contWidth the width of the prompt on every row
// after it.
//
// A row that wraps and then ends in a newline calls fn twice, once for each,
// and both calls carry the startWidth that was current before the wrap. The C
// code computes that value at the top of the loop and does not recompute it
// after the wrap, so the second call reports the prompt width of the row the
// text came from rather than of the row it moved to.
//
// When fn stops the walk the result is the row it stopped on, which is one
// less than the count that a complete walk returns for the same text.
func ForEachRow(s []byte, termWidth, promptWidth, contWidth int, fn rowFunc) int {
	count, col, start := 0, 0, 0
	startWidth := promptWidth
	i := 0
	for i < len(s) {
		next, w := NextOfs(s, i)
		if next <= 0 {
			// Unreachable for a slice, because nextOfs returns at least one
			// byte for every position inside it. The C code asserts here.
			break
		}
		if count == 0 {
			startWidth = promptWidth
		} else {
			startWidth = contWidth
		}
		// The +1 leaves room for the cursor, which sits after the character.
		termCol := col + w + startWidth + 1
		if termWidth != 0 && i != 0 && termCol >= termWidth {
			if fn != nil && fn(s, count, start, i-start, startWidth, true) {
				return count
			}
			count++
			start = i
			col = 0
		}
		if s[i] == '\n' {
			if fn != nil && fn(s, count, start, i-start, startWidth, false) {
				return count
			}
			count++
			start = i + 1
			col = 0
		}
		i += next
		col += w
	}
	if fn != nil && fn(s, count, start, i-start, startWidth, false) {
		return count
	}
	return count + 1
}

// RowColAtPos returns the number of rows that s takes, and where pos sits.
func RowColAtPos(s []byte, termWidth, promptWidth, contWidth, pos int) (int, RowCol) {
	var rc RowCol
	rows := ForEachRow(s, termWidth, promptWidth, contWidth,
		func(s []byte, row, rowStart, rowLen, _ int, isWrap bool) bool {
			if pos < rowStart || pos > rowStart+rowLen {
				return false // keep going, so that every row is counted
			}
			rc.RowStart = rowStart
			rc.RowLen = rowLen
			rc.Row = row
			rc.Col = StrWidth(s[rowStart:pos])
			rc.FirstOnRow = pos == rowStart
			if isWrap {
				// On a wrapped row the last position is the one whose
				// character reaches the end of the row.
				next, _ := NextOfs(s[:rowStart+rowLen], pos)
				rc.LastOnRow = pos+next >= rowStart+rowLen
			} else {
				rc.LastOnRow = pos >= rowStart+rowLen
			}
			return false
		})
	return rows, rc
}

// PosAtRowCol returns the position in s that sits at row and col, or -1 when
// s has no such row. col does not count the prompt.
func PosAtRowCol(s []byte, termWidth, promptWidth, contWidth, row, col int) int {
	pos := -1
	ForEachRow(s, termWidth, promptWidth, contWidth,
		func(s []byte, r, rowStart, rowLen, _ int, _ bool) bool {
			if r != row {
				return false
			}
			w, i := 0, rowStart
			end := rowStart + rowLen
			for w < col && i < end {
				next, cw := NextOfs(s[:end], i)
				if next <= 0 {
					break
				}
				i += next
				w += cw
			}
			pos = i
			return true
		})
	return pos
}

// WrappedRowColAtPos returns where pos sits after the terminal is resized to
// newTermWidth, and how many rows the text then takes.
//
// The text was laid out for termWidth and the terminal has not redrawn it, so
// each of those rows can spill over several rows of the new width. Those
// extra rows are hard wraps: the terminal broke the line itself, with no
// newline in the text.
func WrappedRowColAtPos(s []byte, termWidth, newTermWidth, promptWidth, contWidth, pos int) (int, RowCol) {
	var rc RowCol
	hardRows := 0
	rows := ForEachRow(s, termWidth, promptWidth, contWidth,
		func(s []byte, row, rowStart, rowLen, startWidth int, isWrap bool) bool {
			width := startWidth
			// The walk goes one past the last character, because the cursor
			// can sit just after it.
			for i := 0; i <= rowLen; {
				var cw, next int
				isCursor := pos == rowStart+i
				if i < rowLen {
					next, cw = NextOfs(s[rowStart:rowStart+rowLen], i)
				} else {
					// A wrap draws a back arrow and carries an invisible
					// newline, so it takes two columns.
					switch {
					case isWrap:
						cw = 2
					case isCursor:
						cw = 1
					}
					next = 1
				}
				if next > 0 {
					if width+cw > newTermWidth {
						width = 0
						hardRows++
					}
				} else {
					next++ // make sure the walk ends
				}
				if isCursor {
					rc.RowStart = rowStart
					rc.RowLen = rowLen
					rc.Row = hardRows + row
					rc.Col = width
					rc.FirstOnRow = i == 0
					last := rowLen
					if isWrap {
						last--
					}
					rc.LastOnRow = i+next >= last
				}
				width += cw
				i += next
			}
			return false
		})
	return rows + hardRows, rc
}

// --------------------------------------------------------------------------
// width.go

// Column width of a character.
//
// isocline carries its own copy of the wcwidth function of Markus Kuhn, in
// wcwidth.c. The port does not carry that table. It calls go-runewidth
// instead, which follows a newer version of Unicode and which the author of
// this package already depends on elsewhere.
//
// The two do not agree everywhere. testdata/wcwidth.txt records the width that
// the C code gives every code point, and testdata/wcwidth-delta.txt records
// every range where go-runewidth gives a different answer. TestWidthDelta
// keeps that record current, so an upgrade of go-runewidth shows up as a
// change to a committed file rather than as a silent shift in cursor
// positions.
//
// One difference is systematic. The C function returns -1 for a character that
// a terminal cannot print, such as a control character. go-runewidth returns 0
// for those. The callers in stringbuf.c treat a negative width as an error, so
// the port must make that check where it uses this function, not here.

// runeWidth returns the number of terminal columns that r occupies. The result
// is 0, 1 or 2.
func runeWidth(r rune) int {
	return runewidth.RuneWidth(r)
}

// --------------------------------------------------------------------------
// charclass.go

// Character classes. A class answers whether one character belongs to a set.
// The word and line search in strfind.go takes one of these, and so does the
// token matching that a highlighter uses.
//
// Each of these takes the bytes of a single character, not a whole string.
// Some of them refuse anything but one byte, so no multi byte character is
// ever white space or a separator or a digit. The ones that accept a longer
// character look at its first byte only, and every byte from 0x80 up counts
// as a letter, whatever it decodes to.
//
// Ported from isocline/src/stringbuf.c.

// CharClass reports whether the character in s belongs to a set.
type CharClass func(s []byte) bool

// charIsWhite reports whether s is one byte of white space.
func charIsWhite(s []byte) bool {
	if len(s) != 1 {
		return false
	}
	c := s[0]
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// charIsNonWhite reports whether s is not one byte of white space. A character
// of more than one byte is therefore non white.
func charIsNonWhite(s []byte) bool {
	return !charIsWhite(s)
}

// separators is the set that charIsSeparator takes.
const separators = " \t\r\n,.;:/\\(){}[]"

// charIsSeparator reports whether s is one byte that separates words.
//
// A zero byte counts as a separator, because the C code asks strchr and
// strchr finds the terminating zero of the set. See charSetHas.
func charIsSeparator(s []byte) bool {
	return len(s) == 1 && CharSetHas(separators, s[0])
}

// CharIsNonSeparator reports whether s does not separate words.
func CharIsNonSeparator(s []byte) bool {
	return !charIsSeparator(s)
}

// charIsDigit reports whether s is one decimal digit.
func charIsDigit(s []byte) bool {
	return len(s) == 1 && s[0] >= '0' && s[0] <= '9'
}

// charIsHexDigit reports whether s is one hexadecimal digit.
func charIsHexDigit(s []byte) bool {
	if len(s) != 1 {
		return false
	}
	c := s[0]
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// charIsLetter reports whether s is a letter. Every byte from 0x80 up counts,
// so any character outside ASCII is a letter without being decoded.
func charIsLetter(s []byte) bool {
	if len(s) == 0 {
		return false
	}
	c := s[0]
	return c >= 0x80 || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// CharIsIDLetter reports whether s can appear in an identifier. That is a
// letter, a digit, an underscore, a hyphen, or any byte from 0x80 up. The word
// search uses this class.
func CharIsIDLetter(s []byte) bool {
	if len(s) == 0 {
		return false
	}
	c := s[0]
	return c >= 0x80 || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '_' || c == '-'
}

// fileNameStops is the set of bytes that cannot appear in a file name.
const fileNameStops = " \t\r\n`@$><=;|&{}()[]"

// CharIsFileNameLetter reports whether s can appear in a file name.
//
// A zero byte is not a file name letter, for the same strchr rule that makes
// it a separator.
func CharIsFileNameLetter(s []byte) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 0x80 || !CharSetHas(fileNameStops, s[0])
}

// isToken returns the length of the token that starts at pos, or -1 when pos
// does not start one.
//
// A token starts at pos only when the byte in front of pos is not in the
// class. That one byte is tested on its own, whatever character it belongs
// to, so a position inside a multi byte character can look like a start.
func isToken(s []byte, pos int, class CharClass) int {
	if pos < 0 || pos >= len(s) || class == nil {
		return -1
	}
	if pos > 0 && class(s[pos-1:pos]) {
		return -1
	}
	i := pos
	for i < len(s) {
		next, _ := NextOfs(s, i)
		if next <= 0 {
			return -1
		}
		if !class(s[i : i+next]) {
			break
		}
		i += next
	}
	return i - pos
}

// matchToken returns the length of the token at pos when it is exactly token,
// and 0 otherwise. It does not match a prefix or a suffix of a longer token,
// so matching "fun" against "function" gives 0.
func matchToken(s []byte, pos int, class CharClass, token string) int {
	n := isToken(s, pos, class)
	if n > 0 && n == len(token) && string(s[pos:pos+n]) == token {
		return n
	}
	return 0
}

// matchAnyToken returns the length of the token at pos when it is exactly one
// of tokens, and 0 otherwise.
func matchAnyToken(s []byte, pos int, class CharClass, tokens []string) int {
	n := isToken(s, pos, class)
	if n <= 0 {
		return 0
	}
	for _, token := range tokens {
		if n == len(token) && string(s[pos:pos+n]) == token {
			return n
		}
	}
	return 0
}

// prevChar returns the position of the character before pos, or -1 when there
// is none.
func prevChar(s []byte, pos int) int {
	if pos < 0 || pos > len(s) {
		return -1
	}
	ofs, _ := PrevOfs(s, pos)
	if ofs <= 0 {
		return -1
	}
	return pos - ofs
}

// nextChar returns the position of the character after pos, or -1 when there
// is none.
func nextChar(s []byte, pos int) int {
	if pos < 0 || pos > len(s) {
		return -1
	}
	ofs, _ := NextOfs(s, pos)
	if ofs <= 0 {
		return -1
	}
	return pos + ofs
}

// CountEndOverlap returns the length of the longest prefix of postfix that is
// also a suffix of s.
func CountEndOverlap(s, postfix string) int {
	for count := len(postfix); count > 0; count-- {
		if count <= len(s) && s[len(s)-count:] == postfix[:count] {
			return count
		}
	}
	return 0
}

// --------------------------------------------------------------------------
// qutf8.go

// QUTF-8 is "quite like UTF-8". A decoder reads valid UTF-8 as usual, and maps
// every byte it cannot read to a code point in the raw plane, which runs from
// U+EE000 to U+EE0FF. Encoding a raw code point gives the original byte back,
// so a line that holds bytes from some other encoding survives a round trip.
//
// This is not what the unicode/utf8 package does. That package reports
// utf8.RuneError for a byte it cannot read, and the byte itself is lost.
//
// Ported from isocline/src/common.c.

// rawBase is the first code point of the raw plane.
const rawBase = 0xEE000

// RawRune returns the raw plane code point that stands for the byte b.
func RawRune(b byte) rune {
	return rawBase + rune(b)
}

// RawByte returns the byte that r stands for, and reports whether r is a raw
// plane code point at all.
func RawByte(r rune) (byte, bool) {
	if r >= rawBase && r <= rawBase+0xFF {
		return byte(r - rawBase), true
	}
	return 0, false
}

// IsCont reports whether b is a UTF-8 continuation byte.
func IsCont(b byte) bool {
	return b&0xC0 == 0x80
}

// AppendRune appends the QUTF-8 encoding of r to dst.
//
// A raw plane code point appends the single byte that it stands for. A code
// point above U+10FFFF appends nothing.
//
// A surrogate code point is encoded rather than refused, but decodeRune will
// not read one back. The encoder and the decoder disagree there. The C code
// disagrees in the same way.
func AppendRune(dst []byte, r rune) []byte {
	switch {
	case r < 0:
		return dst
	case r <= 0x7F:
		return append(dst, byte(r))
	case r <= 0x7FF:
		return append(dst,
			0xC0|byte(r>>6),
			0x80|(byte(r)&0x3F))
	case r <= 0xFFFF:
		return append(dst,
			0xE0|byte(r>>12),
			0x80|(byte(r>>6)&0x3F),
			0x80|(byte(r)&0x3F))
	case r <= 0x10FFFF:
		if b, ok := RawByte(r); ok {
			return append(dst, b)
		}
		return append(dst,
			0xF0|byte(r>>18),
			0x80|(byte(r>>12)&0x3F),
			0x80|(byte(r>>6)&0x3F),
			0x80|(byte(r)&0x3F))
	}
	return dst
}

// DecodeRune reads the first character of s. It returns the character and the
// number of bytes it used.
//
// A byte that does not start a valid sequence returns the raw plane code point
// for that byte, and a count of one. Empty input returns zero and zero, where
// the C code reads past the end of the buffer instead.
func DecodeRune(s []byte) (rune, int) {
	if len(s) == 0 {
		return 0, 0
	}
	c0 := s[0]
	if c0 <= 0x7F {
		return rune(c0), 1
	}
	// 0xC0 and 0xC1 start an overlong two byte sequence, and anything below
	// them is a continuation byte with nothing in front of it.
	if c0 > 0xC1 {
		if c0 <= 0xDF && len(s) >= 2 && IsCont(s[1]) {
			return rune(c0&0x1F)<<6 | rune(s[1]&0x3F), 2
		}
		if len(s) >= 3 && isLead3(c0, s[1]) && IsCont(s[2]) {
			return rune(c0&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
		}
		if len(s) >= 4 && isLead4(c0, s[1]) && IsCont(s[2]) && IsCont(s[3]) {
			return rune(c0&0x07)<<18 | rune(s[1]&0x3F)<<12 |
				rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F), 4
		}
	}
	return RawRune(c0), 1
}

// isLead3 reports whether c0 and c1 can start a three byte sequence.
//
// The rule refuses an overlong encoding and a UTF-16 surrogate half. It also
// refuses the pair 0xED 0x80, which encodes U+D000 to U+D03F. Those are
// ordinary characters, so the C decoder cannot read them. The port keeps that
// behavior, because the recorded sessions come from the C build.
func isLead3(c0, c1 byte) bool {
	switch {
	case c0 == 0xE0:
		return c1 >= 0xA0 && c1 <= 0xBF
	case c0 == 0xED:
		return c1 > 0x80 && c1 <= 0x9F
	case c0 >= 0xE1 && c0 <= 0xEF:
		return IsCont(c1)
	}
	return false
}

// isLead4 reports whether c0 and c1 can start a four byte sequence. The rule
// refuses an overlong encoding and a code point above U+10FFFF.
func isLead4(c0, c1 byte) bool {
	switch {
	case c0 == 0xF0:
		return c1 >= 0x90 && c1 <= 0xBF
	case c0 >= 0xF1 && c0 <= 0xF3:
		return IsCont(c1)
	case c0 == 0xF4:
		return c1 >= 0x80 && c1 <= 0x8F
	}
	return false
}

// --------------------------------------------------------------------------
// ascii.go

// Case-insensitive comparison, by the ASCII rule only. Only the letters A to Z
// change case. A byte above 0x7f never changes, so "é" and "É" do not match.
//
// This is not what strings.EqualFold does. That function folds by the Unicode
// rules, and it does match "é" against "É".
//
// Ported from isocline/src/common.c.

// ASCIILower returns c in lower case, by the ASCII rule only.
func ASCIILower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c - 'A' + 'a'
	}
	return c
}

// CompareFold compares a and b without regard to ASCII case. It returns -1, 0
// or 1.
//
// The length decides first. A shorter string sorts before a longer one,
// whatever the two hold, so this is not an alphabetical order. "b" sorts
// before "aa". Strings of equal length compare byte by byte, with the signed
// order that compareFoldN describes.
func CompareFold(a, b string) int {
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return CompareFoldN(a, b, len(a))
}

// CompareFoldN compares the first n bytes of a and b without regard to ASCII
// case. It returns -1, 0 or 1.
//
// The bytes compare as signed values, so any byte above 0x7f sorts before
// every ASCII character. "a" is greater than "\xff", not less. The C code
// compares C char values, and char is signed on x86 and on x86-64, which is
// where the recorded corpus comes from.
//
// This makes the C code sort differently on a machine where char is unsigned,
// which is the default on ARM. The port keeps the x86 order, because that is
// what the corpus records. See the note in PLAN.md.
//
// When a runs out before n bytes and b keeps going, the result is -1.
func CompareFoldN(a, b string, n int) int {
	i := 0
	for ; i < len(a) && i < n; i++ {
		c1 := int8(ASCIILower(a[i]))
		var c2 int8
		if i < len(b) {
			c2 = int8(ASCIILower(b[i]))
		}
		switch {
		case c1 < c2:
			return -1
		case c1 > c2:
			return 1
		}
	}
	if i >= n || i >= len(b) {
		return 0
	}
	return -1
}

// HasPrefixFold reports whether s starts with prefix, without regard to ASCII
// case.
func HasPrefixFold(s, prefix string) bool {
	i := 0
	for ; i < len(s) && i < len(prefix); i++ {
		if ASCIILower(s[i]) != ASCIILower(prefix[i]) {
			return false
		}
	}
	return i >= len(prefix)
}

// indexFold returns the position of the first substr in s, without regard to
// ASCII case, or -1 when s does not hold it. An empty substr returns 0.
func indexFold(s, substr string) int {
	if substr == "" {
		return 0
	}
	for i := range len(s) {
		if CompareFoldN(s[i:], substr, len(substr)) == 0 {
			return i
		}
	}
	return -1
}

// ContainsFold reports whether s holds substr, without regard to ASCII case.
func ContainsFold(s, substr string) bool {
	return indexFold(s, substr) >= 0
}

// --------------------------------------------------------------------------
// parse.go

// Number parsing for the replies that a terminal sends. A terminal answers a
// query with a sequence such as "\x1b[24;80R", and these read the numbers out
// of the part between the brackets.
//
// The C code calls sscanf, so these keep what sscanf does: leading white
// space is skipped, a sign is allowed, and parsing stops at the first byte
// that is not a digit rather than refusing the whole string. "12x" gives 12.
//
// Ported from isocline/src/stringbuf.c.

// isSpace reports whether c is white space to the C library, which counts
// the vertical tab and the form feed as well.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

// scanInt reads one decimal number from s, starting at i. It returns the text
// it matched and the index just after it. It reports false when no digit
// follows, in which case the caller assigns nothing, as sscanf does.
func scanInt(s string, i int) (string, int, bool) {
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == digits {
		return "", start, false
	}
	return s[start:i], i, true
}

// Atoz reads one decimal number from s.
//
// A number too large for an int is not defined by C and not defined here
// either. This returns false for it, where sscanf would store something.
func Atoz(s string) (int, bool) {
	text, _, ok := scanInt(s, 0)
	if !ok {
		return 0, false
	}
	v, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return v, true
}

// atoz2 reads two decimal numbers from s, separated by a semicolon. It
// returns both numbers and how many of them it read.
//
// The caller must look at the count. Reading "12" gives the first number and
// a count of 1, and the second number is meaningless. The semicolon has to
// come straight after the first number, because a literal character in a
// scanf format does not skip white space, so " 1 ; 2 " reads only the 1.
func atoz2(s string) (int, int, int) {
	first, i, ok := scanInt(s, 0)
	if !ok {
		return 0, 0, 0
	}
	a, err := strconv.Atoi(first)
	if err != nil {
		return 0, 0, 0
	}
	if i >= len(s) || s[i] != ';' {
		return a, 0, 1
	}
	second, _, ok := scanInt(s, i+1)
	if !ok {
		return a, 0, 1
	}
	b, err := strconv.Atoi(second)
	if err != nil {
		return a, 0, 1
	}
	return a, b, 2
}

// atou32 reads one decimal number from s as an unsigned 32 bit value.
//
// A leading minus is accepted and the result wraps, because that is what
// sscanf does with the %u conversion. "-5" gives 4294967291.
func atou32(s string) (uint32, bool) {
	text, _, ok := scanInt(s, 0)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, false
	}
	return uint32(v), true //nolint:gosec // the wrap is what %u does
}
