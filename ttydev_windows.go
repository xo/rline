//go:build windows

package rline

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"

	"github.com/xo/rline/key"
)

// The Windows console.
//
// Windows does not hand a program a stream of bytes from the terminal. It
// hands it key events, each naming a virtual key, a character and which
// modifiers were held. So this turns every event back into the escape
// sequence a Unix terminal would have sent, and hands those bytes on. The
// decoder above it is the same one every system uses, which is how the C code
// keeps one decoder rather than two.
//
// The sequences themselves are built in winkey.go, which is not behind a
// build tag, because the functions that build them sit above the part of
// tty.c that is compiled per system. So they are checked against the C on
// every system rather than only on this one.
//
// Ported from isocline/src/tty.c.

// errNotATerminal says the handle is not a console.
var errNotATerminal = errors.New("not a console")

// Console modes. Raw mode keeps the ability to select text with the mouse,
// and asks for the events that say the window changed size. It deliberately
// leaves out the modes that would have the console interpret keys itself.
const (
	enableQuickEditMode = 0x0040
	enableWindowInput   = 0x0008
	rawConsoleMode      = enableQuickEditMode | enableWindowInput
)

// The virtual keys that have no character of their own.
const (
	vkTab    = 0x09
	vkReturn = 0x0D
	vkMenu   = 0x12 // the Alt key
	vkShift  = 0x10
	vkPrior  = 0x21 // Page Up
	vkNext   = 0x22 // Page Down
	vkEnd    = 0x23
	vkHome   = 0x24
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28
	vkDelete = 0x2E
	vkF1     = 0x70
	vkF5     = 0x74
	vkF6     = 0x75
	vkF10    = 0x79
	vkF11    = 0x7A
	vkF12    = 0x7B
)

// ttyDevice is the console that keys arrive on.
type ttyDevice struct {
	handle windows.Handle

	// mu guards the mode, because the mode is put back from elsewhere when
	// the program is stopping.
	mu         sync.Mutex
	origMode   uint32
	rawEnabled bool

	// pending holds the bytes of the sequence built from the last key event,
	// in the order they are to be read.
	pending []byte

	// surrogate holds the first half of a character that arrived in two
	// events, which is how Windows sends anything above U+FFFF.
	surrogate uint32

	// resized is set when the window changes size.
	resized atomic.Bool
}

// isATTY reports whether the standard input is a console. The file descriptor
// is ignored, because a console is reached by handle rather than by
// descriptor, and isocline only ever reads the standard input.
func isATTY(_ int) bool {
	h, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return false
	}
	var mode uint32
	return windows.GetConsoleMode(h, &mode) == nil
}

// openTTYDevice prepares the console for reading keys. The file descriptor is
// ignored, for the reason isATTY gives.
func openTTYDevice(_ int) (*ttyDevice, error) {
	h, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return nil, fmt.Errorf("taking the console handle: %w", errNotATerminal)
	}
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return nil, fmt.Errorf("reading the console mode: %w", errNotATerminal)
	}
	return &ttyDevice{handle: h, origMode: mode}, nil
}

// close puts the console back the way it was.
func (d *ttyDevice) close() error {
	d.endRaw()
	return nil
}

// startRaw puts the console into the mode that delivers keys one at a time.
func (d *ttyDevice) startRaw() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rawEnabled {
		return nil
	}
	if err := windows.GetConsoleMode(d.handle, &d.origMode); err != nil {
		return fmt.Errorf("reading the console mode: %w", err)
	}
	if err := windows.SetConsoleMode(d.handle, rawConsoleMode); err != nil {
		return fmt.Errorf("putting the console into raw mode: %w", err)
	}
	d.rawEnabled = true
	return nil
}

// endRaw puts the console back the way it was.
func (d *ttyDevice) endRaw() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.rawEnabled {
		return
	}
	if err := windows.SetConsoleMode(d.handle, d.origMode); err != nil {
		return
	}
	d.rawEnabled = false
}

// readByte returns the next byte of the sequence built from a key event,
// reading another event when the last one has been used up.
func (d *ttyDevice) readByte(timeout time.Duration) (byte, bool) {
	if b, ok := d.takePending(); ok {
		return b, true
	}
	d.waitForKey(timeout)
	return d.takePending()
}

// takePending returns the next byte already built, if there is one.
func (d *ttyDevice) takePending() (byte, bool) {
	if len(d.pending) == 0 {
		return 0, false
	}
	b := d.pending[0]
	d.pending = d.pending[1:]
	return b, true
}

// push adds a sequence to what is waiting to be read.
//
// A zero byte ends it and everything from there is dropped, which is what the
// C code does by measuring the sequence with strlen.
func (d *ttyDevice) push(s string) {
	if n := limitToLength(s); n > 0 {
		d.pending = append(d.pending, s[:n]...)
	}
}

// waitForKey reads console events until one turns into something to read, or
// until the wait runs out. A negative timeout waits for as long as it takes.
func (d *ttyDevice) waitForKey(timeout time.Duration) {
	for len(d.pending) == 0 {
		if timeout >= 0 && !d.inputWaiting(&timeout) {
			return
		}
		var record inputRecord
		var read uint32
		if err := readConsoleInput(d.handle, &record, 1, &read); err != nil || read != 1 {
			return
		}
		d.handleEvent(&record)
	}
}

// inputWaiting reports whether an event is there to be read, waiting up to
// timeout for one and taking what it waited off the timeout.
func (d *ttyDevice) inputWaiting(timeout *time.Duration) bool {
	var count uint32
	if err := getNumberOfConsoleInputEvents(d.handle, &count); err != nil {
		return false
	}
	if count > 0 {
		return true
	}
	if *timeout == 0 {
		return false
	}
	start := time.Now()
	ms := uint32(*timeout / time.Millisecond)
	res, err := windows.WaitForSingleObject(d.handle, ms)
	if err != nil || res != windows.WAIT_OBJECT_0 {
		return false
	}
	*timeout -= time.Since(start)
	if *timeout < 0 {
		*timeout = 0
	}
	return true
}

// handleEvent turns one console event into bytes to be read, if it is a key
// press that means anything.
func (d *ttyDevice) handleEvent(record *inputRecord) {
	if record.eventType == windowBufferSizeEvent {
		d.resized.Store(true)
		return
	}
	if record.eventType != keyEvent {
		return
	}
	ev := record.asKeyEvent()

	state := ev.controlKeyState
	// A shift release has to be handled on its own, or letting go of shift
	// looks like shift being held.
	if ev.keyDown == 0 && ev.virtualKeyCode == vkShift {
		state &^= shiftPressed
	}
	// AltGr arrives as left ctrl and right alt together, and means neither.
	const altGr = leftCtrlPressed | rightAltPressed
	if state&altGr == altGr {
		state &^= altGr
	}

	var mods key.Code
	if state&(leftCtrlPressed|rightCtrlPressed) != 0 {
		mods |= key.ModCtrl
	}
	if state&(leftAltPressed|rightAltPressed) != 0 {
		mods |= key.ModAlt
	}
	if state&shiftPressed != 0 {
		mods |= key.ModShift
	}

	// Only a key going down counts, except for Alt coming up, which is how a
	// character pasted by holding Alt and typing its number arrives.
	if ev.keyDown == 0 && ev.virtualKeyCode != vkMenu {
		return
	}

	chr := uint32(ev.unicodeChar)
	if chr == 0 {
		d.pushVirtualKey(mods, ev.virtualKeyCode)
		return
	}
	switch {
	case chr >= 0xD800 && chr <= 0xDBFF:
		// The first half of a character above U+FFFF. Keep it until the
		// second half arrives.
		d.surrogate = chr - 0xD800
		return
	case chr >= 0xDC00 && chr <= 0xDFFF:
		chr = (d.surrogate << 10) + (chr - 0xDC00) + 0x10000
		d.surrogate = 0
	}
	d.push(csiUnicodeSequence(mods, chr))
}

// pushVirtualKey turns a key with no character of its own into a sequence.
// A key with no rule here is ignored, which is what happens to shift and the
// other keys that only modify.
func (d *ttyDevice) pushVirtualKey(mods key.Code, virt uint16) {
	switch virt {
	case vkUp:
		d.push(csiXtermSequence(mods, 'A'))
	case vkDown:
		d.push(csiXtermSequence(mods, 'B'))
	case vkRight:
		d.push(csiXtermSequence(mods, 'C'))
	case vkLeft:
		d.push(csiXtermSequence(mods, 'D'))
	case vkEnd:
		d.push(csiXtermSequence(mods, 'F'))
	case vkHome:
		d.push(csiXtermSequence(mods, 'H'))
	case vkDelete:
		d.push(csiVTSequence(mods, 3))
	case vkPrior:
		d.push(csiVTSequence(mods, 5))
	case vkNext:
		d.push(csiVTSequence(mods, 6))
	case vkTab:
		d.push(csiUnicodeSequence(mods, uint32(key.Tab)))
	case vkReturn:
		d.push(csiUnicodeSequence(mods, uint32(key.Enter)))
	default:
		// The function keys are numbered in three runs, and the third one
		// carries on from where the first left off rather than from the
		// second. That is what the C table does.
		var vtcode uint32
		switch {
		case virt >= vkF1 && virt <= vkF5:
			vtcode = 10 + uint32(virt-vkF1)
		case virt >= vkF6 && virt <= vkF10:
			vtcode = 17 + uint32(virt-vkF6)
		case virt >= vkF11 && virt <= vkF12:
			vtcode = 13 + uint32(virt-vkF11)
		}
		if vtcode > 0 {
			d.push(csiVTSequence(mods, vtcode))
		}
	}
}

// resizeEvent reports whether the window changed size since the last call.
// The console always reports these once raw mode is on, so unlike the Unix
// side there is no case where it has to guess.
func (d *ttyDevice) resizeEvent() bool {
	return d.resized.Swap(false)
}

// asyncStop makes a waiting read return, by putting a ctrl+c into the input
// of the console as a key going down and then coming up.
func (d *ttyDevice) asyncStop() bool {
	var events [2]inputRecord
	events[0] = makeKeyEventRecord(byte(key.CtrlC), true)
	events[1] = makeKeyEventRecord(byte(key.CtrlC), false)
	var written uint32
	if err := writeConsoleInput(d.handle, &events[0], 2, &written); err != nil {
		return false
	}
	return written == 2
}

// localeIsUTF8 reports whether the input is UTF-8. The console gives
// characters rather than bytes, so it always is.
func localeIsUTF8() bool {
	return true
}

// defaultEscInitial is how long to wait for the byte after an escape.
//
// Nothing here ever waits for one in practice, because a sequence is built
// from a key event all at once and arrives whole. It matters only if a
// program feeds bytes in some other way.
const defaultEscInitial = 100 * time.Millisecond

// openTTY opens the console and returns a tty that reads keys from it.
func openTTY(fd int) (*tty, error) {
	d, err := openTTYDevice(fd)
	if err != nil {
		return nil, err
	}
	t := newTTY(d)
	t.dev = d
	t.isUTF8 = localeIsUTF8()
	t.escInitialTimeout = defaultEscInitial
	return t, nil
}
