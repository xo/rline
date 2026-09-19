package rline

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
func findBackward(s []byte, pos int, class charClass, skipMatches bool) int {
	pos = min(max(pos, 0), len(s))
	i := pos
	if skipMatches {
		for {
			prev, _ := prevOfs(s, i)
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
		prev, _ := prevOfs(s, i)
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
func findForward(s []byte, pos int, class charClass, skipMatches bool) int {
	pos = min(max(pos, 0), len(s))
	i := pos
	if skipMatches {
		for {
			next, _ := nextOfs(s, i)
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
		next, _ := nextOfs(s, i)
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

// findLineStart returns the position just after the newline that precedes
// pos, or 0 when pos is on the first line.
func findLineStart(s []byte, pos int) int {
	return max(findBackward(s, pos, charIsLineFeed, false), 0)
}

// findLineEnd returns the position of the newline at or after pos, or the
// length of s when there is none.
func findLineEnd(s []byte, pos int) int {
	if end := findForward(s, pos, charIsLineFeed, false); end >= 0 {
		return end
	}
	return len(s)
}

// findWordStart returns the position of the start of the word before pos, or
// 0 when there is none. A word is a run of identifier letters.
func findWordStart(s []byte, pos int) int {
	return max(findBackward(s, pos, charIsIDLetter, true), 0)
}

// findWordEnd returns the position of the end of the word after pos, or the
// length of s when there is none.
func findWordEnd(s []byte, pos int) int {
	if end := findForward(s, pos, charIsIDLetter, true); end >= 0 {
		return end
	}
	return len(s)
}

// findWSWordStart returns the position of the start of the run of
// non-whitespace before pos, or 0 when there is none.
func findWSWordStart(s []byte, pos int) int {
	return max(findBackward(s, pos, charIsWhite, true), 0)
}

// findWSWordEnd returns the position of the end of the run of non-whitespace
// after pos, or the length of s when there is none.
func findWSWordEnd(s []byte, pos int) int {
	if end := findForward(s, pos, charIsWhite, true); end >= 0 {
		return end
	}
	return len(s)
}
