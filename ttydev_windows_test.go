//go:build windows

package rline

import (
	"errors"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/xo/rline/key"
)

// Tests for the Windows console layer.
//
// Turning a console event into bytes needs no console: an event is a plain
// structure, so these build them and check what comes out. That covers the
// virtual key table, the modifier rules and the pair of events that carry a
// character above U+FFFF.
//
// What is left needing a real console is fetching an event, changing the
// mode, and waking a waiting read. Those are checked further down, and they
// say what they could not check when there is no console to hand.

// keyRecord builds a key event the way the console would deliver one.
func keyRecord(down bool, virt uint16, char rune, state uint32) inputRecord {
	ev := keyEventRecord{
		repeatCount:     1,
		virtualKeyCode:  virt,
		unicodeChar:     uint16(char),
		controlKeyState: state,
	}
	if down {
		ev.keyDown = 1
	}
	var record inputRecord
	record.eventType = keyEvent
	*(*keyEventRecord)(unsafe.Pointer(&record.data[0])) = ev
	return record
}

// pendingOf feeds events to a device and returns the bytes they produced.
func pendingOf(records ...inputRecord) string {
	d := &ttyDevice{}
	for i := range records {
		d.handleEvent(&records[i])
	}
	return string(d.pending)
}

// TestWindowsKeyEventsBecomeSequences checks the table that turns a console
// key event into the bytes a Unix terminal would have sent.
func TestWindowsKeyEventsBecomeSequences(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		event inputRecord
		want  string
	}{
		{"a plain letter", keyRecord(true, 0, 'a', 0), "a"},
		{"a letter with ctrl", keyRecord(true, 0, 'a', leftCtrlPressed), "\x1b[97;5u"},
		{"a letter with alt", keyRecord(true, 0, 'a', leftAltPressed), "\x1b[97;3u"},
		{"an arrow", keyRecord(true, vkUp, 0, 0), "\x1b[1;1A"},
		{"an arrow with ctrl", keyRecord(true, vkUp, 0, rightCtrlPressed), "\x1b[1;5A"},
		{"the other arrows", keyRecord(true, vkLeft, 0, 0), "\x1b[1;1D"},
		{"home", keyRecord(true, vkHome, 0, 0), "\x1b[1;1H"},
		{"end", keyRecord(true, vkEnd, 0, 0), "\x1b[1;1F"},
		{"delete", keyRecord(true, vkDelete, 0, 0), "\x1b[3;1~"},
		{"page up", keyRecord(true, vkPrior, 0, 0), "\x1b[5;1~"},
		{"page down", keyRecord(true, vkNext, 0, 0), "\x1b[6;1~"},
		{"the first function key", keyRecord(true, vkF1, 0, 0), "\x1b[10;1~"},
		{"the sixth function key", keyRecord(true, vkF6, 0, 0), "\x1b[17;1~"},
		// The last two carry on from where the first run left off rather
		// than from the second, which is what the C table does.
		{"the eleventh function key", keyRecord(true, vkF11, 0, 0), "\x1b[13;1~"},
		{"the twelfth function key", keyRecord(true, vkF12, 0, 0), "\x1b[14;1~"},
		{"a key coming up is ignored", keyRecord(false, vkUp, 0, 0), ""},
		{"a key with no rule is ignored", keyRecord(true, vkShift, 0, 0), ""},
		// AltGr arrives as left ctrl and right alt together and means
		// neither, so the character goes through plain.
		{"alt gr is not ctrl and alt", keyRecord(true, 0, 'a', leftCtrlPressed|rightAltPressed), "a"},
		{"tab", keyRecord(true, vkTab, 0, 0), "\t"},
		{"tab with shift", keyRecord(true, vkTab, 0, shiftPressed), "\x1b[9;2u"},
		{"enter", keyRecord(true, vkReturn, 0, 0), "\r"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := pendingOf(test.event); got != test.want {
				t.Errorf("gave %q, want %q", got, test.want)
			}
		})
	}
}

// TestWindowsSurrogatePair checks that a character above U+FFFF, which
// arrives as two events, is put back together.
func TestWindowsSurrogatePair(t *testing.T) {
	t.Parallel()
	// U+1F600 arrives as D83D then DE00.
	high := keyRecord(true, 0, 0xD83D, 0)
	low := keyRecord(true, 0, 0xDE00, 0)
	if got := pendingOf(high); got != "" {
		t.Errorf("the first half alone gave %q, want nothing yet", got)
	}
	want := "\x1b[128512;1u"
	if got := pendingOf(high, low); got != want {
		t.Errorf("the pair gave %q, want %q", got, want)
	}
}

// TestWindowsResizeEvent checks that a window size event is noticed and
// reported once.
func TestWindowsResizeEvent(t *testing.T) {
	t.Parallel()
	d := &ttyDevice{}
	if d.resizeEvent() {
		t.Error("a resize was reported before one happened")
	}
	record := inputRecord{eventType: windowBufferSizeEvent}
	d.handleEvent(&record)
	if len(d.pending) != 0 {
		t.Errorf("a resize event produced %q, want nothing to read", d.pending)
	}
	if !d.resizeEvent() {
		t.Error("the resize was not reported")
	}
	if d.resizeEvent() {
		t.Error("the same resize was reported twice")
	}
}

// TestWindowsSequencesDecodeBack checks the whole way round: a console event
// becomes bytes, and the decoder reads those bytes back as the key that was
// pressed. This is the path a key actually takes.
func TestWindowsSequencesDecodeBack(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		event inputRecord
		want  key.Code
	}{
		{"a letter", keyRecord(true, 0, 'a', 0), 'a'},
		{"an arrow", keyRecord(true, vkUp, 0, 0), key.Up},
		{"an arrow with ctrl", keyRecord(true, vkUp, 0, leftCtrlPressed), key.Up | key.ModCtrl},
		{"delete", keyRecord(true, vkDelete, 0, 0), key.Del},
		{"page up with shift", keyRecord(true, vkPrior, 0, shiftPressed), key.PageUp | key.ModShift},
		{"the first function key", keyRecord(true, vkF1, 0, 0), key.F1},
		{"the twelfth function key", keyRecord(true, vkF12, 0, 0), key.F12},
		{"a letter with alt", keyRecord(true, 0, 'a', leftAltPressed), 'a' | key.ModAlt},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			bytes := pendingOf(test.event)
			term := newTTY(&idleReader{bytes: []byte(bytes)})
			term.setEscDelay(0, 0)
			got, ok := term.readTimeout(0)
			if !ok {
				t.Fatalf("the bytes %q decoded to nothing", bytes)
			}
			if got != test.want {
				t.Errorf("the bytes %q decoded to %08x, want %08x", bytes, got, test.want)
			}
			if _, more := term.readTimeout(0); more {
				t.Errorf("the bytes %q decoded to more than one key", bytes)
			}
		})
	}
}

// TestWindowsOpenTTYNeedsAConsole checks the way in. Under go test the
// standard input is usually a pipe rather than a console, and then opening
// it has to fail rather than half work.
func TestWindowsOpenTTYNeedsAConsole(t *testing.T) {
	if isATTY(0) {
		// Running with a real console attached, so opening it must work.
		term, err := openTTY(0)
		if err != nil {
			t.Fatalf("opening a real console: %v", err)
		}
		t.Cleanup(func() { _ = term.close() })
		if term.dev == nil {
			t.Error("the tty has no device")
		}
		return
	}
	if _, err := openTTY(0); !errors.Is(err, errNotATerminal) {
		t.Errorf("opening something that is not a console gave %v, want %v", err, errNotATerminal)
	}
}

// TestWindowsRawModeRoundTrip checks that raw mode is entered and left, and
// that the console is put back the way it was.
//
// It needs a real console, so it says what it could not check when there is
// none. Run it from a console window rather than through a pipe to have it
// mean something.
func TestWindowsRawModeRoundTrip(t *testing.T) {
	if !isATTY(0) {
		t.Skip("no console on standard input: run this from a console window to check raw mode")
	}
	d, err := openTTYDevice(0)
	if err != nil {
		t.Fatalf("opening the console: %v", err)
	}
	t.Cleanup(func() { _ = d.close() })

	before := consoleMode(t, d)
	if err := d.startRaw(); err != nil {
		t.Fatalf("starting raw mode: %v", err)
	}
	during := consoleMode(t, d)
	if during != rawConsoleMode {
		t.Errorf("raw mode is %#x, want %#x", during, uint32(rawConsoleMode))
	}
	if during&enableWindowInput == 0 {
		t.Error("raw mode does not ask for window size events, so a resize cannot be noticed")
	}
	if during&enableQuickEditMode == 0 {
		t.Error("raw mode does not allow selecting text with the mouse")
	}
	d.endRaw()
	if after := consoleMode(t, d); after != before {
		t.Errorf("the console mode is %#x afterwards, want %#x", after, before)
	}
}

// consoleMode reads the current mode of the console.
func consoleMode(t *testing.T, d *ttyDevice) uint32 {
	t.Helper()
	var mode uint32
	if err := windows.GetConsoleMode(d.handle, &mode); err != nil {
		t.Fatalf("reading the console mode: %v", err)
	}
	return mode
}

// TestWindowsReadByteTimesOut checks that a wait with nothing typed ends
// rather than hanging. It needs a real console, because that is the only
// thing the wait can be made against.
func TestWindowsReadByteTimesOut(t *testing.T) {
	if !isATTY(0) {
		t.Skip("no console on standard input: run this from a console window to check the wait")
	}
	d, err := openTTYDevice(0)
	if err != nil {
		t.Fatalf("opening the console: %v", err)
	}
	t.Cleanup(func() { _ = d.close() })
	start := time.Now()
	if _, ok := d.readByte(50 * time.Millisecond); ok {
		t.Skip("something was typed while the test was running")
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("a wait of 50ms took %v", waited)
	}
}
