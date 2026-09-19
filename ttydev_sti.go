//go:build (linux || darwin || freebsd || netbsd || dragonfly) && !aix

package rline

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/xo/rline/key"
)

// Stopping a read from another goroutine, on the systems that have the ioctl
// for it.
//
// The split here is by the ioctl rather than by the system, because that is
// what actually differs: OpenBSD removed TIOCSTI, and Solaris does not offer
// it through this interface. Both take the other file, which answers false.

// asyncStop makes a read that is waiting for a key return, by putting a
// ctrl+c into the input of the terminal.
//
// This needs the TIOCSTI request, which recent systems refuse: Linux hides it
// behind a build option that distributions turn off, and macOS allows it only
// to a privileged process. It answers false when the system refuses, which is
// what the C code does on a system that has no such request at all.
func (d *ttyDevice) asyncStop() bool {
	c := byte(key.CtrlC)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(d.fd),
		uintptr(unix.TIOCSTI), uintptr(unsafe.Pointer(&c)))
	return errno == 0
}
