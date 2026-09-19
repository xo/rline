//go:build windows

package rline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"github.com/xo/rline/key"
	"golang.org/x/sys/windows"
)

// --------------------------------------------------------------------------
// console_windows_test.go

// Tests that need a real console.
//
// Written by the windows-vm session, which has the only machine that can run
// them, and kept here with small changes: the cursor is put back afterwards,
// and the way to make them run is written down rather than assumed.
//
// Every one of them needs a console on the standard input, and go test never
// gives the test binary one: it hands it a null input whatever window it was
// started from. So they have to be built and started by hand:
//
//	go test -c -o rline.test.exe .
//	rline.test.exe -test.run TestConsoleKeysRoundTrip -test.v > out.txt 2>&1
//
// They differ in what they want of the standard output, and it matters.
// TestConsoleKeysRoundTrip does not care, so redirect it and read the file.
// TestConsoleReadsEscapeSequences needs the output attached to the console,
// because redirecting it is the one case where the flag it checks makes no
// difference: the sequences go to the file and the console is not involved.
// Redirecting that one makes it skip, so run it with nothing redirected and
// read what it says on the console:
//
//	rline.test.exe -test.run TestConsoleReadsEscapeSequences -test.v
//
// TestConsoleEscapesFromOff and TestConsoleWritesToTerminalOnAConsole say
// beside themselves what they want, and the second is worth running both
// ways.
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
	d, err := openTTYDevice(-1)
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

// TestConsoleEscapesFromOff runs the one path nothing else reaches: a console
// that starts with escape sequences turned off, so that startRaw has to turn
// them on rather than finding them on already.
//
// Written by the windows-vm session, which found that a console can start
// either way. Its findings go to a file rather than to the standard output,
// and that is not tidiness: startOutputEscapes looks at the standard output
// handle and does nothing at all when it is redirected, so sending the
// findings there would redirect the very thing being measured. windows-vm
// lost a run to exactly that and reported a silent failure that was its own
// harness.
//
// The original mode is put back first thing, so that a failure part way
// through does not leave the console changed.
//
// On the console that refuses the flag, which nobody has found: startRaw
// returning an error is the port working as promised, so that is reported
// rather than failed. What is a failure is startRaw saying yes and the flag
// still being off, because that is the port breaking silently.
func TestConsoleEscapesFromOff(t *testing.T) {
	if !isATTY(0) {
		t.Skip("no console on standard input: see the comment at the top of this file")
	}
	h, ambient, ok := consoleOutput()
	if !ok {
		t.Skip("the standard output is not a console, so the flag cannot be reached: " +
			"run this with the output attached rather than redirected")
	}
	report, err := os.Create(filepath.Join(t.TempDir(), "escapes-from-off.txt"))
	if err != nil {
		t.Fatalf("making the report: %v", err)
	}
	defer func() { _ = report.Close() }()
	say := func(format string, a ...any) {
		_, _ = fmt.Fprintf(report, format+"\n", a...)
		t.Logf(format, a...)
	}
	defer func() { _ = windows.SetConsoleMode(h, ambient) }()

	say("the console started at %#06x with escape sequences %v",
		ambient, ambient&enableVirtualTerminalProcessing != 0)
	if err := windows.SetConsoleMode(h, ambient&^enableVirtualTerminalProcessing); err != nil {
		t.Skipf("this console will not have the flag cleared, so the path cannot be reached: %v", err)
	}
	var off uint32
	if err := windows.GetConsoleMode(h, &off); err != nil {
		t.Fatalf("reading the mode back: %v", err)
	}
	if off&enableVirtualTerminalProcessing != 0 {
		t.Skip("this console refused to have the flag cleared, so the path cannot be reached")
	}
	say("with the flag cleared the mode is %#06x", off)

	_, d := openConsoleForTest(t)
	if err := d.startRaw(); err != nil {
		// This is the host the C emulation would have been for, and failing
		// here is what the port promises to do on it.
		say("startRaw refused the flag: %v", err)
		t.Skipf("this console refuses the flag, which is the loud failure the port promises: %v", err)
	}
	var on uint32
	if err := windows.GetConsoleMode(h, &on); err != nil {
		t.Fatalf("reading the mode after raw mode: %v", err)
	}
	say("after startRaw the mode is %#06x", on)
	if on&enableVirtualTerminalProcessing == 0 {
		t.Error("startRaw reported success and left escape sequences off, " +
			"so the editor would draw them as text with nothing to say so")
	}

	d.endRaw()
	var back uint32
	if err := windows.GetConsoleMode(h, &back); err != nil {
		t.Fatalf("reading the mode after leaving raw mode: %v", err)
	}
	say("after endRaw the mode is %#06x", back)
	if back != off {
		t.Errorf("endRaw left the console at %#06x, want the %#06x it found", back, off)
	}
}

// TestConsoleWritesToTerminalOnAConsole is the check that
// TestWritesToTerminalLooksAtTheWriter in api_test.go cannot make.
//
// Colour is turned off when the output is not a terminal, and the question
// was answered on Windows by looking at the standard input, so a program with
// its output redirected wrote escape sequences into the file. That answer is
// only wrong while a console is on the standard input at the same time, and
// go test never gives the test binary one, so the check in api_test.go passes
// against the bug it was written to catch. Here it fails against it.
//
// Run it both ways. With the output attached, the temporary file below is the
// case that matters. With the output redirected, os.Stdout is as well, and
// that is the one a user meets:
//
//	rline.test.exe -test.run TestConsoleWritesToTerminalOnAConsole -test.v
//	rline.test.exe -test.run TestConsoleWritesToTerminalOnAConsole -test.v > out.txt 2>&1
//
// The attached run gives an exit code and nothing a reader can capture, the
// same as TestConsoleReadsEscapeSequences: what it says appears on the
// console itself, for a human to read. So the log line below can only ever be
// captured saying false, because capturing it is what makes it false. That is
// the null standard input again in miniature — observing changes the thing
// observed — and it is why the assertion asks the console what the answer
// should be rather than leaving it to the reader of a log line.
//
// Found by the windows-vm session, which measured isATTY answering true for a
// redirected standard output while a console was on the standard input.
func TestConsoleWritesToTerminalOnAConsole(t *testing.T) {
	if !isATTY(0) {
		t.Skip("no console on standard input, which is the only state this can fail in: " +
			"see the comment at the top of this file")
	}
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatalf("making a file: %v", err)
	}
	defer func() { _ = f.Close() }()
	if writesToTerminal(f) {
		t.Error("a plain file is taken for a terminal while a console is on the standard input, " +
			"so colour would be written into a redirected output")
	}

	// And the writer a program actually passes. Whether this one is a console
	// depends on how the binary was started, so the answer is checked against
	// the console rather than fixed.
	_, _, isConsole := consoleOutput()
	t.Logf("the standard output is a console: %v", isConsole)
	if got := writesToTerminal(os.Stdout); got != isConsole {
		t.Errorf("writesToTerminal(os.Stdout) is %v, want %v", got, isConsole)
	}
}

// --------------------------------------------------------------------------
// ttydev_windows_test.go

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
		// The last two depart from the C on purpose, which sends 13 and 14
		// where its own decoder reads 23 and 24. These expectations used to
		// hold the C numbers, which made them agree with the encoder rather
		// than with the truth, so they stayed green while F11 arrived as F4.
		// Only the round trip below caught it, and only for F12, because
		// F11 had no case there. It has one now.
		{"the eleventh function key", keyRecord(true, vkF11, 0, 0), "\x1b[23;1~"},
		{"the twelfth function key", keyRecord(true, vkF12, 0, 0), "\x1b[24;1~"},
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
		{"the fifth function key", keyRecord(true, vkF5, 0, 0), key.F5},
		{"the sixth function key", keyRecord(true, vkF6, 0, 0), key.F6},
		{"the tenth function key", keyRecord(true, vkF10, 0, 0), key.F10},
		{"the eleventh function key", keyRecord(true, vkF11, 0, 0), key.F11},
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
// It needs a real console on the standard input, and go test never gives the
// test binary one: it hands it a null input whatever window it was started
// from, so this skips even from a console. To make it run, build the binary
// and start it yourself with only the output redirected:
//
//	go test -c -o rline.test.exe .
//	rline.test.exe -test.run TestWindows -test.v > out.txt 2>&1
//
// A skip here means raw mode was not checked at all, which is worth knowing
// rather than reading as a pass.
func TestWindowsRawModeRoundTrip(t *testing.T) {
	if !isATTY(0) {
		t.Skip("no console on standard input: see the comment above for how to run this so it checks raw mode")
	}
	d, err := openTTYDevice(-1)
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
// thing the wait can be made against, and it is reached the same way
// TestWindowsRawModeRoundTrip is.
func TestWindowsReadByteTimesOut(t *testing.T) {
	if !isATTY(0) {
		t.Skip("no console on standard input: see TestWindowsRawModeRoundTrip for how to run this")
	}
	d, err := openTTYDevice(-1)
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

// TestConsoleOpenTTYDeviceHonoursItsArgument checks that a descriptor handed
// to openTTYDevice is used rather than thrown away.
//
// It ignored its argument and always took the standard input, which made
// WithInput and WithInputFd silently do nothing on Windows: the caller's
// stream was accepted and discarded, the console was read instead, and
// because opening it succeeded the reader stayed in editing mode, so it
// looked as though it had worked. Found by windows-vm, by passing a file that
// already held a whole line and watching ReadLine wait for the keyboard
// instead of returning it.
//
// This needs a console on the standard input, because the point of it is that
// a plain file is refused while a console is there to be taken by mistake.
// See the comment at the top of this file for how to run it.
func TestConsoleOpenTTYDeviceHonoursItsArgument(t *testing.T) {
	if !isATTY(0) {
		t.Skip("no console on standard input, which is the only state this can fail in: " +
			"see the comment at the top of this file")
	}
	f, err := os.CreateTemp(t.TempDir(), "in")
	if err != nil {
		t.Fatalf("making a file: %v", err)
	}
	defer func() { _ = f.Close() }()

	// A plain file is not a console, so opening it as one has to fail. If the
	// argument were ignored this would take the console and succeed.
	if d, err := openTTYDevice(int(f.Fd())); err == nil {
		_ = d.close()
		t.Error("a plain file opened as a console, so the descriptor was thrown away " +
			"and the console taken instead")
	}

	// And a negative descriptor still means the standard input, which is what
	// every caller that has not been given one passes.
	d, err := openTTYDevice(-1)
	if err != nil {
		t.Fatalf("opening the standard input as a console: %v", err)
	}
	_ = d.close()
}
