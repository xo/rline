// Matching one bracket against its partner.
//
// This is pure text: it takes a line, a cursor and the pairs to consider, and
// answers where the partner is. Both the editor, which moves the cursor to it,
// and the highlighter, which marks it, ask for the same answer, so it lives
// below them rather than inside either.
//
// Ported from isocline/src/highlight.c.

package text

// MaxBraceNesting is how deep matching will go before it gives up.
const MaxBraceNesting = 64

// OpenBrace is a brace that has been opened and not yet closed.
type OpenBrace struct {
	// Closer is the brace that would close this one.
	Closer byte

	// Pos is where the opening brace is.
	Pos int

	// AtCursor says the cursor sits just after the opening brace.
	AtCursor bool
}

// BraceOpener returns the closing brace that c opens, and whether c opens one
// at all. The braces are given in pairs, as in "()[]{}".
func BraceOpener(braces string, c byte) (byte, bool) {
	for b := 0; b+1 < len(braces); b += 2 {
		if c == braces[b] {
			return braces[b+1], true
		}
	}
	return 0, false
}

// IsBraceCloser reports whether c closes a brace.
func IsBraceCloser(braces string, c byte) bool {
	for b := 1; b < len(braces); b += 2 {
		if c == braces[b] {
			return true
		}
	}
	return false
}

// FindMatchingBrace returns the position just after the brace that goes with
// the one at the cursor, or -1 when there is none, and whether the whole line
// is balanced.
func FindMatchingBrace(s string, cursorPos int, braces string) (int, bool) {
	var open [MaxBraceNesting + 1]OpenBrace
	nesting := 0
	match := -1
	balanced := true
	for i := range len(s) {
		c := s[i]
		if Closer, ok := BraceOpener(braces, c); ok {
			if nesting >= MaxBraceNesting {
				return -1, false
			}
			open[nesting] = OpenBrace{Closer: Closer, Pos: i, AtCursor: i == cursorPos-1}
			nesting++
			continue
		}
		if !IsBraceCloser(braces, c) {
			continue
		}
		switch {
		case nesting <= 0:
			balanced = false
		case open[nesting-1].Closer != c:
			balanced = false
		default:
			nesting--
			if i == cursorPos-1 {
				match = open[nesting].Pos + 1
			} else if open[nesting].AtCursor {
				match = i + 1
			}
		}
	}
	if nesting != 0 {
		balanced = false
	}
	return match, balanced
}
