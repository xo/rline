package rline

import (
	"fmt"

	"github.com/xo/rline/key"
)

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
	n := limitToLength(s)
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
