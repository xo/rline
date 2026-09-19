//go:build linux || darwin

package rline

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/xo/rline/key"
	"golang.org/x/sys/unix"
)

// The terminal itself: the file descriptor that keys arrive on, the raw mode
// that lets them arrive one at a time, and the signals that say the window
// changed size or that the program is going away.
//
// Ported from isocline/src/tty.c.

// terminatingSignals are the signals that mean the program is stopping. The
// terminal has to go back to how it was before the program dies, or the shell
// that started it is left with no echo and no line editing.
//
// isocline also catches SIGSEGV, SIGTRAP and SIGBUS. This port does not. The
// Go runtime owns those, it needs them to print a stack trace and to run its
// own checks, and taking them over would break more than a tidy terminal is
// worth.
var terminatingSignals = []os.Signal{
	unix.SIGTERM, unix.SIGINT, unix.SIGQUIT, unix.SIGHUP,
	unix.SIGTSTP, unix.SIGTTIN, unix.SIGTTOU,
}

// ttyDevice is a terminal opened on a file descriptor. It supplies the bytes
// that a tty decodes, and it owns the terminal settings.
type ttyDevice struct {
	// fd is the file descriptor that input arrives on.
	fd int

	// origMode is how the terminal was set up before this program touched
	// it, and rawMode is how it is set up while reading keys.
	origMode unix.Termios
	rawMode  unix.Termios

	// mu guards rawEnabled and the changes to the terminal settings, because
	// the signal watcher restores them from its own goroutine.
	mu         sync.Mutex
	rawEnabled bool

	// resized is set when the window changes size, and read and cleared by
	// resizeEvent.
	resized atomic.Bool

	// watching says whether the signal watchers are installed. Until they
	// are, a resize cannot be detected and every check has to assume one
	// happened.
	watching atomic.Bool

	resizeCh chan os.Signal
	stopCh   chan os.Signal
	done     chan struct{}
	stopOnce sync.Once
}

// isATTY reports whether fd is a terminal. Reading the terminal settings
// succeeds only for one.
func isATTY(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, termiosGet)
	return err == nil
}

// fileIsTerminal reports whether f is a terminal. On Unix this is the same
// question as isATTY, because a terminal is reached by file descriptor
// whichever way it is being used.
func fileIsTerminal(f *os.File) bool {
	return isATTY(int(f.Fd()))
}

// openTTYDevice prepares fd for reading keys. A negative fd means standard
// input, as it does in the C code.
//
// This works out the raw settings but does not apply them. startRaw does
// that, so that the terminal is only in raw mode while a line is being read.
func openTTYDevice(fd int) (*ttyDevice, error) {
	if fd < 0 {
		fd = int(os.Stdin.Fd())
	}
	mode, err := unix.IoctlGetTermios(fd, termiosGet)
	if err != nil {
		return nil, fmt.Errorf("reading the settings of file descriptor %d: %w", fd, errNotATerminal)
	}
	d := &ttyDevice{fd: fd, origMode: *mode, done: make(chan struct{})}

	// Raw mode, following the termios manual page.
	raw := *mode
	// Input: no break signal, no carriage return to newline, no parity
	// check, no stripping to seven bits, no flow control.
	raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	// Control: allow all eight bits.
	raw.Cflag |= unix.CS8
	// Local: no echo, no line at a time, no extended input handling, and no
	// signals for ctrl+c and ctrl+z, which are keys here.
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	// One byte at a time, with no delay.
	raw.Cc[unix.VTIME] = 0
	raw.Cc[unix.VMIN] = 1
	d.rawMode = raw

	d.watchSignals()
	return d, nil
}

// watchSignals starts listening for a window resize and for the signals that
// end the program.
func (d *ttyDevice) watchSignals() {
	d.resizeCh = make(chan os.Signal, 1)
	signal.Notify(d.resizeCh, unix.SIGWINCH)
	go func() {
		for {
			select {
			case <-d.resizeCh:
				d.resized.Store(true)
			case <-d.done:
				return
			}
		}
	}()

	d.stopCh = make(chan os.Signal, 1)
	signal.Notify(d.stopCh, terminatingSignals...)
	go func() {
		var sig os.Signal
		select {
		case sig = <-d.stopCh:
		case <-d.done:
			return
		}
		// Put the terminal back before the program goes away, then let the
		// signal do what it would have done. Stopping the watch first means
		// the second delivery reaches the default handler.
		d.endRaw()
		signal.Stop(d.stopCh)
		if s, ok := sig.(syscall.Signal); ok {
			signal.Reset(s)
			_ = unix.Kill(unix.Getpid(), s)
		}
	}()

	d.watching.Store(true)
}

// close puts the terminal back and stops watching for signals.
func (d *ttyDevice) close() error {
	d.endRaw()
	d.stopOnce.Do(func() {
		signal.Stop(d.resizeCh)
		signal.Stop(d.stopCh)
		close(d.done)
		d.watching.Store(false)
	})
	return nil
}

// startRaw puts the terminal into raw mode, so that a key arrives as soon as
// it is pressed rather than at the end of a line. Calling it twice is safe.
func (d *ttyDevice) startRaw() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rawEnabled {
		return nil
	}
	if err := unix.IoctlSetTermios(d.fd, termiosSetFlush, &d.rawMode); err != nil {
		return fmt.Errorf("putting the terminal into raw mode: %w", err)
	}
	d.rawEnabled = true
	return nil
}

// endRaw puts the terminal back the way it was.
func (d *ttyDevice) endRaw() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.rawEnabled {
		return
	}
	if err := unix.IoctlSetTermios(d.fd, termiosSetFlush, &d.origMode); err != nil {
		return
	}
	d.rawEnabled = false
}

// readByte returns the next byte from the terminal. It reports false when
// none arrived before timeout. A negative timeout waits for as long as it
// takes.
//
// A byte that does not arrive leaves nothing behind, which is what the escape
// decoder needs: it keeps the byte it already had and reads that as alt and
// that character.
func (d *ttyDevice) readByte(timeout time.Duration) (byte, bool) {
	if timeout < 0 {
		return d.readNow()
	}
	ms := int(timeout / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	fds := []unix.PollFd{{Fd: int32(d.fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, ms)
	if err != nil || n < 1 {
		// An interrupted wait counts as nothing arriving. A resize signal
		// interrupts it, and the edit loop looks for that separately.
		return 0, false
	}
	return d.readNow()
}

// readNow reads one byte, waiting for it if the terminal is set to wait.
func (d *ttyDevice) readNow() (byte, bool) {
	var buf [1]byte
	n, err := unix.Read(d.fd, buf[:])
	if err != nil || n != 1 {
		return 0, false
	}
	return buf[0], true
}

// resizeEvent reports whether the window changed size since the last call.
//
// It answers yes when nothing is watching for the signal, because then there
// is no way to tell, and redrawing a line that did not need it costs less
// than leaving a line drawn at the wrong width.
func (d *ttyDevice) resizeEvent() bool {
	if !d.watching.Load() {
		return true
	}
	return d.resized.Swap(false)
}

// asyncStop makes a read that is waiting for a key return, by putting a
// ctrl+c into the input of the terminal.
//
// This needs the TIOCSTI request, which recent systems refuse: Linux hides it
// behind a build option that distributions turn off, and macOS allows it only
// to a privileged process. It answers false when the system refuses, which is
// what the C code does on a system that has no such request at all.
func (d *ttyDevice) asyncStop() bool {
	c := byte(key.CtrlC)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(d.fd),
		uintptr(unix.TIOCSTI), uintptr(unsafe.Pointer(&c)))
	return errno == 0
}

// localeIsUTF8 reports whether the locale says the input is UTF-8.
//
// The C code calls setlocale, which Go has no equivalent for, so this reads
// the same environment variables that setlocale reads, in the same order, and
// applies the same test. An environment that names none of them leaves the
// locale at "C", which the C code counts as UTF-8.
func localeIsUTF8() bool {
	loc := "C"
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(name); v != "" {
			loc = v
			break
		}
	}
	return containsFold(loc, "UTF-8") || containsFold(loc, "utf8") || compareFold(loc, "C") == 0
}

// openTTY opens the terminal on fd and returns a tty that reads keys from it.
// A negative fd means standard input.
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
