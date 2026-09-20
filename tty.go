// Reading keys: the byte stream from the terminal, and the escape sequences
// in it decoded into key codes.
//
// Ported from isocline/src/tty.c and tty_esc.c.

package rline

import (
	"fmt"
	"runtime"
	"time"

	"github.com/xo/rline/internal/text"
	"github.com/xo/rline/key"
)

// --------------------------------------------------------------------------
// tty.go

// --------------------------------------------------------------------------
// tty.go

// Reading keys from a terminal.
//
// A tty turns a stream of bytes into key codes. It sits on a byteReader,
// which is the terminal on a real run and a fixed slice of bytes in a test.
//
// Two pushback buffers sit in front of that reader. The byte buffer holds
// input the escape decoder looked at and did not use, and the code buffer
// holds whole keys that the edit loop wants to read again.
//
// Ported from isocline/src/tty.c.

// ttyPushMax is how much either pushback buffer holds. Anything past that is
// dropped.
//
// The C code guards the byte buffer with the length of the code buffer rather
// than its own, so the byte buffer can overrun. Nothing reaches that, because
// the decoder never pushes back more than three bytes at once, but the port
// checks the buffer it is about to write to.
const ttyPushMax = 32

// Default waits for the escape decoder.
//
// Nothing follows the Escape key, so telling it from the start of a sequence
// means waiting.
const defaultEscTimeout = 10 * time.Millisecond

// defaultEscInitial is how long this system waits for the byte after an
// escape before deciding the user pressed the Escape key.
func defaultEscInitial() time.Duration {
	return escInitialFor(runtime.GOOS)
}

// escInitialFor is the wait for a named system.
//
// macOS waits twice as long, because it sends an escape and then the key when
// alt is held with a key, so a real sequence can arrive slowly. Nobody has
// measured whether a BSD does the same; the figure everywhere else is the
// Linux one.
//
// This is a heuristic about terminal emulators rather than a system call, so
// the system is a parameter rather than a build tag. runtime.GOOS is a
// constant, so defaultEscInitial still costs nothing at run time, and taking
// the name as an argument is what lets any machine check any figure: a tagged
// file, or a branch on runtime.GOOS here, would leave each figure checkable
// only on the system that uses it. See TestEscInitialFigures.
func escInitialFor(goos string) time.Duration {
	if goos == "darwin" {
		return 200 * time.Millisecond
	}
	return 100 * time.Millisecond
}

// byteReader supplies the bytes that a tty decodes.
type byteReader interface {
	// readByte returns the next byte of input. It reports false when none
	// arrived before timeout, and then it has consumed nothing. A negative
	// timeout waits for as long as it takes.
	readByte(timeout time.Duration) (byte, bool)
}

// tty reads key codes from a terminal.
type tty struct {
	// src is where the bytes come from.
	src byteReader

	// isUTF8 says whether the input is UTF-8. When it is not, a byte above
	// 0x7f becomes a raw code point instead of being decoded.
	isUTF8 bool

	// escInitialTimeout is how long to wait for the byte after an escape,
	// and escTimeout how long for each byte after that.
	escInitialTimeout time.Duration
	escTimeout        time.Duration

	// pushedBytes holds input read but not used, and pushedCodes whole keys
	// to be read again. Both are taken from the end.
	pushedBytes []byte
	pushedCodes []key.Code

	// dev is the terminal, when src is one. It is nil when the bytes come
	// from somewhere else, such as a test.
	dev terminalDevice
}

// terminalDevice is a byteReader that is a terminal, so its settings can be
// changed and its signals watched.
type terminalDevice interface {
	byteReader

	// startRaw makes keys arrive one at a time, and endRaw puts the terminal
	// back the way it was.
	startRaw() error
	endRaw()

	// resizeEvent reports whether the window changed size since the last
	// call.
	resizeEvent() bool

	// asyncStop makes a waiting read return.
	asyncStop() bool

	// close puts the terminal back and stops watching its signals.
	close() error
}

// newTTY returns a tty that reads from src.
func newTTY(src byteReader) *tty {
	return &tty{
		src:               src,
		isUTF8:            true,
		escInitialTimeout: defaultEscInitial(),
		escTimeout:        defaultEscTimeout,
	}
}

// pushByte puts a byte back, to be read before anything from src.
//
// A zero byte is dropped rather than pushed. The C code builds a string of
// one character and hands it to a function that measures it with strlen, so a
// zero byte measures as nothing and no byte is pushed at all. That matters:
// the UTF-8 assembly pushes back whatever it did not use, so a zero byte in
// the middle of an invalid sequence is lost rather than read as a key. The
// differential fuzzer found this on the input f5 00 30.
func (t *tty) pushByte(b byte) {
	if b == 0 || len(t.pushedBytes) >= ttyPushMax {
		return
	}
	t.pushedBytes = append(t.pushedBytes, b)
}

// readByte returns the next byte, from the pushback buffer first and from src
// after that. It reports false when nothing arrived before timeout.
func (t *tty) readByte(timeout time.Duration) (byte, bool) {
	if n := len(t.pushedBytes); n > 0 {
		b := t.pushedBytes[n-1]
		t.pushedBytes = t.pushedBytes[:n-1]
		return b, true
	}
	if t.src == nil {
		return 0, false
	}
	return t.src.readByte(timeout)
}

// pushCode puts a whole key back, to be returned by the next read.
func (t *tty) pushCode(c key.Code) {
	if len(t.pushedCodes) >= ttyPushMax {
		return
	}
	t.pushedCodes = append(t.pushedCodes, c)
}

// popCode takes a pushed back key, if there is one.
func (t *tty) popCode() (key.Code, bool) {
	if n := len(t.pushedCodes); n > 0 {
		c := t.pushedCodes[n-1]
		t.pushedCodes = t.pushedCodes[:n-1]
		return c, true
	}
	return key.None, false
}

// readUTF8 reads the rest of a character that starts with c0 and returns its
// code point.
//
// It reads as many bytes as the first one allows, then decodes what it got.
// Bytes the decoder did not use are put back, so that an invalid sequence
// does not swallow the character after it.
func (t *tty) readUTF8(c0 byte) key.Code {
	buf := make([]byte, 0, 4)
	buf = append(buf, c0)
	if c0 > 0x7F {
		if b, ok := t.readByte(t.escTimeout); ok {
			buf = append(buf, b)
			if c0 > 0xDF {
				if b, ok := t.readByte(t.escTimeout); ok {
					buf = append(buf, b)
					if c0 > 0xEF {
						if b, ok := t.readByte(t.escTimeout); ok {
							buf = append(buf, b)
						}
					}
				}
			}
		}
	}
	r, used := text.DecodeRune(buf)
	for i := len(buf); i > used; {
		i--
		t.pushByte(buf[i])
	}
	return key.Code(r)
}

// readTimeout reads one key. It reports false when nothing arrived before
// timeout. A negative timeout waits for as long as it takes.
func (t *tty) readTimeout(timeout time.Duration) (key.Code, bool) {
	// A key that was pushed back is returned as it was. It has been through
	// modifyCode already.
	if code, ok := t.popCode(); ok {
		return code, true
	}
	c, ok := t.readByte(timeout)
	if !ok {
		return key.None, false
	}
	var code key.Code
	switch {
	case key.Code(c) == key.Esc:
		code = t.readEsc(t.escInitialTimeout, t.escTimeout)
	case c <= 0x7F:
		code = key.Code(c)
	case t.isUTF8:
		code = t.readUTF8(c)
	default:
		// The input is not UTF-8, so keep the byte as a raw code point and
		// let the encoder turn it back into that byte at the end.
		code = key.Code(text.RawRune(c))
	}
	return modifyCode(code), true
}

// read waits for one key and returns it. It returns key.None when the input
// ends.
func (t *tty) read() key.Code {
	code, ok := t.readTimeout(-1)
	if !ok {
		return key.None
	}
	return code
}

// modifyCode rewrites the keys that terminals disagree about, so that the
// edit loop sees one code for each of them whatever sent it.
func modifyCode(code key.Code) key.Code {
	k, mods := code.NoMods(), code.Mods()
	switch {
	case k == key.Rubout:
		// Delete always arrives as backspace.
		code = key.Backspace | mods
	case k == 0x1F && mods&key.ModAlt == 0:
		// Linux sends 0x1F for ctrl+'_'. Put it back together.
		//
		// The assignment to k is not dead. It stops the rule at the end of
		// this function from taking the ctrl bit off again, because 0x1F is
		// below space and '_' is not.
		k = '_'
		code = '_' | key.ModCtrl
	case k == key.Enter && (mods == key.ModShift || mods == key.ModAlt || mods == key.ModCtrl):
		// Any one modifier with enter means a new line rather than a return.
		code = key.Linefeed
	case code == key.Tab|key.ModCtrl:
		code = key.ShiftTab
	case code == key.Down|key.ModAlt, code == '>'|key.ModAlt, code == key.End|key.ModCtrl:
		code = key.PageDown
	case code == key.Up|key.ModAlt, code == '<'|key.ModAlt, code == key.Home|key.ModCtrl:
		code = key.PageUp
	}
	// A control character already carries the control key, so do not say so
	// twice.
	if k < key.Space && mods&key.ModCtrl != 0 {
		code &^= key.ModCtrl
	}
	return code
}

// startRaw makes keys arrive one at a time. It does nothing when the bytes do
// not come from a terminal.
func (t *tty) startRaw() error {
	if t.dev == nil {
		return nil
	}
	return t.dev.startRaw()
}

// endRaw puts the terminal back the way it was.
func (t *tty) endRaw() {
	if t.dev != nil {
		t.dev.endRaw()
	}
}

// close puts the terminal back and stops watching its signals.
func (t *tty) close() error {
	if t.dev == nil {
		return nil
	}
	return t.dev.close()
}

// resizeEvent reports whether the window changed size since the last call. It
// answers yes when there is no terminal to ask, because then there is no way
// to tell and redrawing costs less than being wrong.
func (t *tty) resizeEvent() bool {
	if t.dev == nil {
		return true
	}
	return t.dev.resizeEvent()
}

// asyncStop makes a read that is waiting for a key return.
func (t *tty) asyncStop() bool {
	if t.dev == nil {
		return false
	}
	return t.dev.asyncStop()
}

// escDelayMax is the longest wait that setEscDelay accepts. The C code clamps
// to the same value.
const escDelayMax = time.Second

// setEscDelay sets how long the decoder waits for the byte after an escape,
// and for each byte after that. Each is held between zero and one second.
func (t *tty) setEscDelay(initial, follow time.Duration) {
	t.escInitialTimeout = clampDelay(initial)
	t.escTimeout = clampDelay(follow)
}

// clampDelay holds d between zero and escDelayMax.
func clampDelay(d time.Duration) time.Duration {
	switch {
	case d < 0:
		return 0
	case d > escDelayMax:
		return escDelayMax
	}
	return d
}

// readEscResponse reads back the answer to a query that was written to the
// terminal, such as where the cursor is or what a palette colour is.
//
// escStart is the byte that follows the escape, and finalST says the answer
// ends at a bell or at ESC backslash rather than at the first byte that is
// not part of a number. max is the most bytes to keep.
//
// The first wait is twice the initial escape wait, because the terminal has
// to be given time to answer at all.
//
// Nothing useful comes back when this fails. The C code fills its buffer as
// it goes and only writes the terminating zero once it succeeds, so a failure
// leaves bytes there that are not a string and that no caller may read. This
// returns an empty string instead.
func (t *tty) readEscResponse(escStart byte, finalST bool, max int) (string, bool) {
	c, ok := t.readByte(2 * t.escInitialTimeout)
	if !ok || c != '\x1B' {
		return "", false
	}
	if c, ok = t.readByte(t.escTimeout); !ok || c != escStart {
		return "", false
	}
	buf := make([]byte, 0, max)
	for len(buf) < max {
		c, ok := t.readByte(t.escTimeout)
		if !ok {
			return "", false
		}
		if finalST {
			// An operating system command ends at a bell, at ESC backslash,
			// or at the start of text byte.
			if c == '\x07' || c == '\x02' {
				break
			}
			if c == '\x1B' {
				c1, ok := t.readByte(t.escTimeout)
				if !ok {
					return "", false
				}
				if c1 == '\\' {
					break
				}
				t.pushByte(c1)
			}
		} else {
			if c == '\x02' {
				break
			}
			partOfNumber := (c >= '0' && c <= '9') || text.CharSetHas("<=>?;:", c)
			if !partOfNumber {
				// Keep the byte that ended it, which names what the answer
				// was about.
				buf = append(buf, c)
				break
			}
		}
		buf = append(buf, c)
	}
	return string(buf), true
}

// --------------------------------------------------------------------------
// ttyesc.go

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
	if c1 == '[' && text.CharSetHas("[Oo", peek) {
		start := peek
		if next, ok := t.readByte(timeout); ok {
			peek = next
			c1 = start
		}
	}
	// A private sequence starts with one of these. The byte is read and kept
	// only so that it can be put back, because nothing else uses it.
	if text.CharSetHas(":<=>?", peek) {
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

// --------------------------------------------------------------------------
// winkey.go

// Turning a key press into the escape sequence a terminal would have sent.
//
// Windows does not hand a program a stream of bytes. It hands it key events,
// each naming a virtual key, a character and which modifiers were held. So
// the Windows side turns every event back into the sequence a Unix terminal
// would have sent, pushes that into the same buffer the decoder reads from,
// and lets the decoder do the rest. Everything below this point is then
// shared between the systems.
//
// These sit above the part of tty.c that is compiled per system, so they are
// not behind a build tag here either, and the recorded cases for them come
// from a probe built on any system rather than only on Windows.
//
// Ported from isocline/src/tty.c.

// csiMods returns the number a CSI sequence uses to name which modifier keys
// were held. It counts from one, and each modifier adds a bit.
func csiMods(mods key.Code) uint32 {
	m := uint32(1)
	if mods&key.ModShift != 0 {
		m++
	}
	if mods&key.ModAlt != 0 {
		m += 2
	}
	if mods&key.ModCtrl != 0 {
		m += 4
	}
	return m
}

// pushBytes puts a whole sequence back, so that its first byte is read first.
//
// The buffer is a stack, so the bytes go in backwards. A zero byte ends the
// sequence and everything from it is dropped, because the C code measures
// what it is given with strlen.
func (t *tty) pushBytes(s string) {
	n := text.LimitToLength(s)
	if n <= 0 || len(t.pushedBytes)+n > ttyPushMax {
		return
	}
	for i := n - 1; i >= 0; i-- {
		t.pushedBytes = append(t.pushedBytes, s[i])
	}
}

// csiVTSequence returns the sequence for a key that a terminal names by
// number, such as Delete or Page Up.
func csiVTSequence(mods key.Code, vtcode uint32) string {
	return fmt.Sprintf("\x1B[%d;%d~", vtcode, csiMods(mods))
}

// csiXtermSequence returns the sequence for a key that a terminal names by a
// letter, such as an arrow or Home.
func csiXtermSequence(mods key.Code, xcode byte) string {
	return fmt.Sprintf("\x1B[1;%d%c", csiMods(mods), xcode)
}

// csiUnicodeSequence returns what to send for a character.
//
// A character that a terminal would have sent as itself is sent as itself,
// which keeps the common case cheap and keeps the bytes a program sees the
// same as on any other system. That covers plain ASCII with no modifier, a
// control character with ctrl held, and a printable character with shift
// held. Everything else needs the sequence that names the character and the
// modifiers.
func csiUnicodeSequence(mods key.Code, code uint32) string {
	plain := code < 0x80 && mods == 0
	control := mods == key.ModCtrl && code < uint32(key.Space) &&
		code != uint32(key.Tab) && code != uint32(key.Enter) &&
		code != uint32(key.Linefeed) && code != uint32(key.Backspace)
	shifted := mods == key.ModShift && code >= uint32(key.Space) && code <= uint32(key.Rubout)
	if plain || control || shifted {
		return string([]byte{byte(code)})
	}
	return fmt.Sprintf("\x1B[%d;%du", code, csiMods(mods))
}

// pushCSIVT pushes the sequence for a key named by number.
func (t *tty) pushCSIVT(mods key.Code, vtcode uint32) {
	t.pushBytes(csiVTSequence(mods, vtcode))
}

// pushCSIXterm pushes the sequence for a key named by a letter.
func (t *tty) pushCSIXterm(mods key.Code, xcode byte) {
	t.pushBytes(csiXtermSequence(mods, xcode))
}

// pushCSIUnicode pushes a character.
func (t *tty) pushCSIUnicode(mods key.Code, code uint32) {
	t.pushBytes(csiUnicodeSequence(mods, code))
}
