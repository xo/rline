//go:build linux || solaris

package rline

import "golang.org/x/sys/unix"

// The ioctl requests that read and write the terminal settings, in the System
// V spelling. The flush variant waits for output to drain and throws away
// input that has not been read, which is what the C code asks for with
// TCSAFLUSH.
//
// This file is named for the axis it splits on rather than for a system on
// it. The names come in two families and nothing else about a system decides
// which it uses, so a file per system would have said TCGETS twice and hidden
// that linux and solaris agree here by family rather than by coincidence.
const (
	termiosGet      = unix.TCGETS
	termiosSetFlush = unix.TCSETSF
)
