// Package key holds the key codes that rline reads from a terminal.
//
// A [Code] is one key press: a code point, a virtual key such as an arrow, or
// an event such as a terminal resize, with the modifier keys held in the top
// four bits. A program that binds keys or reads them from rline names them
// from here.
//
// Ported from isocline/src/tty.h and isocline/src/tty.c. isocline is a
// readline replacement written in C by Daan Leijen, under the MIT license:
//
//	Copyright (c) 2021, Daan Leijen
package key

// Code is a key press with its modifiers.
//
// The bottom 21 bits hold a code point, up to U+10FFFF. Codes from [Virt] are
// virtual keys, and codes from [EventBase] are events. The top four bits are
// the modifiers.
type Code uint32

// Modifier bits, and the masks that separate them from the key.
const (
	ModShift Code = 0x10000000
	ModAlt   Code = 0x20000000
	ModCtrl  Code = 0x40000000

	// ModMask covers every modifier bit, and Mask everything below them.
	ModMask Code = 0xF0000000
	Mask    Code = 0x0FFFFFFF
)

// The control characters, and the keys that share their codes.
const (
	None      Code = 0
	CtrlA     Code = 1
	CtrlB     Code = 2
	CtrlC     Code = 3
	CtrlD     Code = 4
	CtrlE     Code = 5
	CtrlF     Code = 6
	Bell      Code = 7
	Backspace Code = 8
	Tab       Code = 9
	// Linefeed is what ctrl+enter and shift+enter both become.
	Linefeed Code = 10
	CtrlK    Code = 11
	CtrlL    Code = 12
	Enter    Code = 13
	CtrlN    Code = 14
	CtrlO    Code = 15
	CtrlP    Code = 16
	CtrlQ    Code = 17
	CtrlR    Code = 18
	CtrlS    Code = 19
	CtrlT    Code = 20
	CtrlU    Code = 21
	CtrlV    Code = 22
	CtrlW    Code = 23
	CtrlX    Code = 24
	CtrlY    Code = 25
	CtrlZ    Code = 26
	Esc      Code = 27
	Space    Code = 32
	// Rubout is what the Delete key sends. It always becomes [Backspace].
	Rubout Code = 127

	// UnicodeMax is the largest code point a Code can hold.
	UnicodeMax Code = 0x0010FFFF
)

// The virtual keys, which have no code point of their own.
const (
	Virt     Code = 0x01000000
	Up       Code = Virt + 0
	Down     Code = Virt + 1
	Left     Code = Virt + 2
	Right    Code = Virt + 3
	Home     Code = Virt + 4
	End      Code = Virt + 5
	Del      Code = Virt + 6
	PageUp   Code = Virt + 7
	PageDown Code = Virt + 8
	Ins      Code = Virt + 9

	F1  Code = Virt + 11
	F2  Code = Virt + 12
	F3  Code = Virt + 13
	F4  Code = Virt + 14
	F5  Code = Virt + 15
	F6  Code = Virt + 16
	F7  Code = Virt + 17
	F8  Code = Virt + 18
	F9  Code = Virt + 19
	F10 Code = Virt + 20
	F11 Code = Virt + 21
	F12 Code = Virt + 22
)

// The events, which the edit loop reads from the same stream as the keys.
const (
	EventBase   Code = 0x02000000
	EventResize Code = EventBase + 1
	// EventAutoTab asks the edit loop to complete without the user pressing
	// Tab.
	EventAutoTab Code = EventBase + 2
	EventStop    Code = EventBase + 3
)

// ShiftTab is what the terminal sends for shift+Tab, and what ctrl+Tab
// becomes as well.
const ShiftTab = Tab | ModShift

// F returns the code of function key n, counting from one.
func F(n int) Code {
	return F1 + Code(n) - 1
}

// Mods returns just the modifier bits of c.
func (c Code) Mods() Code {
	return c & ModMask
}

// NoMods returns c without its modifier bits.
func (c Code) NoMods() Code {
	return c & Mask
}

// ASCIIChar returns the printable ASCII character that c stands for.
//
// The modifiers count, so ctrl+A is not an ASCII character even though its
// bottom bits hold one.
func (c Code) ASCIIChar() (byte, bool) {
	if c >= Space && c <= 0x7F {
		return byte(c), true
	}
	return 0, false
}

// Unicode returns the code point that c stands for. Every code up to U+10FFFF
// is one, so this is false only for a virtual key or an event.
func (c Code) Unicode() (rune, bool) {
	if c <= UnicodeMax {
		return rune(c), true
	}
	return 0, false
}

// IsVirtKey reports whether c is a key that has no printable character: a
// control character, the space, or a virtual key or event.
//
// Space counts, because the test is for a code of 0x20 or less rather than
// less than 0x20. The C code is the same.
func (c Code) IsVirtKey() bool {
	return c.NoMods() <= 0x20 || c.NoMods() >= Virt
}
