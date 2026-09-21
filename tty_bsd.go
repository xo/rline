//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package rline

import "golang.org/x/sys/unix"

// The ioctl requests that read and write the terminal settings, in the BSD
// spelling, which macOS uses too. See the note in tty_sysv.go about why
// these files are named for the family rather than for a system.
const (
	termiosGet      = unix.TIOCGETA
	termiosSetFlush = unix.TIOCSETAF
)
