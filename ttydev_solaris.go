//go:build solaris

package rline

import (
	"time"

	"golang.org/x/sys/unix"
)

// The ioctl requests that read and write the terminal settings. Solaris and
// illumos use the same names Linux does. The flush variant waits for output
// to drain and throws away input that has not been read, which is what the C
// code asks for with TCSAFLUSH.
const (
	termiosGet      = unix.TCGETS
	termiosSetFlush = unix.TCSETSF
)

// defaultEscInitial is how long to wait for the byte after an escape before
// deciding the user pressed the Escape key. See the note in ttydev_bsd.go
// about why this is the Linux figure.
const defaultEscInitial = 100 * time.Millisecond
