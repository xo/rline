//go:build linux || darwin

package rline

import (
	"testing"
	"time"

	"github.com/xo/rline/internal/capture"
	"github.com/xo/rline/key"
	"golang.org/x/sys/unix"
)

// The one device test that needs a real terminal rather than a pipe.
//
// It is here rather than in tty_test.go because it needs capture.OpenPTY,
// which is written for Linux and macOS only. The other eleven device tests
// use pipes and environment variables, so they run on every Unix the package
// builds for. Splitting them is what gives FreeBSD and its relatives real
// coverage of the terminal layer without anyone writing pseudo-terminal code
// for a system they cannot run.

// TestTTYOnARealTerminal drives the whole terminal path against a
// pseudo-terminal: opening it, going into raw mode, reading keys through the
// decoder, and putting the terminal back.
//
// Nothing else covers raw mode. A pipe is not a terminal, so the unit tests
// above stop at the point where the settings are read.
func TestTTYOnARealTerminal(t *testing.T) {
	leader, follower, err := capture.OpenPTY()
	if err != nil {
		t.Skipf("no pseudo-terminal available: %v", err)
	}
	t.Cleanup(func() {
		_ = leader.Close()
		_ = follower.Close()
	})
	fd := int(follower.Fd())
	if !isTerminal(fd) {
		t.Fatal("the follower side of a pseudo-terminal is not a terminal")
	}

	term, err := openDecoder(fd)
	if err != nil {
		t.Fatalf("opening the terminal: %v", err)
	}
	t.Cleanup(func() { _ = term.close() })

	before, err := unix.IoctlGetTermios(fd, termiosGet)
	if err != nil {
		t.Fatalf("reading the terminal settings: %v", err)
	}
	if before.Lflag&unix.ECHO == 0 {
		t.Fatal("echo was already off before raw mode")
	}
	if err := term.startRaw(); err != nil {
		t.Fatalf("starting raw mode: %v", err)
	}
	during, err := unix.IoctlGetTermios(fd, termiosGet)
	if err != nil {
		t.Fatalf("reading the terminal settings in raw mode: %v", err)
	}
	// These are tested one at a time rather than in a loop, because the width
	// of Lflag differs by system and an untyped constant fits either one
	// while a slice of them would have to pick a type.
	if during.Lflag&unix.ECHO != 0 {
		t.Error("raw mode left echo on")
	}
	if during.Lflag&unix.ICANON != 0 {
		t.Error("raw mode left the terminal reading a line at a time")
	}
	if during.Lflag&unix.ISIG != 0 {
		t.Error("raw mode left ctrl+c raising a signal rather than arriving as a key")
	}
	if during.Lflag&unix.IEXTEN != 0 {
		t.Error("raw mode left extended input handling on")
	}

	// A key typed at the leader side arrives at the follower side, and the
	// decoder turns the bytes into one key.
	if _, err := leader.WriteString("a\x1b[A"); err != nil {
		t.Fatalf("writing to the terminal: %v", err)
	}
	for _, want := range []key.Code{'a', key.Up} {
		got, ok := term.readTimeout(2 * time.Second)
		if !ok {
			t.Fatalf("no key arrived where %08x was sent", want)
		}
		if got != want {
			t.Errorf("read %08x, want %08x", got, want)
		}
	}

	term.endRaw()
	after, err := unix.IoctlGetTermios(fd, termiosGet)
	if err != nil {
		t.Fatalf("reading the terminal settings after raw mode: %v", err)
	}
	if after.Lflag != before.Lflag || after.Iflag != before.Iflag || after.Cflag != before.Cflag {
		t.Error("the terminal was not put back the way it was")
	}
}

// TestRawModeDiscardsWhatWasTypedBeforeIt checks that entering raw mode
// throws away input that was already waiting, which is what termiosSetFlush
// is for: TCSETSF and TIOCSETAF set the attributes and empty the input queue,
// where TCSETS and TIOCSETA set them and leave it alone.
//
// Nothing else covers the difference. Found by ken-mba, who changed
// TIOCSETAF to TIOCSETA on macOS and watched the whole suite pass; the same
// change to TCSETSF here passes it too. The promise matters because a
// keystroke typed before the prompt was drawn would otherwise be read as
// though it had been typed at the prompt.
func TestRawModeDiscardsWhatWasTypedBeforeIt(t *testing.T) {
	leader, follower, err := capture.OpenPTY()
	if err != nil {
		t.Skipf("no pseudo-terminal available: %v", err)
	}
	t.Cleanup(func() {
		_ = leader.Close()
		_ = follower.Close()
	})

	term, err := openDecoder(int(follower.Fd()))
	if err != nil {
		t.Fatalf("opening the terminal: %v", err)
	}
	t.Cleanup(func() { _ = term.close() })

	if _, err := leader.WriteString("abc\n"); err != nil {
		t.Fatalf("writing to the terminal: %v", err)
	}

	// Waiting for the echo rather than sleeping. The terminal is still in
	// canonical mode with echo on, so the line discipline sends back what it
	// accepted, and that arriving is what proves the bytes reached the input
	// queue. Without it this test could pass by flushing nothing, which is
	// the shape that has cost this port time before. A byte-count ioctl
	// would say so directly, but only Linux spells one that x/sys exports.
	echoed := make(chan struct{})
	go func() {
		defer close(echoed)
		_, _ = leader.Read(make([]byte, 64))
	}()
	select {
	case <-echoed:
	case <-time.After(2 * time.Second):
		t.Fatal("the terminal never echoed what was typed, so nothing was waiting to be flushed and this test checked nothing")
	}

	if err := term.startRaw(); err != nil {
		t.Fatalf("starting raw mode: %v", err)
	}
	defer term.endRaw()

	if got, ok := term.readTimeout(200 * time.Millisecond); ok {
		t.Errorf("raw mode kept %08x, which was typed before it started", got)
	}

	// And the terminal is still readable, so the silence above is a flushed
	// queue rather than a terminal that stopped delivering.
	if _, err := leader.WriteString("z"); err != nil {
		t.Fatalf("writing to the terminal: %v", err)
	}
	got, ok := term.readTimeout(2 * time.Second)
	if !ok {
		t.Fatal("nothing arrived after raw mode started")
	}
	if got != key.Code('z') {
		t.Errorf("read %08x after raw mode started, want %08x", got, key.Code('z'))
	}
}
