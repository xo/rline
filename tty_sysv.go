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

// outputSpeed returns the output speed of a terminal in bits a second. The
// System V family keeps it as a code in the control flags, and speeds holds
// the codes. A code that is not there gives 0. Solaris keeps a speed above
// 38400 in a second field, which this does not read, so such a speed gives 0
// there.
func outputSpeed(mode *unix.Termios) int {
	return speeds[mode.Cflag&unix.CBAUD]
}

// speeds maps each code of an output speed to the speed in bits a second.
var speeds = map[uint32]int{
	unix.B50:    50,
	unix.B75:    75,
	unix.B110:   110,
	unix.B134:   134,
	unix.B150:   150,
	unix.B200:   200,
	unix.B300:   300,
	unix.B600:   600,
	unix.B1200:  1200,
	unix.B1800:  1800,
	unix.B2400:  2400,
	unix.B4800:  4800,
	unix.B9600:  9600,
	unix.B19200: 19200,
	unix.B38400: 38400,
	// Linux keeps these inside the mask, and Solaris does not.
	unix.B57600:  57600,
	unix.B115200: 115200,
	unix.B230400: 230400,
}
