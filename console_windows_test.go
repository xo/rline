//go:build windows

package rline

import (
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/xo/rline/key"
)

// Tests that need a real console.
//
// Written by the windows-vm session, which has the only machine that can run
// them, and kept here with small changes: the cursor is put back afterwards,
// and the way to make them run is written down rather than assumed.
//
// Both need a console on the standard input, and go test never gives the test
// binary one: it hands it a null input whatever window it was started from.
// So they have to be built and started by hand:
//
//	go test -c -o rline.test.exe .
//	rline.test.exe -test.run TestConsoleKeysRoundTrip -test.v > out.txt 2>&1
//
// The two differ in what they want of the standard output, and it matters.
// TestConsoleKeysRoundTrip does not care, so redirect it and read the file.
// TestConsoleReadsEscapeSequences needs the output attached to the console,
// because redirecting it is the one case where the flag it checks makes no
// difference: the sequences go to the file and the console is not involved.
// Redirecting that one makes it skip, so run it with nothing redirected and
// read what it says on the console:
//
//	rline.test.exe -test.run TestConsoleReadsEscapeSequences -test.v
//
// A skip here means nothing was checked against a real console. That is worth
// knowing rather than reading as a pass.

// TestConsoleKeysRoundTrip pushes key events into the real console input
// buffer and reads them back out through the device, so that
// ReadConsoleInput, the device, the sequence builder and the decoder all run
// against a console rather than a stub.
//
// The limitation worth stating: the events are written by this test rather
// than by the keyboard driver, so this covers everything from the console
// input buffer onwards and nothing about how the driver fills it. Somebody
// physically pressing F11 is still a different test.
func TestConsoleKeysRoundTrip(t *testing.T) {
	if !isATTY(0) {
		t.Skip("no console on standard input: see the comment above for how to run this")
	}
	term, d := openConsoleForTest(t)
	if err := d.startRaw(); err != nil {
		t.Fatalf("starting raw mode: %v", err)
	}
	defer d.endRaw()

	// A key press is a key going down and then coming up.
	press := func(virt uint16, ch rune, state uint32) []inputRecord {
		return []inputRecord{
			keyRecord(true, virt, ch, state),
			keyRecord(false, virt, ch, state),
		}
	}

	for _, test := range []struct {
		name    string
		records []inputRecord
		want    key.Code
	}{
		{"up", press(vkUp, 0, 0), key.Up},
		{"down", press(vkDown, 0, 0), key.Down},
		{"left", press(vkLeft, 0, 0), key.Left},
		{"right", press(vkRight, 0, 0), key.Right},
		{"home", press(vkHome, 0, 0), key.Home},
		{"end", press(vkEnd, 0, 0), key.End},
		{"delete", press(vkDelete, 0, 0), key.Del},
		{"page up", press(vkPrior, 0, 0), key.PageUp},
		{"page down", press(vkNext, 0, 0), key.PageDown},
		// Both ends of all three runs of function keys. The last run is the
		// one the C numbers wrongly, so F11 and F12 are the point of this.
		{"f1", press(vkF1, 0, 0), key.F1},
		{"f5", press(vkF5, 0, 0), key.F5},
		{"f6", press(vkF6, 0, 0), key.F6},
		{"f10", press(vkF10, 0, 0), key.F10},
		{"f11", press(vkF11, 0, 0), key.F11},
		{"f12", press(vkF12, 0, 0), key.F12},
		{"ctrl and a letter", press(0, 'a', leftCtrlPressed), 'a' | key.ModCtrl},
		{"alt and a letter", press(0, 'a', leftAltPressed), 'a' | key.ModAlt},
		{"a plain letter", press(0, 'a', 0), 'a'},
		// A character above U+FFFF arrives as two events and is put back
		// together.
		{"a character above U+FFFF", []inputRecord{
			keyRecord(true, 0, 0xD83D, 0),
			keyRecord(true, 0, 0xDE00, 0),
		}, key.Code(0x1F600)},
	} {
		var written uint32
		if err := writeConsoleInput(d.handle, &test.records[0], uint32(len(test.records)), &written); err != nil {
			t.Errorf("%s: writing the events: %v", test.name, err)
			continue
		}
		got, ok := term.readTimeout(2 * time.Second)
		if !ok {
			t.Errorf("%s: nothing came back, want %08x", test.name, test.want)
			continue
		}
		if got != test.want {
			t.Errorf("%s: gave %08x, want %08x", test.name, got, test.want)
			continue
		}
		if extra, more := term.readTimeout(50 * time.Millisecond); more {
			t.Errorf("%s: gave a second key %08x as well", test.name, extra)
		}
	}
}

// TestConsoleReadsEscapeSequences checks what the port promises: that after
// startRaw the console reads the escape sequences written to it, and that
// endRaw puts the console back as it was found.
//
// It deliberately does not assert anything about the state before startRaw.
// An earlier version did, and it was wrong twice over. It asserted that the
// console already read sequences, which was true of the port before it asked
// for the flag and is not true now. And it read that state from a freshly
// opened CONOUT$ rather than from the handle the port uses, so with the
// output redirected it read a console that the port was not writing to, and
// passed while the port's own handle was not a console at all. The state
// before anything asks is a fact about the machine, so it is logged rather
// than asserted.
//
// This one needs the standard output attached to the console as well as the
// standard input, because that is the only configuration in which the flag
// matters: with the output redirected the sequences go to the file and the
// console is not involved. So it cannot be run with the output redirected to
// capture it, and what it finds appears on the console itself.
func TestConsoleReadsEscapeSequences(t *testing.T) {
	if !isATTY(0) {
		t.Skip("no console on standard input: see the comment above for how to run this")
	}
	h, before, ok := consoleOutput()
	if !ok {
		t.Skip("the standard output is not a console, so the flag would not matter: " +
			"run this with the output attached rather than redirected")
	}
	// What the machine does before anything asks. Not an assertion: it
	// varies with what else has written to the console, and the port no
	// longer depends on it.
	t.Logf("before startRaw the console output mode is %#06x and escape sequences are %v",
		before, before&enableVirtualTerminalProcessing != 0)
	t.Logf("WT_SESSION=%q TERM=%q TERM_PROGRAM=%q",
		os.Getenv("WT_SESSION"), os.Getenv("TERM"), os.Getenv("TERM_PROGRAM"))

	_, d := openConsoleForTest(t)
	if err := d.startRaw(); err != nil {
		t.Fatalf("starting raw mode: %v", err)
	}

	if _, during, ok := consoleOutput(); ok {
		t.Logf("after startRaw the console output mode is %#06x", during)
		if during&enableVirtualTerminalProcessing == 0 {
			t.Error("startRaw left escape sequences off, so the editor would draw them as text")
		}
	}

	// The flag being set is not proof that the console acts on it, so move
	// the cursor with a sequence and ask where it went. Row 5 and column 10
	// count from one, so the answer is X=9 Y=4.
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(h, &info); err != nil {
		t.Fatalf("reading where the cursor is: %v", err)
	}
	at := info.CursorPosition
	defer func() { _ = windows.SetConsoleCursorPosition(h, at) }()

	var written uint32
	if err := windows.WriteFile(h, []byte("\x1b[5;10H"), &written, nil); err != nil {
		t.Fatalf("writing the sequence: %v", err)
	}
	if err := windows.GetConsoleScreenBufferInfo(h, &info); err != nil {
		t.Fatalf("reading where the cursor went: %v", err)
	}
	if got := info.CursorPosition; got.X != 9 || got.Y != 4 {
		t.Errorf("after startRaw the console printed the escape sequence instead of reading it: "+
			"the cursor went to X=%d Y=%d rather than X=9 Y=4", got.X, got.Y)
	}

	// And the console has to be left as it was found, including when the
	// flag had to be turned on.
	d.endRaw()
	if _, after, ok := consoleOutput(); ok && after != before {
		t.Errorf("after endRaw the console output mode is %#06x, want %#06x", after, before)
	}
}

// openConsoleForTest opens the console and returns both the tty and the
// device behind it. The tests need the device as well, because writing
// events into the console needs its handle, and the tty only keeps it as an
// interface.
func openConsoleForTest(t *testing.T) (*tty, *ttyDevice) {
	t.Helper()
	d, err := openTTYDevice(0)
	if err != nil {
		t.Fatalf("opening the console: %v", err)
	}
	t.Cleanup(func() { _ = d.close() })
	term := newTTY(d)
	term.dev = d
	term.isUTF8 = localeIsUTF8()
	term.escInitialTimeout = defaultEscInitial
	return term, d
}
