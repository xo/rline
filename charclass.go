package rline

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

// charClass reports whether the character in s belongs to a set.
type charClass func(s []byte) bool

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
	return len(s) == 1 && charSetHas(separators, s[0])
}

// charIsNonSeparator reports whether s does not separate words.
func charIsNonSeparator(s []byte) bool {
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

// charIsIDLetter reports whether s can appear in an identifier. That is a
// letter, a digit, an underscore, a hyphen, or any byte from 0x80 up. The word
// search uses this class.
func charIsIDLetter(s []byte) bool {
	if len(s) == 0 {
		return false
	}
	c := s[0]
	return c >= 0x80 || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '_' || c == '-'
}

// fileNameStops is the set of bytes that cannot appear in a file name.
const fileNameStops = " \t\r\n`@$><=;|&{}()[]"

// charIsFileNameLetter reports whether s can appear in a file name.
//
// A zero byte is not a file name letter, for the same strchr rule that makes
// it a separator.
func charIsFileNameLetter(s []byte) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 0x80 || !charSetHas(fileNameStops, s[0])
}

// isToken returns the length of the token that starts at pos, or -1 when pos
// does not start one.
//
// A token starts at pos only when the byte in front of pos is not in the
// class. That one byte is tested on its own, whatever character it belongs
// to, so a position inside a multi byte character can look like a start.
func isToken(s []byte, pos int, class charClass) int {
	if pos < 0 || pos >= len(s) || class == nil {
		return -1
	}
	if pos > 0 && class(s[pos-1:pos]) {
		return -1
	}
	i := pos
	for i < len(s) {
		next, _ := nextOfs(s, i)
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
func matchToken(s []byte, pos int, class charClass, token string) int {
	n := isToken(s, pos, class)
	if n > 0 && n == len(token) && string(s[pos:pos+n]) == token {
		return n
	}
	return 0
}

// matchAnyToken returns the length of the token at pos when it is exactly one
// of tokens, and 0 otherwise.
func matchAnyToken(s []byte, pos int, class charClass, tokens []string) int {
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
	ofs, _ := prevOfs(s, pos)
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
	ofs, _ := nextOfs(s, pos)
	if ofs <= 0 {
		return -1
	}
	return pos + ofs
}

// countEndOverlap returns the length of the longest prefix of postfix that is
// also a suffix of s.
func countEndOverlap(s, postfix string) int {
	for count := len(postfix); count > 0; count-- {
		if count <= len(s) && s[len(s)-count:] == postfix[:count] {
			return count
		}
	}
	return 0
}
