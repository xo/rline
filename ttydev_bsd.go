//go:build freebsd || netbsd || openbsd || dragonfly

package rline

import (
	"time"

	"golang.org/x/sys/unix"
)

// The ioctl requests that read and write the terminal settings. The BSDs use
// the same names macOS does. The flush variant waits for output to drain and
// throws away input that has not been read, which is what the C code asks
// for with TCSAFLUSH.
const (
	termiosGet      = unix.TIOCGETA
	termiosSetFlush = unix.TIOCSETAF
)

// defaultEscInitial is how long to wait for the byte after an escape before
// deciding the user pressed the Escape key.
//
// This is the Linux figure rather than the macOS one. macOS waits twice as
// long because of how it sends alt and a key, and nobody has measured
// whether a BSD does the same. See the note on support in PLAN.md.
const defaultEscInitial = 100 * time.Millisecond
