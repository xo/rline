//go:build unix && !aix

package rline

import (
	"errors"
	"os"
	"testing"
	"time"
)

// newPipe returns the two ends of a pipe, closed when the test ends.
func newPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating a pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	return r, w
}

// TestIsATTYRejectsAPipe checks that only a terminal counts as one.
func TestIsATTYRejectsAPipe(t *testing.T) {
	t.Parallel()
	r, _ := newPipe(t)
	if isATTY(int(r.Fd())) {
		t.Error("a pipe was reported as a terminal")
	}
}

// TestOpenTTYDeviceRejectsAPipe checks that opening something that is not a
// terminal fails, and says so in a way a caller can test for.
func TestOpenTTYDeviceRejectsAPipe(t *testing.T) {
	t.Parallel()
	r, _ := newPipe(t)
	d, err := openTTYDevice(int(r.Fd()))
	if err == nil {
		_ = d.close()
		t.Fatal("opening a pipe as a terminal succeeded")
	}
	if !errors.Is(err, errNotATerminal) {
		t.Errorf("opening a pipe gave %v, want it to wrap %v", err, errNotATerminal)
	}
}

// TestOpenTTYRejectsAPipe checks the whole way in, from a file descriptor to
// a tty ready to read keys. A pipe is not a terminal, so it fails there.
func TestOpenTTYRejectsAPipe(t *testing.T) {
	t.Parallel()
	r, _ := newPipe(t)
	term, err := openTTY(int(r.Fd()))
	if err == nil {
		_ = term.close()
		t.Fatal("opening a pipe as a terminal succeeded")
	}
	if !errors.Is(err, errNotATerminal) {
		t.Errorf("opening a pipe gave %v, want it to wrap %v", err, errNotATerminal)
	}
}

// TestDefaultEscInitialIsSane checks the wait that tells the Escape key from
// the start of a sequence. It differs by system, so this only checks that it
// is long enough to be useful and short enough not to be felt.
func TestDefaultEscInitialIsSane(t *testing.T) {
	t.Parallel()
	if defaultEscInitial < 50*time.Millisecond || defaultEscInitial > escDelayMax {
		t.Errorf("the initial escape wait is %v", defaultEscInitial)
	}
}

// TestTTYDeviceReadByte checks the wait. A byte that is there is returned,
// and a wait with nothing to read ends without one.
func TestTTYDeviceReadByte(t *testing.T) {
	t.Parallel()
	r, w := newPipe(t)
	d := &ttyDevice{fd: int(r.Fd())}

	if _, err := w.WriteString("ab"); err != nil {
		t.Fatalf("writing to the pipe: %v", err)
	}
	for _, want := range []byte{'a', 'b'} {
		got, ok := d.readByte(time.Second)
		if !ok {
			t.Fatalf("no byte where %q was written", want)
		}
		if got != want {
			t.Errorf("read %q, want %q", got, want)
		}
	}

	start := time.Now()
	if _, ok := d.readByte(50 * time.Millisecond); ok {
		t.Error("a byte arrived from an empty pipe")
	}
	if waited := time.Since(start); waited < 25*time.Millisecond {
		t.Errorf("the wait lasted %v, want it to last about 50ms", waited)
	}
}

// TestTTYDeviceReadByteZeroTimeoutDoesNotWait checks that a wait of zero
// answers at once. The escape decoder uses that to look ahead without
// holding up a key press.
func TestTTYDeviceReadByteZeroTimeoutDoesNotWait(t *testing.T) {
	t.Parallel()
	r, _ := newPipe(t)
	d := &ttyDevice{fd: int(r.Fd())}
	start := time.Now()
	if _, ok := d.readByte(0); ok {
		t.Error("a byte arrived from an empty pipe")
	}
	if waited := time.Since(start); waited > 250*time.Millisecond {
		t.Errorf("a wait of zero took %v", waited)
	}
}

// TestTTYDeviceResizeEventBeforeWatching checks that a device that is not
// watching for the signal says a resize happened, because it cannot tell.
func TestTTYDeviceResizeEventBeforeWatching(t *testing.T) {
	t.Parallel()
	d := &ttyDevice{fd: -1}
	if !d.resizeEvent() {
		t.Error("a device that is not watching reported no resize")
	}
}

// TestTTYDeviceResizeEventClearsItself checks that a resize is reported once
// and then not again until the next one.
func TestTTYDeviceResizeEventClearsItself(t *testing.T) {
	t.Parallel()
	d := &ttyDevice{fd: -1}
	d.watching.Store(true)
	if d.resizeEvent() {
		t.Error("a resize was reported before one happened")
	}
	d.resized.Store(true)
	if !d.resizeEvent() {
		t.Error("the resize was not reported")
	}
	if d.resizeEvent() {
		t.Error("the same resize was reported twice")
	}
}

// TestLocaleIsUTF8 checks how the locale decides whether the input is UTF-8.
// The C code asks setlocale, which reads these variables in this order.
func TestLocaleIsUTF8(t *testing.T) {
	for _, test := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"nothing set means the C locale, which counts", map[string]string{}, true},
		{"a UTF-8 locale", map[string]string{"LC_ALL": "en_US.UTF-8"}, true},
		{"spelled without the hyphen", map[string]string{"LC_ALL": "en_US.utf8"}, true},
		{"case does not matter", map[string]string{"LC_ALL": "en_US.Utf-8"}, true},
		{"the C locale", map[string]string{"LC_ALL": "C"}, true},
		{"a single byte locale", map[string]string{"LC_ALL": "en_US.ISO-8859-1"}, false},
		{"POSIX is not the C locale", map[string]string{"LC_ALL": "POSIX"}, false},
		{"LC_ALL wins over LC_CTYPE", map[string]string{
			"LC_ALL": "en_US.ISO-8859-1", "LC_CTYPE": "en_US.UTF-8"}, false},
		{"LC_CTYPE wins over LANG", map[string]string{
			"LC_CTYPE": "en_US.UTF-8", "LANG": "en_US.ISO-8859-1"}, true},
		{"LANG is used last", map[string]string{"LANG": "en_US.UTF-8"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
				t.Setenv(name, test.env[name])
			}
			if got := localeIsUTF8(); got != test.want {
				t.Errorf("localeIsUTF8 = %v, want %v", got, test.want)
			}
		})
	}
}

// TestTTYWithoutADeviceIsHarmless checks what the terminal operations do when
// the bytes do not come from a terminal, which is how every test drives the
// decoder.
func TestTTYWithoutADeviceIsHarmless(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{})
	if term.dev != nil {
		t.Fatal("a tty built from a plain reader has a device")
	}
	if err := term.startRaw(); err != nil {
		t.Errorf("startRaw gave %v, want no error", err)
	}
	term.endRaw()
	if err := term.close(); err != nil {
		t.Errorf("close gave %v, want no error", err)
	}
	if !term.resizeEvent() {
		t.Error("resizeEvent said no, want yes when there is nothing to ask")
	}
	if term.asyncStop() {
		t.Error("asyncStop said yes, want no when there is no terminal")
	}
}

// TestSetEscDelayClamps checks that the waits stay between zero and a second,
// as they do in the C code.
func TestSetEscDelayClamps(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{})
	term.setEscDelay(-time.Second, 2*time.Second)
	if term.escInitialTimeout != 0 {
		t.Errorf("a negative wait became %v, want 0", term.escInitialTimeout)
	}
	if term.escTimeout != escDelayMax {
		t.Errorf("a wait over the limit became %v, want %v", term.escTimeout, escDelayMax)
	}
	term.setEscDelay(30*time.Millisecond, 5*time.Millisecond)
	if term.escInitialTimeout != 30*time.Millisecond || term.escTimeout != 5*time.Millisecond {
		t.Errorf("the waits are %v and %v, want 30ms and 5ms",
			term.escInitialTimeout, term.escTimeout)
	}
}
