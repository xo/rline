package rline

// Completing a word rather than the whole line.
//
// A completer usually wants the last word of the line, not everything the
// user has typed. These narrow the prefix down to that word, hand it over,
// and then put back what they took off, by telling the completion how much of
// the line it has to take away when it goes in.
//
// The quoted form also understands quotes and escapes, so that completing a
// file name with a space in it works: the word given to the completer has its
// quoting taken off, and what comes back has it put on again.
//
// Ported from isocline/src/completers.c.

// defaultQuoteChars are the quotes the quoted form looks for when the caller
// names none.
const defaultQuoteChars = "'\""

// defaultEscapeChar is the escape the quoted form uses when the caller names
// none.
const defaultEscapeChar = '\\'

// completeWord narrows prefix to the word at its end and calls fun with just
// that word.
//
// isWordChar says what a word is made of. Passing nil means anything that is
// not a separator.
func completeWord(cenv *Completion, prefix string, fun Completer, isWordChar charClass) {
	if isWordChar == nil {
		isWordChar = charIsNonSeparator
	}
	p := []byte(prefix)
	pos := len(p)
	for pos > 0 {
		ofs, _ := prevOfs(p, pos)
		if ofs <= 0 {
			break
		}
		if !isWordChar(p[pos-ofs : pos]) {
			break
		}
		pos -= ofs
	}
	withWordPrefix(cenv, len(prefix)-pos, func(replacement string) string {
		return replacement
	}, fun, prefix[pos:])
}

// withWordPrefix calls fun with word, with cenv set up so that every
// completion it offers is passed through fix and then told how much of the
// line to take away.
//
// deleteBeforeAdjust is how much of the line the word itself covers, which
// the completer knows nothing about. The amount to take away after the cursor
// is whatever end of the replacement is already typed there, so that
// completing in the middle of a word does not leave its tail behind.
func withWordPrefix(cenv *Completion, deleteBeforeAdjust int,
	fix func(string) string, fun Completer, word string,
) {
	postfix := ""
	if cenv.cursor >= 0 && cenv.cursor <= len(cenv.input) {
		postfix = cenv.input[cenv.cursor:]
	}
	prev := cenv.add
	cenv.add = func(replacement, display, help string, deleteBefore, deleteAfter int) bool {
		replacement = fix(replacement)
		return prev(replacement, display, help,
			deleteBeforeAdjust+deleteBefore,
			countEndOverlap(replacement, postfix)+deleteAfter)
	}
	fun(cenv, word)
	cenv.add = prev
}

// completeQWord is completeWord for a word that may be quoted, with the usual
// backslash escape and single or double quotes.
func completeQWord(cenv *Completion, prefix string, fun Completer, isWordChar charClass) {
	completeQWordEx(cenv, prefix, fun, isWordChar, defaultEscapeChar, defaultQuoteChars)
}

// completeQWordEx is completeQWord with the escape character and the quotes
// named by the caller. An empty quoteChars means the default pair.
func completeQWordEx(cenv *Completion, prefix string, fun Completer,
	isWordChar charClass, escape byte, quoteChars string,
) {
	if isWordChar == nil {
		isWordChar = charIsNonSeparator
	}
	if quoteChars == "" {
		quoteChars = defaultQuoteChars
	}
	p := []byte(prefix)
	quote, pos, quoteLen := findQuotedWord(p, isWordChar, escape, quoteChars)

	if quote == 0 {
		// No quote, so the word is the run of word characters at the end,
		// stepping over anything that is escaped.
		pos = len(p)
		for pos > 0 {
			ofs, _ := prevOfs(p, pos)
			if ofs <= 0 {
				break
			}
			if !isWordChar(p[pos-ofs : pos]) {
				// A separator ends the word unless it is escaped.
				if pos <= ofs || p[pos-ofs-1] != escape {
					break
				}
				pos-- // step over the escape as well
			}
			pos -= ofs
		}
	}

	word := p[pos:]
	if quote != 0 {
		word = word[:min(quoteLen, len(word))]
	}
	if quote == 0 {
		word = unescapeWord(word, isWordChar, escape)
	}

	deleteBeforeAdjust := len(p) - pos
	withWordPrefix(cenv, deleteBeforeAdjust, func(replacement string) string {
		return requoteReplacement(replacement, quote, isWordChar, escape)
	}, fun, string(word))
}

// findQuotedWord looks for a quoted word at the end of p. It returns the
// quote character, where the word starts just after it, and how long the word
// is. A quote of zero means there is no quoted word.
//
// It counts the quotes from the front rather than looking backwards, because
// only an odd count, or a closing quote with nothing but word characters
// after it, means the cursor is inside a quoted word.
func findQuotedWord(p []byte, isWordChar charClass, escape byte, quoteChars string) (byte, int, int) {
	var quote byte
	openAt, closeAt, count := -1, -1, 0
	for pos := 0; pos < len(p); {
		switch {
		case p[pos] == escape && byteAt(p, pos+1) != 0 && !isWordChar(p[pos+1:pos+2]):
			pos++ // step over the escape and whatever it escapes
		case count%2 == 0 && charSetHas(quoteChars, p[pos]):
			openAt, quote = pos, p[pos]
			count++
		case count%2 == 1 && p[pos] == quote:
			closeAt = pos
			count++
		case !isWordChar(p[pos : pos+1]):
			// A separator outside a quote means any quote that closed before
			// it belongs to an earlier word.
			closeAt = -1
		}
		ofs, _ := nextOfs(p, pos)
		if ofs <= 0 {
			break
		}
		pos += ofs
	}
	// An odd count means a quote is still open. An even count with a closing
	// quote that only word characters follow means the cursor is still in
	// that word.
	if (count%2 == 0 && closeAt >= 0) || count%2 == 1 {
		return quote, openAt + 1, len(p) - openAt - 1
	}
	return 0, 0, 0
}

// unescapeWord takes the escapes off a word, so that the completer sees the
// name the user meant rather than the way it was typed.
//
// The C code shifts the rest of the string down over each escape it removes
// while keeping its idea of the length, so the walk carries on over what is
// left. This keeps a buffer of that fixed length with a terminating zero and
// does the same, then takes the string up to that zero.
func unescapeWord(word []byte, isWordChar charClass, escape byte) []byte {
	wlen := len(word)
	buf := make([]byte, wlen+1)
	copy(buf, word)
	wpos := 0
	for wpos < wlen {
		ofs, _ := nextOfs(buf[:wlen], wpos)
		if ofs <= 0 {
			break
		}
		if buf[wpos] == escape && byteAt(buf, wpos+1) != 0 &&
			!isWordChar(buf[wpos+1:min(wpos+1+ofs, len(buf))]) {
			copy(buf[wpos:], buf[wpos+1:])
		}
		wpos += ofs
	}
	if end := indexZero(buf); end >= 0 {
		return buf[:end]
	}
	return buf[:wlen]
}

// indexZero returns where the first zero byte is, or -1.
func indexZero(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

// requoteReplacement puts the quoting back on a completion, so that what goes
// into the line reads the way the user typed the rest of it.
//
// Inside a quote it only needs the closing quote. Outside one, every
// character that is not part of a word gets the escape in front of it.
func requoteReplacement(replacement string, quote byte, isWordChar charClass, escape byte) string {
	var buf buffer
	buf.replace(replacement)
	if quote != 0 {
		buf.appendByte(quote)
		return buf.string()
	}
	pos := 0
	for {
		next, _ := buf.nextOfs(pos)
		if next <= 0 {
			break
		}
		if !isWordChar(buf.bytes()[pos : pos+next]) {
			buf.insertByteAt(escape, pos)
			pos++
		}
		pos += next
	}
	return buf.string()
}
