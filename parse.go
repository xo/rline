package rline

import "strconv"

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

// atoz reads one decimal number from s.
//
// A number too large for an int is not defined by C and not defined here
// either. This returns false for it, where sscanf would store something.
func atoz(s string) (int, bool) {
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
