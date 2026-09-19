//go:build windows

package rline

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The parts of the console input interface that golang.org/x/sys/windows
// does not carry: the shape of an input record, and the two calls that read
// and write them.
//
// The layouts here are the ones the Win32 headers give. A record is a tag and
// a union, and the union is four bytes into the record because its largest
// member has to start on a four byte boundary.

// The kinds of console event. Only these two matter here: a key going down
// or coming up, and the window changing size.
const (
	keyEvent              = 0x0001
	windowBufferSizeEvent = 0x0004
)

// The bits that say which modifier keys were held.
const (
	rightAltPressed  = 0x0001
	leftAltPressed   = 0x0002
	rightCtrlPressed = 0x0004
	leftCtrlPressed  = 0x0008
	shiftPressed     = 0x0010
)

// inputRecord is one console event. The union is kept as bytes and read
// through asKeyEvent, because Go has no unions.
type inputRecord struct {
	eventType uint16
	_         uint16 // the union starts on a four byte boundary
	data      [16]byte
}

// keyEventRecord is the union member for a key going down or coming up.
type keyEventRecord struct {
	// keyDown is a Win32 BOOL, which is four bytes rather than one.
	keyDown         int32
	repeatCount     uint16
	virtualKeyCode  uint16
	virtualScanCode uint16
	// unicodeChar is zero for a key that has no character of its own, such
	// as an arrow.
	unicodeChar     uint16
	controlKeyState uint32
}

// asKeyEvent reads the union as a key event. The caller has to have checked
// the kind first.
func (r *inputRecord) asKeyEvent() *keyEventRecord {
	return (*keyEventRecord)(unsafe.Pointer(&r.data[0]))
}

// makeKeyEventRecord builds a record for a character going down or coming
// up, which is what asyncStop writes into the console to wake a read.
func makeKeyEventRecord(ch byte, down bool) inputRecord {
	ev := keyEventRecord{repeatCount: 1, unicodeChar: uint16(ch)}
	if down {
		ev.keyDown = 1
	}
	var record inputRecord
	record.eventType = keyEvent
	*(*keyEventRecord)(unsafe.Pointer(&record.data[0])) = ev
	return record
}

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procReadConsoleInputW  = kernel32.NewProc("ReadConsoleInputW")
	procWriteConsoleInputW = kernel32.NewProc("WriteConsoleInputW")
)

// readConsoleInput reads events from the console, waiting for one if none has
// arrived. The wide version is used so that a character above ASCII arrives
// whole.
func readConsoleInput(console windows.Handle, records *inputRecord, count uint32, read *uint32) error {
	r, _, err := procReadConsoleInputW.Call(
		uintptr(console),
		uintptr(unsafe.Pointer(records)),
		uintptr(count),
		uintptr(unsafe.Pointer(read)),
	)
	if r == 0 {
		return fmt.Errorf("reading console input: %w", err)
	}
	return nil
}

// writeConsoleInput puts events into the console input, as though they had
// been typed.
func writeConsoleInput(console windows.Handle, records *inputRecord, count uint32, written *uint32) error {
	r, _, err := procWriteConsoleInputW.Call(
		uintptr(console),
		uintptr(unsafe.Pointer(records)),
		uintptr(count),
		uintptr(unsafe.Pointer(written)),
	)
	if r == 0 {
		return fmt.Errorf("writing console input: %w", err)
	}
	return nil
}

// getNumberOfConsoleInputEvents says how many events are waiting.
func getNumberOfConsoleInputEvents(console windows.Handle, count *uint32) error {
	return windows.GetNumberOfConsoleInputEvents(console, count) //nolint:wrapcheck // nothing to add
}
