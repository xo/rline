//go:build unix && !aix

// What Linux and macOS do: reading a password with the echo turned off, and
// asking
// the terminal how wide it is.

package rline

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// --------------------------------------------------------------------------
// password_unix.go

// Turning off echo the way a password prompt does.
//
// A terminal cannot see what a program is reading, so it guesses. Ghostty and
// iTerm2 show a lock when tcsetattr turns echo off, and turning canonical mode
// off as well is the documented way to avoid that guess, which is what an
// image viewer wants and what a line editor does every time it reads a line.
//
// So reading a password in raw mode hides the text and loses the indicator,
// which is the wrong way round. This leaves canonical mode on, which is what
// getpass has always done and what golang.org/x/term does.
//
// The cost is that the terminal driver does the editing rather than this
// package: backspace and kill-line work because the driver honors them, and
// Ctrl-C raises an interrupt rather than coming back as a key.

// startNoEcho turns off echo while leaving canonical mode on.
func (d *tty) startNoEcho() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	mode := d.origMode
	mode.Lflag &^= unix.ECHO
	mode.Lflag |= unix.ICANON | unix.ISIG
	mode.Iflag |= unix.ICRNL
	if err := unix.IoctlSetTermios(d.fd, termiosSetFlush, &mode); err != nil {
		return fmt.Errorf("turning off echo: %w", err)
	}
	return nil
}

// endNoEcho puts the terminal back the way it was found.
func (d *tty) endNoEcho() {
	d.mu.Lock()
	defer d.mu.Unlock()
	mode := d.origMode
	_ = unix.IoctlSetTermios(d.fd, termiosSetFlush, &mode)
}

// --------------------------------------------------------------------------
// termsize_unix.go

// fileSizer reports the size of the terminal a file is connected to.
//
// The size comes from the file that is written to rather than the one that is
// read from, which is what the C asks as well. A terminal that answers nothing
// leaves the size as it was.
type fileSizer struct {
	f *os.File
}

// size returns the width and height in characters.
func (s fileSizer) size() (int, int, bool) {
	if s.f == nil {
		return 0, 0, false
	}
	ws, err := unix.IoctlGetWinsize(int(s.f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, false
	}
	return int(ws.Col), int(ws.Row), true
}
