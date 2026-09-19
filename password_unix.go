//go:build unix

package rline

import (
	"fmt"

	"golang.org/x/sys/unix"
)

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
func (d *ttyDevice) startNoEcho() error {
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
func (d *ttyDevice) endNoEcho() {
	d.mu.Lock()
	defer d.mu.Unlock()
	mode := d.origMode
	_ = unix.IoctlSetTermios(d.fd, termiosSetFlush, &mode)
}
