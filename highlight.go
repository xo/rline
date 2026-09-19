package rline

// Syntax highlighting.
//
// A highlighter is handed the line and marks stretches of it with a style. The
// marks land in an attribute buffer, one attribute per byte, which the edit
// loop then draws.
//
// Ported from isocline/src/highlight.c.

// maxBraceNesting is how deep brace matching will go before it gives up.
const maxBraceNesting = 64

// highlightFunc marks up a line. It is given the line and an environment to
// mark it through.
//
//nolint:unused // wired up by editline, step 11
type highlightFunc func(env *highlightEnv, input string)

// highlightEnv is what a highlighter marks a line through.
type highlightEnv struct {
	// What is being marked, and where the marks go.
	input string
	attrs *attrBuf

	// bb resolves a style name to attributes.
	bb *bbCode

	// The last character position that was turned into a byte position.
	// Highlighters walk a line from the front, so remembering one place stops
	// the walk being repeated from the start every time.
	cachedUPos int
	cachedCPos int
}

// runHighlight fills attrs with one attribute per byte of s and then lets the
// highlighter mark it up. A nil highlighter leaves the line unmarked.
//
//nolint:unused // wired up by editline, step 11
func runHighlight(bb *bbCode, s string, attrs *attrBuf, fn highlightFunc) {
	if len(s) == 0 {
		return
	}
	attrs.setAt(0, len(s), attr{})
	if fn == nil {
		return
	}
	env := &highlightEnv{input: s, attrs: attrs, bb: bb}
	fn(env, s)
}

// posAdjust turns a position and a count given in characters into one given in
// bytes. A negative value means characters, which is how a caller that counts
// in characters rather than bytes says so.
//
// Nothing reaches the negative position case, because the one public entry
// point refuses a negative position before it gets here. Only a negative count
// can arrive.
func (h *highlightEnv) posAdjust(pos, count int) (int, int) {
	if pos >= len(h.input) {
		return pos, count
	}
	if pos >= 0 && count >= 0 {
		return pos, count
	}
	if pos < 0 {
		upos := -pos
		cpos, ucount := 0, 0
		if h.cachedUPos <= upos {
			ucount, cpos = h.cachedUPos, h.cachedCPos
		}
		for ucount < upos {
			next, _ := nextOfs([]byte(h.input), cpos)
			if next <= 0 {
				return pos, count
			}
			ucount++
			cpos += next
		}
		pos = cpos
		h.cachedUPos, h.cachedCPos = upos, cpos
	}
	if count < 0 {
		want := -count
		ucount, clen := 0, 0
		for ucount < want {
			next, _ := nextOfs([]byte(h.input), pos+clen)
			if next <= 0 {
				return pos, count
			}
			ucount++
			clen += next
		}
		count = clen
		if h.cachedCPos == pos {
			h.cachedUPos += ucount
			h.cachedCPos += clen
		}
	}
	return pos, count
}

// mark lays a over count bytes from pos.
func (h *highlightEnv) mark(pos, count int, a attr) {
	pos, count = h.posAdjust(pos, count)
	if pos < 0 || count <= 0 {
		return
	}
	h.attrs.updateAt(pos, count, a)
}

// Highlight marks count characters from pos with a named style.
//
// A negative pos is refused, even though posAdjust is written to take one as a
// count of characters rather than bytes. That is what the C does, so the
// character counted form can only ever arrive as the count.
func (h *highlightEnv) Highlight(pos, count int, style string) {
	if style == "" || pos < 0 {
		return
	}
	h.mark(pos, count, h.bb.style(style))
}

// HighlightFormatted marks up s using markup that spells out the same text.
//
// The markup is parsed for its attributes and the text it produces is thrown
// away. When the two disagree in length the marks simply run out, and the rest
// of the line keeps what it had. The C writes a debug line about it, which the
// port drops because nothing reads it.
func (h *highlightEnv) HighlightFormatted(s, format string) {
	if s == "" {
		return
	}
	var out buffer
	var attrs attrBuf
	h.bb.appendTo(format, &out, &attrs)
	for i := range len(s) {
		h.attrs.updateAt(i, 1, attrs.at(i))
	}
}

//-------------------------------------------------------------
// Brace matching
//-------------------------------------------------------------

// openBrace is a brace that has been opened and not yet closed.
type openBrace struct {
	// closer is the brace that would close this one.
	closer byte

	// pos is where the opening brace is.
	pos int

	// atCursor says the cursor sits just after the opening brace.
	atCursor bool
}

// braceOpener returns the closing brace that c opens, and whether c opens one
// at all. The braces are given in pairs, as in "()[]{}".
func braceOpener(braces string, c byte) (byte, bool) {
	for b := 0; b+1 < len(braces); b += 2 {
		if c == braces[b] {
			return braces[b+1], true
		}
	}
	return 0, false
}

// isBraceCloser reports whether c closes a brace.
func isBraceCloser(braces string, c byte) bool {
	for b := 1; b < len(braces); b += 2 {
		if c == braces[b] {
			return true
		}
	}
	return false
}

// highlightMatchBraces marks the brace under the cursor and the one that goes
// with it, and marks a brace that has no partner as an error.
//
// An opening brace left unclosed at the end of the line is not marked, because
// the line is probably still being typed.
func highlightMatchBraces(s string, attrs *attrBuf, cursorPos int, braces string, matchAttr, errorAttr attr) {
	var open [maxBraceNesting + 1]openBrace
	nesting := 0
	for i := range len(s) {
		c := s[i]
		if closer, ok := braceOpener(braces, c); ok {
			if nesting >= maxBraceNesting {
				return // too deep to be worth following
			}
			open[nesting] = openBrace{closer: closer, pos: i, atCursor: i == cursorPos-1}
			nesting++
			continue
		}
		if !isBraceCloser(braces, c) {
			continue
		}
		if nesting <= 0 {
			attrs.updateAt(i, 1, errorAttr)
			continue
		}
		// One wrong opening brace can be stepped over, when the one before it
		// is the partner. That turns "([)" into a single error rather than
		// making everything after it wrong.
		if open[nesting-1].closer != c && nesting > 1 && open[nesting-2].closer == c {
			attrs.updateAt(open[nesting-1].pos, 1, errorAttr)
			nesting--
		}
		if open[nesting-1].closer != c {
			attrs.updateAt(i, 1, errorAttr)
			continue
		}
		nesting--
		if i == cursorPos-1 || (open[nesting].atCursor && open[nesting].pos != i-1) {
			attrs.updateAt(open[nesting].pos, 1, matchAttr)
			attrs.updateAt(i, 1, matchAttr)
		}
	}
}

// findMatchingBrace returns the position just after the brace that goes with
// the one at the cursor, or -1 when there is none, and whether the whole line
// is balanced.
func findMatchingBrace(s string, cursorPos int, braces string) (int, bool) {
	var open [maxBraceNesting + 1]openBrace
	nesting := 0
	match := -1
	balanced := true
	for i := range len(s) {
		c := s[i]
		if closer, ok := braceOpener(braces, c); ok {
			if nesting >= maxBraceNesting {
				return -1, false
			}
			open[nesting] = openBrace{closer: closer, pos: i, atCursor: i == cursorPos-1}
			nesting++
			continue
		}
		if !isBraceCloser(braces, c) {
			continue
		}
		switch {
		case nesting <= 0:
			balanced = false
		case open[nesting-1].closer != c:
			balanced = false
		default:
			nesting--
			if i == cursorPos-1 {
				match = open[nesting].pos + 1
			} else if open[nesting].atCursor {
				match = i + 1
			}
		}
	}
	if nesting != 0 {
		balanced = false
	}
	return match, balanced
}
