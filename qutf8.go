package rline

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

// rawRune returns the raw plane code point that stands for the byte b.
func rawRune(b byte) rune {
	return rawBase + rune(b)
}

// rawByte returns the byte that r stands for, and reports whether r is a raw
// plane code point at all.
func rawByte(r rune) (byte, bool) {
	if r >= rawBase && r <= rawBase+0xFF {
		return byte(r - rawBase), true
	}
	return 0, false
}

// isCont reports whether b is a UTF-8 continuation byte.
func isCont(b byte) bool {
	return b&0xC0 == 0x80
}

// appendRune appends the QUTF-8 encoding of r to dst.
//
// A raw plane code point appends the single byte that it stands for. A code
// point above U+10FFFF appends nothing.
//
// A surrogate code point is encoded rather than refused, but decodeRune will
// not read one back. The encoder and the decoder disagree there. The C code
// disagrees in the same way.
func appendRune(dst []byte, r rune) []byte {
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
		if b, ok := rawByte(r); ok {
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

// decodeRune reads the first character of s. It returns the character and the
// number of bytes it used.
//
// A byte that does not start a valid sequence returns the raw plane code point
// for that byte, and a count of one. Empty input returns zero and zero, where
// the C code reads past the end of the buffer instead.
func decodeRune(s []byte) (rune, int) {
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
		if c0 <= 0xDF && len(s) >= 2 && isCont(s[1]) {
			return rune(c0&0x1F)<<6 | rune(s[1]&0x3F), 2
		}
		if len(s) >= 3 && isLead3(c0, s[1]) && isCont(s[2]) {
			return rune(c0&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
		}
		if len(s) >= 4 && isLead4(c0, s[1]) && isCont(s[2]) && isCont(s[3]) {
			return rune(c0&0x07)<<18 | rune(s[1]&0x3F)<<12 |
				rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F), 4
		}
	}
	return rawRune(c0), 1
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
		return isCont(c1)
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
		return isCont(c1)
	case c0 == 0xF4:
		return c1 >= 0x80 && c1 <= 0x8F
	}
	return false
}
