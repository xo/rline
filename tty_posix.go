//go:build unix && !aix

package rline

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/xo/rline/internal/text"
	"golang.org/x/sys/unix"
)

// The terminal itself: the file descriptor that keys arrive on, the raw mode
// that lets them arrive one at a time, and the signals that say the window
// changed size or that the program is going away.
//
// Ported from isocline/src/tty.c.

// tty is a terminal opened on a file descriptor. It supplies the bytes
// that a keyDecoder decodes, and it owns the terminal settings.
type tty struct {
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

	// ended is set when a read finds that the input ended, as it does when
	// the terminal is closed. A wait that runs out does not set it.
	ended atomic.Bool

	resizeCh chan os.Signal
	stopCh   chan os.Signal
	done     chan struct{}
	stopOnce sync.Once
}

// isTerminal reports whether fd is a terminal. Reading the terminal settings
// succeeds only for one.
func isTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, termiosGet)
	return err == nil
}

// fileIsTerminal reports whether f is a terminal. On Unix this is the same
// question as isTerminal, because a terminal is reached by file descriptor
// whichever way it is being used.
func fileIsTerminal(f *os.File) bool {
	return isTerminal(int(f.Fd()))
}

// openTTY prepares fd for reading keys. A negative fd means standard
// input, as it does in the C code.
//
// This works out the raw settings but does not apply them. startRaw does
// that, so that the terminal is only in raw mode while a line is being read.
func openTTY(fd int) (*tty, error) {
	if fd < 0 {
		fd = int(os.Stdin.Fd())
	}
	mode, err := unix.IoctlGetTermios(fd, termiosGet)
	if err != nil {
		return nil, fmt.Errorf("reading the settings of file descriptor %d: %w", fd, errNotATerminal)
	}
	d := &tty{fd: fd, origMode: *mode, done: make(chan struct{})}

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

// stopSignals are the signals that mean the program is stopping. While the
// terminal is in raw mode, the terminal has to go back to how it was before
// the program stops, or the shell that started it is left with no echo and no
// line editing.
//
// isocline also catches SIGSEGV, SIGTRAP and SIGBUS. This port does not. The
// Go runtime owns those, it needs them to print a stack trace and to run its
// own checks, and taking them over would break more than a tidy terminal is
// worth.
//
// isocline catches these signals for as long as it runs, and the port did the
// same until D48. That ended the program on a Ctrl-C between two reads, even
// when the program had a handler of its own for it. usql has one to cancel a
// query, and died of the signal after the query was cancelled.
var stopSignals = []os.Signal{
	unix.SIGTERM, unix.SIGINT, unix.SIGQUIT, unix.SIGHUP,
	unix.SIGTSTP, unix.SIGTTIN, unix.SIGTTOU,
}

// childSignals are the signals that a key raises at the terminal: Ctrl-C and
// Ctrl-\. The terminal sends them to every process in the foreground, so a
// program that runs a pager gets them as well as the pager. Session.Release
// keeps them from ending the program while it has given the terminal away.
//
// Ctrl-Z is left out on purpose. It has to stop the program as well as the
// child, or the shell cannot take the terminal back.
var childSignals = []os.Signal{unix.SIGINT, unix.SIGQUIT}

// watchSignals starts listening for a window resize, and gets ready to listen
// for the signals that end the program.
func (d *tty) watchSignals() {
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

	// The signals that mean the program is stopping are watched only while
	// the terminal is in raw mode, which is the only time it has to be put
	// back before the program goes. startRaw starts the watch and endRaw
	// stops it. See stopSignals.
	d.stopCh = make(chan os.Signal, 1)
	go func() {
		for {
			var sig os.Signal
			select {
			case sig = <-d.stopCh:
			case <-d.done:
				return
			}
			// Put the terminal back, then send the signal again. endRaw
			// stops the watch first, so the second signal does what it would
			// have done without rline: it reaches the handler the program
			// set up for it, or takes the default action when there is none.
			// A program with a handler gets the signal twice.
			d.endRaw()
			if s, ok := sig.(syscall.Signal); ok {
				_ = unix.Kill(unix.Getpid(), s)
			}
		}
	}()

	d.watching.Store(true)
}

// close puts the terminal back and stops watching for signals.
func (d *tty) close() error {
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
func (d *tty) startRaw() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rawEnabled {
		return nil
	}
	if err := unix.IoctlSetTermios(d.fd, termiosSetFlush, &d.rawMode); err != nil {
		return fmt.Errorf("putting the terminal into raw mode: %w", err)
	}
	d.rawEnabled = true
	if d.watching.Load() {
		signal.Notify(d.stopCh, stopSignals...)
	}
	return nil
}

// endRaw puts the terminal back the way it was.
func (d *tty) endRaw() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.rawEnabled {
		return
	}
	// The watch stops even when the terminal cannot be put back, because
	// the signals belong to the program again once raw mode is given up.
	signal.Stop(d.stopCh)
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
func (d *tty) readByte(timeout time.Duration) (byte, bool) {
	if timeout < 0 {
		return d.readNow()
	}
	ms := max(int(timeout/time.Millisecond), 0)
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
//
// A read that returns nothing, or fails with anything but an interruption,
// means that the input ended: a terminal whose other side was closed answers
// EIO, or end of file. That is recorded, so that the edit loop can stop
// rather than read again at once, which spun on a full core when a program
// that ignores SIGHUP lost its terminal. isocline has the same fault, and its
// pull request 47 is where it was found.
func (d *tty) readNow() (byte, bool) {
	var buf [1]byte
	n, err := unix.Read(d.fd, buf[:])
	if err == nil && n == 1 {
		return buf[0], true
	}
	if !errors.Is(err, unix.EINTR) && !errors.Is(err, unix.EAGAIN) {
		d.ended.Store(true)
	}
	return 0, false
}

// inputEnded reports whether a read found that the input ended.
func (d *tty) inputEnded() bool { return d.ended.Load() }

// resizeEvent reports whether the window changed size since the last call.
//
// It answers yes when nothing is watching for the signal, because then there
// is no way to tell, and redrawing a line that did not need it costs less
// than leaving a line drawn at the wrong width.
func (d *tty) resizeEvent() bool {
	if !d.watching.Load() {
		return true
	}
	return d.resized.Swap(false)
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
	return text.ContainsFold(loc, "UTF-8") || text.ContainsFold(loc, "utf8") || text.CompareFold(loc, "C") == 0
}

// openDecoder opens the terminal on fd and returns a keyDecoder reading it.
// A negative fd means standard input.
func openDecoder(fd int) (*keyDecoder, error) {
	d, err := openTTY(fd)
	if err != nil {
		return nil, err
	}
	t := newDecoder(d)
	t.ctrl = d
	t.isUTF8 = localeIsUTF8()
	return t, nil
}
