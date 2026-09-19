package rline

// Column width and navigation over a byte slice.
//
// These functions move by character, not by byte, and they treat an escape
// sequence as a single character of width zero. They do not validate UTF-8.
// A byte that cannot start a sequence, or a sequence that runs off the end of
// the slice, counts as one byte of width one.
//
// Ported from isocline/src/stringbuf.c.

// charSetHas reports whether c is in set, with the rule that C's strchr uses:
// the terminating zero of the set counts as a member, so a zero byte is
// always found. The C code relies on this in more than one place, and the
// results differ without it.
func charSetHas(set string, c byte) bool {
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

// strWidth returns the column width of s.
func strWidth(s []byte) int {
	width := 0
	for pos := 0; pos < len(s); {
		ofs, w := nextOfs(s, pos)
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
	if charSetHas("[PX^_]", s[1]) {
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

// nextOfs returns the number of bytes from pos to the next character, and the
// column width of the character at pos. An escape sequence counts as one
// character. A pos at or past the end returns zero and zero.
func nextOfs(s []byte, pos int) (int, int) {
	ofs := 0
	if pos >= 0 && pos < len(s) {
		if n, ok := skipEsc(s[pos:]); ok {
			ofs = n
		} else {
			ofs = 1
			for pos+ofs < len(s) && isCont(s[pos+ofs]) {
				ofs++
			}
		}
	}
	if ofs == 0 {
		return 0, 0
	}
	return ofs, charWidth(s[pos : pos+ofs])
}

// prevOfs returns the number of bytes from pos back to the previous
// character, and the column width of that character.
//
// This does not step back over an escape sequence, because reading backwards
// cannot tell one from ordinary text. The C code says the same.
// A pos past the end is not clamped, because the C code does not clamp it
// either. C reads the terminating zero there and measures a character of
// width zero, so byteAt supplies that zero and the width comes out the same.
func prevOfs(s []byte, pos int) (int, int) {
	if s == nil {
		// The C code tests its pointer against null here and answers zero.
		// An empty buffer holds a null pointer, while an empty string does
		// not, so the two give different answers and the port keeps that.
		return 0, 0
	}
	ofs := 0
	if pos > 0 {
		ofs = 1
		for pos > ofs && isCont(byteAt(s, pos-ofs)) {
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

// byteAt returns the byte at i, or zero when i is outside s. A C string ends
// in a zero byte, so reading at or past its length gives zero there too.
func byteAt(s []byte, i int) byte {
	if i < 0 || i >= len(s) {
		return 0
	}
	return s[i]
}

// skipUntilFit returns the offset of the longest tail of s whose width is at
// most maxWidth.
func skipUntilFit(s []byte, maxWidth int) int {
	width := strWidth(s)
	pos := 0
	for width > maxWidth {
		ofs, w := nextOfs(s, pos)
		if ofs <= 0 {
			break
		}
		width -= w
		pos += ofs
	}
	return pos
}

// takeWhileFit returns the length of the longest head of s whose width is at
// most maxWidth.
func takeWhileFit(s []byte, maxWidth int) int {
	pos, width := 0, 0
	for {
		ofs, w := nextOfs(s, pos)
		if ofs <= 0 || width+w > maxWidth {
			return pos
		}
		width += w
		pos += ofs
	}
}
