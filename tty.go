package rline

import (
	"time"

	"github.com/xo/rline/key"
)

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
// means waiting. macOS waits longer, because it sends an escape and the key
// for alt and a key, so a real sequence can arrive slowly.
const (
	defaultEscInitialTimeout      = 100 * time.Millisecond
	defaultEscInitialTimeoutMacOS = 200 * time.Millisecond
	defaultEscTimeout             = 10 * time.Millisecond
)

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
}

// newTTY returns a tty that reads from src.
func newTTY(src byteReader) *tty {
	return &tty{
		src:               src,
		isUTF8:            true,
		escInitialTimeout: defaultEscInitialTimeout,
		escTimeout:        defaultEscTimeout,
	}
}

// setEscDelay sets how long the decoder waits for the byte after an escape,
// and for each byte after that.
func (t *tty) setEscDelay(initial, follow time.Duration) {
	t.escInitialTimeout = initial
	t.escTimeout = follow
}

// pushByte puts a byte back, to be read before anything from src.
func (t *tty) pushByte(b byte) {
	if len(t.pushedBytes) >= ttyPushMax {
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
	r, used := decodeRune(buf)
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
		code = key.Code(rawRune(c))
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
