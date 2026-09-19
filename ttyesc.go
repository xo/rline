package rline

import (
	"time"

	"github.com/xo/rline/key"
)

// Decoding escape sequences into key codes.
//
// There is no one encoding for a key. A terminal sends an escape, then a
// start byte, then optional numbers, then a final byte, and every family of
// terminal fills that in differently. The grammar the decoder accepts is:
//
//	key ::= ESC                                          # the Escape key
//	     |  ESC char                                     # alt+char
//	     |  ESC '[' special? vtcode  ';' mods '~'        # vt100
//	     |  ESC '[' special? '1'     ';' mods [A-Z]      # xterm
//	     |  ESC 'O' special? '1'     ';' mods [A-Za-z]   # SS3
//	     |  ESC '[' special? unicode ';' mods 'u'        # a code point
//
// A missing number counts as 1. The modifiers are (number - 1) as a bit set,
// where 1 is shift, 2 is alt and 4 is ctrl.
//
// Telling the Escape key from the start of a sequence needs a clock. Nothing
// follows the Escape key, so the decoder waits, and a wait that expires means
// the user pressed Escape. That is why every read here carries a timeout.
//
// Ported from isocline/src/tty_esc.c.

// decodeVT returns the key that a vt100 code stands for.
func decodeVT(code uint32) key.Code {
	switch {
	case code == 1, code == 7:
		return key.Home
	case code == 2:
		return key.Ins
	case code == 3:
		return key.Del
	case code == 4, code == 8:
		return key.End
	case code == 5:
		return key.PageUp
	case code == 6:
		return key.PageDown
	case code >= 10 && code <= 15:
		return key.F(int(1 + code - 10))
	case code == 16:
		return key.F5 // minicom
	case code >= 17 && code <= 21:
		return key.F(int(6 + code - 17))
	case code >= 23 && code <= 26:
		return key.F(int(11 + code - 23))
	case code >= 28 && code <= 29:
		return key.F(int(15 + code - 28))
	case code >= 31 && code <= 34:
		return key.F(int(17 + code - 31))
	}
	return key.None
}

// decodeXterm returns the key that an xterm final byte stands for, after
// ESC [.
func decodeXterm(final byte) key.Code {
	switch final {
	case 'A':
		return key.Up
	case 'B':
		return key.Down
	case 'C':
		return key.Right
	case 'D':
		return key.Left
	case 'E':
		return '5' // the 5 in the middle of the number pad
	case 'F':
		return key.End
	case 'H':
		return key.Home
	case 'Z':
		return key.Tab | key.ModShift
	// FreeBSD.
	case 'I':
		return key.PageUp
	case 'L':
		return key.Ins
	case 'M':
		return key.F1
	case 'N':
		return key.F2
	case 'O':
		return key.F3
	case 'P':
		// This differs from the usual table, which calls it F1.
		return key.F4
	case 'Q':
		return key.F5
	case 'R':
		return key.F6
	case 'S':
		return key.F7
	case 'T':
		return key.F8
	// Mach.
	case 'U':
		return key.PageDown
	case 'V':
		return key.PageUp
	case 'W':
		return key.F11
	case 'X':
		return key.F12
	case 'Y':
		return key.End
	}
	return key.None
}

// decodeSS3 returns the key that an SS3 final byte stands for, after ESC O.
// The lower case finals are the number pad.
func decodeSS3(final byte) key.Code {
	switch final {
	case 'A':
		return key.Up
	case 'B':
		return key.Down
	case 'C':
		return key.Right
	case 'D':
		return key.Left
	case 'E':
		return '5'
	case 'F':
		return key.End
	case 'H':
		return key.Home
	case 'I':
		return key.Tab
	case 'Z':
		return key.Tab | key.ModShift
	case 'M':
		return key.Linefeed
	case 'P':
		return key.F1
	case 'Q':
		return key.F2
	case 'R':
		return key.F3
	case 'S':
		return key.F4
	// Mach.
	case 'T':
		return key.F5
	case 'U':
		return key.F6
	case 'V':
		return key.F7
	case 'W':
		return key.F8
	case 'X':
		return key.F9 // '=' on a vt220
	case 'Y':
		return key.F10
	// The number pad.
	case 'a':
		return key.Up
	case 'b':
		return key.Down
	case 'c':
		return key.Right
	case 'd':
		return key.Left
	case 'j':
		return '*'
	case 'k':
		return '+'
	case 'l':
		return ','
	case 'm':
		return '-'
	case 'n':
		return key.Del
	case 'o':
		return '/'
	case 'p':
		return key.Ins
	case 'q':
		return key.End
	case 'r':
		return key.Down
	case 's':
		return key.PageDown
	case 't':
		return key.Left
	case 'u':
		return '5'
	case 'v':
		return key.Right
	case 'w':
		return key.Home
	case 'x':
		return key.Up
	case 'y':
		return key.PageUp
	}
	return key.None
}

// readCSINum reads a decimal number. peek is the byte already in hand, and
// the result is the byte the number stopped on and the number itself. A
// number that is not there at all reads as 1.
//
// A digit only counts once the byte after it has arrived. So a sequence that
// ends on a digit loses that digit, and "ESC [ 1" reads its number as 1
// rather than as 1 from the digit.
func (t *tty) readCSINum(peek byte, timeout time.Duration) (byte, uint32) {
	num := uint32(1)
	count, value := 0, uint32(0)
	for peek >= '0' && peek <= '9' && count < 16 {
		digit := uint32(peek - '0')
		next, ok := t.readByte(timeout)
		if !ok {
			break
		}
		peek = next
		count++
		value = 10*value + digit
	}
	if count > 0 {
		num = value
	}
	return peek, num
}

// readCSI reads the rest of a sequence after its start byte. c1 is '[' for a
// CSI sequence and 'O' for an SS3 one, peek is the byte after that, and mods
// holds the modifiers found so far.
func (t *tty) readCSI(c1, peek byte, mods key.Code, timeout time.Duration) key.Code {
	// Linux sometimes sends a second start byte, as in ESC [ [ 15 ~ for F5.
	if c1 == '[' && charSetHas("[Oo", peek) {
		start := peek
		if next, ok := t.readByte(timeout); ok {
			peek = next
			c1 = start
		}
	}
	// A private sequence starts with one of these. The byte is read and kept
	// only so that it can be put back, because nothing else uses it.
	if charSetHas(":<=>?", peek) {
		special := peek
		next, ok := t.readByte(timeout)
		if !ok {
			t.pushByte(special)
			return key.Code(c1) | key.ModAlt
		}
		peek = next
	}
	// Up to two numbers, both of which default to 1. readCSINum supplies the
	// default for the first one, and the second stays 1 unless a ';' brings
	// it.
	num2 := uint32(1)
	peek, num1 := t.readCSINum(peek, timeout)
	if peek == ';' {
		next, ok := t.readByte(timeout)
		if !ok {
			return key.None
		}
		peek, num2 = t.readCSINum(next, timeout)
	}
	final := peek
	modifiers := mods

	// Fold the shapes that only one family of terminal sends into the shape
	// that the tables below expect.
	switch {
	case (final == '@' || final == '9') && c1 == '[' && num1 == 1:
		// Delete and Insert on Mach.
		if final == '@' {
			num1 = 3
		} else {
			num1 = 2
		}
		final = '~'
	case final == '^' || final == '$' || final == '@':
		// Eterm, rxvt and urxvt put the modifier in the final byte.
		switch final {
		case '^':
			modifiers |= key.ModCtrl
		case '$':
			modifiers |= key.ModShift
		case '@':
			modifiers |= key.ModShift | key.ModCtrl
		}
		final = '~'
	case c1 == '[' && final >= 'a' && final <= 'd':
		// Eterm sends lower case for shift and an arrow. This does not catch
		// ESC [ .. u, which is a code point.
		modifiers |= key.ModShift
		final = 'A' + (final - 'a')
	}

	// Haiku puts the modifier in the first number rather than the second.
	if (c1 == 'O' || (c1 == '[' && final != '~' && final != 'u')) &&
		num2 == 1 && num1 > 1 && num1 <= 8 {
		num2, num1 = num1, 1
	}

	// The second number carries the modifiers, as a bit set of one less.
	if num2 > 1 && num2 <= 9 {
		if num2 == 9 {
			num2 = 3 // iTerm2 in xterm mode
		}
		num2--
		if num2&0x1 != 0 {
			modifiers |= key.ModShift
		}
		if num2&0x2 != 0 {
			modifiers |= key.ModAlt
		}
		if num2&0x4 != 0 {
			modifiers |= key.ModCtrl
		}
	}

	code := key.None
	switch {
	case final == '~':
		code = decodeVT(num1)
	case c1 == '[' && final == 'u':
		code = key.Code(num1)
	case c1 == 'O' && ((final >= 'A' && final <= 'Z') || (final >= 'a' && final <= 'z')):
		code = decodeSS3(final)
	case num1 == 1 && final >= 'A' && final <= 'Z':
		code = decodeXterm(final)
	case c1 == '[' && final == 'R':
		// A report of where the cursor is, which term.c asks for and reads
		// back itself. It is not a key.
		code = key.None
	}
	if code == key.None {
		return key.None
	}
	return code | modifiers
}

// readOSC reads an operating system command to its end and throws it away.
//
// These arrive when a query that term.c sent is answered later than expected,
// so the answer turns up in the middle of typing. It ends at a bell, at
// ESC backslash, or at any byte below the bell, which is put back.
func (t *tty) readOSC(peek byte, timeout time.Duration) key.Code {
	for {
		c := peek
		if c <= '\x07' {
			if c != '\x07' {
				t.pushByte(c)
			}
			break
		}
		if c == '\x1B' {
			c1, ok := t.readByte(timeout)
			if !ok {
				break
			}
			if c1 == '\\' {
				break
			}
			t.pushByte(c1)
		}
		next, ok := t.readByte(timeout)
		if !ok {
			break
		}
		peek = next
	}
	return key.None
}

// readEsc reads a key that starts with an escape. initial is how long to wait
// for the byte after the escape, and follow how long to wait for each byte
// after that.
//
// Nothing arriving within initial means the user pressed the Escape key. A
// byte that starts no sequence the decoder knows becomes alt and that byte.
func (t *tty) readEsc(initial, follow time.Duration) key.Code {
	var mods key.Code
	peek, ok := t.readByte(initial)
	if !ok {
		return key.Esc
	}
	// macOS sends ESC ESC [ A for alt and an arrow, so a second escape is an
	// alt modifier.
	if key.Code(peek) == key.Esc {
		next, ok := t.readByte(follow)
		if !ok {
			return key.Code(peek) | key.ModAlt
		}
		peek = next
		mods |= key.ModAlt
	}
	switch peek {
	case '[':
		next, ok := t.readByte(follow)
		if !ok {
			return key.Code(peek) | key.ModAlt
		}
		return t.readCSI('[', next, mods, follow)
	case 'O', 'o', '?':
		start := peek
		next, ok := t.readByte(follow)
		if !ok {
			return key.Code(peek) | key.ModAlt
		}
		if start == 'o' {
			// Eterm sends this for ctrl and an arrow.
			mods |= key.ModCtrl
		}
		// 'o' and '?' are both treated as a plain SS3 start.
		return t.readCSI('O', next, mods, follow)
	case ']':
		next, ok := t.readByte(follow)
		if !ok {
			return key.Code(peek) | key.ModAlt
		}
		return t.readOSC(next, follow)
	}
	return key.Code(peek) | key.ModAlt
}
