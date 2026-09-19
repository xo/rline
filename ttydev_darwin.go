//go:build darwin

package rline

import (
	"time"

	"golang.org/x/sys/unix"
)

// The ioctl requests that read and write the terminal settings. Linux and
// macOS name them differently. The flush variant waits for output to drain
// and throws away input that has not been read, which is what the C code
// asks for with TCSAFLUSH.
const (
	termiosGet      = unix.TIOCGETA
	termiosSetFlush = unix.TIOCSETAF
)

// defaultEscInitial is how long to wait for the byte after an escape before
// deciding the user pressed the Escape key.
//
// macOS waits twice as long as Linux, because it sends an escape and then the
// key for alt and a key, so a real sequence can arrive slowly enough to look
// like a lone Escape.
const defaultEscInitial = 200 * time.Millisecond
