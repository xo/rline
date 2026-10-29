//go:build linux || darwin

package rline

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
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

// TestSizeReadsTheTerminal checks that Size gives the size of the terminal
// that the Session writes to, and gives the new size after a change.
func TestSizeReadsTheTerminal(t *testing.T) {
	leader, follower, err := capture.OpenPTY()
	if err != nil {
		t.Skipf("no pseudo-terminal available: %v", err)
	}
	t.Cleanup(func() {
		_ = leader.Close()
		_ = follower.Close()
	})
	s, err := New(WithStdin(follower), WithStdout(follower))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for _, want := range []unix.Winsize{{Col: 100, Row: 30}, {Col: 123, Row: 41}} {
		if err := unix.IoctlSetWinsize(int(leader.Fd()), unix.TIOCSWINSZ, &want); err != nil {
			t.Fatalf("setting the size of the terminal: %v", err)
		}
		if cols, rows := s.Size(); cols != int(want.Col) || rows != int(want.Row) {
			t.Errorf("Size gave %d by %d, want %d by %d", cols, rows, want.Col, want.Row)
		}
	}
}

// signalChildEnv names the case that TestSignalChild runs, in a process of
// its own. A signal that ends the program would end the tests too, so each
// case runs in a child process and the parent reads how it ended.
const signalChildEnv = "RLINE_SIGNAL_CHILD"

// TestSignalsReachTheProgram checks what happens to a program that holds a
// Session when it gets a signal. Each case runs in a child process that has a
// pseudo-terminal for its keyboard and its output. The parent reads how the
// child ended, and how the child left the terminal.
func TestSignalsReachTheProgram(t *testing.T) {
	for _, tc := range []struct {
		name string
		// sig is the signal that is to end the child, or 0 when the child is
		// to exit by itself.
		sig syscall.Signal
	}{
		// Between reads a signal is the program's. Its own handler gets the
		// signal, and the program goes on. Until D48, rline sent the signal
		// again with the program's handler taken away, so the program died.
		{name: "between reads with a handler"},
		// With no handler, the signal does what it always does.
		{name: "between reads without a handler", sig: unix.SIGINT},
		// In raw mode rline puts the terminal back, and then the signal
		// ends the program.
		{name: "in raw mode", sig: unix.SIGTERM},
		// Or reaches the program's handler, when it has one.
		{name: "in raw mode with a handler"},
		// While another program has the terminal, the keys that raise a
		// signal are that program's.
		{name: "during release"},
		// After Release, the signal is the program's again.
		{name: "after release", sig: unix.SIGINT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leader, follower, err := capture.OpenPTY()
			if err != nil {
				t.Skipf("no pseudo-terminal available: %v", err)
			}
			t.Cleanup(func() {
				_ = leader.Close()
				_ = follower.Close()
			})
			before, err := unix.IoctlGetTermios(int(follower.Fd()), termiosGet)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestSignalChild$")
			cmd.Env = append(os.Environ(), signalChildEnv+"="+tc.name)
			cmd.ExtraFiles = []*os.File{follower}
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			err = cmd.Run()
			var exit *exec.ExitError
			switch {
			case err == nil && tc.sig != 0:
				t.Errorf("the child exited by itself, want it ended by %v\n%s", tc.sig, out.String())
			case err == nil:
			case !errors.As(err, &exit):
				t.Fatalf("running the child: %v", err)
			default:
				ws, ok := exit.Sys().(syscall.WaitStatus)
				switch {
				case !ok:
					t.Fatalf("no wait status for the child: %v", err)
				case !ws.Signaled():
					t.Errorf("the child exited with status %d, want it ended by %v\n%s", ws.ExitStatus(), tc.sig, out.String())
				case ws.Signal() != tc.sig:
					t.Errorf("the child was ended by %v, want %v\n%s", ws.Signal(), tc.sig, out.String())
				}
			}
			after, err := unix.IoctlGetTermios(int(follower.Fd()), termiosGet)
			if err != nil {
				t.Fatal(err)
			}
			if after.Lflag != before.Lflag || after.Iflag != before.Iflag {
				t.Error("the child left the terminal changed")
			}
		})
	}
}

// TestSignalChild is the child process of TestSignalsReachTheProgram. It
// does nothing unless that test started it.
func TestSignalChild(t *testing.T) {
	name := os.Getenv(signalChildEnv)
	if name == "" {
		t.Skip("run only as the child process of TestSignalsReachTheProgram")
	}
	tty := os.NewFile(3, "terminal")
	s, err := New(WithStdin(tty), WithStdout(tty))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Interactive() {
		t.Fatal("the Session did not open the terminal, so no signal was watched")
	}
	// The cases between reads start with a read that ended, because the end
	// of a read is where rline gives the signals back.
	read := func() {
		if err := s.env.keys.startRaw(); err != nil {
			t.Fatal(err)
		}
		s.env.keys.endRaw()
	}
	handle := func() chan os.Signal {
		ch := make(chan os.Signal, 2)
		signal.Notify(ch, unix.SIGINT)
		return ch
	}
	handled := func(ch chan os.Signal) {
		select {
		case <-ch:
		default:
			t.Fatal("the program's handler did not get the signal")
		}
	}
	raise := func(sig syscall.Signal) {
		if err := unix.Kill(unix.Getpid(), sig); err != nil {
			t.Fatal(err)
		}
		// Long enough for rline's watcher to act, if it is going to.
		time.Sleep(300 * time.Millisecond)
	}
	switch name {
	case "between reads with a handler":
		read()
		ch := handle()
		raise(unix.SIGINT)
		handled(ch)
	case "between reads without a handler":
		read()
		raise(unix.SIGINT)
	case "after release":
		if err := s.Release(func(*os.File) error { return nil }); err != nil {
			t.Fatal(err)
		}
		raise(unix.SIGINT)
	case "in raw mode":
		if err := s.env.keys.startRaw(); err != nil {
			t.Fatal(err)
		}
		raise(unix.SIGTERM)
	case "in raw mode with a handler":
		ch := handle()
		if err := s.env.keys.startRaw(); err != nil {
			t.Fatal(err)
		}
		raise(unix.SIGINT)
		handled(ch)
		// The terminal is checked by the parent, after the exit.
	case "during release":
		err := s.Release(func(*os.File) error {
			raise(unix.SIGINT)
			raise(unix.SIGQUIT)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("no case called %q", name)
	}
	os.Exit(0)
}

// TestReleaseReadsTheSizeAgain checks that a Session knows the size of the
// terminal after Release, without a resize signal, because the program that
// had the terminal can have changed it.
func TestReleaseReadsTheSizeAgain(t *testing.T) {
	leader, follower, err := capture.OpenPTY()
	if err != nil {
		t.Skipf("no pseudo-terminal available: %v", err)
	}
	t.Cleanup(func() {
		_ = leader.Close()
		_ = follower.Close()
	})
	resize := func(cols, rows uint16) error {
		return unix.IoctlSetWinsize(int(leader.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: cols, Row: rows})
	}
	if err := resize(100, 30); err != nil {
		t.Fatal(err)
	}
	s, err := New(WithStdin(follower), WithStdout(follower))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Release(func(*os.File) error { return resize(90, 20) }); err != nil {
		t.Fatal(err)
	}
	if got := s.env.term.width; got != 90 {
		t.Errorf("after Release the editor takes the terminal to be %d wide, want 90", got)
	}
}
