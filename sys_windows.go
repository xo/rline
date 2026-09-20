//go:build windows

// The Windows console.
//
// Windows does not hand a program a stream of bytes from the terminal. It
// hands it key events, each naming a virtual key, a character and which
// modifiers were down, so this file reads those events and builds the escape
// sequences the rest of the port expects. It also turns on the console flag
// that makes escape sequences mean something on the way out, and asks the
// console how wide it is.

package rline

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/xo/rline/internal/text"
	"github.com/xo/rline/key"
	"golang.org/x/sys/windows"
)

// --------------------------------------------------------------------------
// ttydev_windows.go

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

// Console modes. Raw mode keeps the ability to select text with the mouse,
// and asks for the events that say the window changed size. It deliberately
// leaves out the modes that would have the console interpret keys itself.
const (
	enableWindowInput   = 0x0008
	enableQuickEditMode = 0x0040
	// enableExtendedFlags has to be on for enableQuickEditMode to mean
	// anything. The console turns it on by itself when quick edit is asked
	// for and hands it back on the next read, so asking for it makes what
	// comes back match what went in.
	enableExtendedFlags = 0x0080

	rawConsoleMode = enableExtendedFlags | enableQuickEditMode | enableWindowInput
)

// enableVirtualTerminalProcessing makes the console read the escape
// sequences that are written to it, rather than printing them.
const enableVirtualTerminalProcessing = 0x0004

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

	// outHandle is the console being written to, and origOutMode how it was
	// set up, kept only when this turned the escape sequence flag on and so
	// has something to put back.
	outHandle   windows.Handle
	origOutMode uint32
	outChanged  bool

	// pending holds the bytes of the sequence built from the last key event,
	// in the order they are to be read.
	pending []byte

	// surrogate holds the first half of a character that arrived in two
	// events, which is how Windows sends anything above U+FFFF.
	surrogate uint32

	// resized is set when the window changes size.
	resized atomic.Bool
}

// isTerminal reports whether the standard input is a console.
//
// The argument is ignored on purpose: this answers "is there a keyboard",
// which is always about the standard input. Do not give it a second meaning.
// Two faults on this platform came from one function standing for two
// questions — writesToTerminal asked this one about the output, and
// openTTYDevice took a descriptor and threw it away — so anything that wants
// to ask about a particular stream has its own function. fileIsTerminal asks
// about a file, and openTTYDevice now honours the descriptor it is handed.
func isTerminal(_ int) bool {
	h, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return false
	}
	var mode uint32
	return windows.GetConsoleMode(h, &mode) == nil
}

// fileIsTerminal reports whether f is a console.
//
// This asks about f, unlike isTerminal, which asks about the standard input
// whatever it is handed. A handle is what Windows answers for, and f.Fd()
// returns one rather than a descriptor, so it can be asked about directly.
func fileIsTerminal(f *os.File) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) == nil
}

// openTTYDevice prepares the console for reading keys. The file descriptor is
// ignored, for the reason isTerminal gives.
func openTTYDevice(fd int) (*ttyDevice, error) {
	// A negative descriptor means the standard input, as it does on Unix.
	// Anything else is a handle the caller gave, because f.Fd() on Windows
	// returns a handle rather than a descriptor.
	//
	// This used to ignore its argument and always take the standard input,
	// which made WithStdin silently do nothing: the caller's
	// stream was accepted, discarded, and the console read instead, in
	// editing mode, so it looked as though it had worked. Found by the
	// windows-vm session, by passing a file that already held a line and
	// watching ReadLine wait for the keyboard.
	h := windows.Handle(uintptr(fd)) //nolint:gosec // a handle, not a size
	if fd < 0 {
		var err error
		if h, err = windows.GetStdHandle(windows.STD_INPUT_HANDLE); err != nil {
			return nil, fmt.Errorf("taking the console handle: %w", errNotATerminal)
		}
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
	if err := d.startOutputEscapes(); err != nil {
		// Put the input back rather than leave the console half changed.
		_ = windows.SetConsoleMode(d.handle, d.origMode)
		return err
	}
	d.rawEnabled = true
	return nil
}

// consoleOutput returns the console that output goes to, and how it is set
// up. It reports false when the output is not a console at all, which is not
// an error: it means the output was redirected.
func consoleOutput() (windows.Handle, uint32, bool) {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return 0, 0, false
	}
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return 0, 0, false
	}
	return h, mode, true
}

// startOutputEscapes makes sure the console reads the escape sequences that
// are written to it.
//
// The port has no Windows branch in term.go: it writes escape sequences on
// every system and needs the console to read them, where the C instead
// carries several hundred lines that turn them into console calls. This is
// what stands in for those lines, and it is not a precaution: the flag is
// off by default on an ordinary Windows 11 console when the output is
// attached to it, and without this the editor would draw its escape
// sequences on the screen as text. That was measured rather than assumed.
//
// The flag belongs to the console rather than to the process, so another
// handle in this program or another program sharing the console can clear
// it. Hence asking each time rather than once.
//
// A console that refuses the flag would be one where the C emulation would
// have been needed. That fails here, loudly, rather than printing the escape
// sequences to the user. No such console has been found: the one measured
// accepts the flag even when it starts with it off.
func (d *ttyDevice) startOutputEscapes() error {
	h, mode, ok := consoleOutput()
	if !ok {
		// The output is going to a file or a pipe rather than a console, so
		// the escape sequences go with it the same as on any other system
		// and there is nothing to turn on.
		return nil
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return nil // already on, so there is nothing to put back either
	}
	if err := windows.SetConsoleMode(h, mode|enableVirtualTerminalProcessing); err != nil {
		return fmt.Errorf("this console will not read escape sequences, and rline writes them: %w", err)
	}
	d.outHandle, d.origOutMode, d.outChanged = h, mode, true
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
	if d.outChanged {
		_ = windows.SetConsoleMode(d.outHandle, d.origOutMode)
		d.outChanged = false
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
	if n := text.LimitToLength(s); n > 0 {
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
	count, err := pendingConsoleEvents(d.handle)
	if err != nil {
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
		// The function keys are numbered in three runs.
		//
		// The third run departs from the C, which numbers it 13 and 14. Its
		// own decoder reads 13 and 14 as F4 and F5, and reads F11 and F12
		// from 23 and 24, so the two halves of the C disagree and pressing
		// F11 on Windows gives F4. windows-vm confirmed that end to end
		// against a real console before this was changed.
		//
		// There is no faithful answer here, because being faithful to the
		// encoder means being unfaithful to the decoder. The decoder is the
		// half with recorded cases behind it on two systems, 23 and 24 are
		// the numbers every other terminal uses, and no recorded session
		// carries the C's answer, because nothing records on Windows. So
		// this sends what the decoder reads. PLAN.md has the whole of it.
		var vtcode uint32
		switch {
		case virt >= vkF1 && virt <= vkF5:
			vtcode = 10 + uint32(virt-vkF1)
		case virt >= vkF6 && virt <= vkF10:
			vtcode = 17 + uint32(virt-vkF6)
		case virt >= vkF11 && virt <= vkF12:
			vtcode = 23 + uint32(virt-vkF11)
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

// --------------------------------------------------------------------------
// winconsole_windows.go

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

// pendingConsoleEvents says how many events are waiting.
//
// The Win32 call returns its answer through a pointer. Go returns values, so
// the out parameter stops here rather than spreading into the caller.
func pendingConsoleEvents(console windows.Handle) (uint32, error) {
	var count uint32
	if err := windows.GetNumberOfConsoleInputEvents(console, &count); err != nil {
		return 0, err //nolint:wrapcheck // nothing to add
	}
	return count, nil
}

// --------------------------------------------------------------------------
// termsize_windows.go

// fileSizer reports the size of the console a file is connected to.
type fileSizer struct {
	f *os.File
}

// size returns the width and height in characters.
//
// The window is measured rather than the buffer. A console buffer is often
// taller than the window showing it, and what matters is what can be seen.
func (s fileSizer) size() (int, int, bool) {
	if s.f == nil {
		return 0, 0, false
	}
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(s.f.Fd()), &info); err != nil {
		return 0, 0, false
	}
	cols := int(info.Window.Right) - int(info.Window.Left) + 1
	rows := int(info.Window.Bottom) - int(info.Window.Top) + 1
	if cols <= 0 || rows <= 0 {
		return 0, 0, false
	}
	return cols, rows, true
}
