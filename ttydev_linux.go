//go:build linux

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
	termiosGet      = unix.TCGETS
	termiosSetFlush = unix.TCSETSF
)

// defaultEscInitial is how long to wait for the byte after an escape before
// deciding the user pressed the Escape key.
const defaultEscInitial = 100 * time.Millisecond
