//go:build openbsd || solaris

package rline

// Stopping a read from another goroutine, on the systems that have no ioctl
// for it. See ttydev_sti.go for the other half.

// asyncStop reports that a waiting read cannot be made to return.
//
// OpenBSD removed TIOCSTI, and Solaris does not offer it here, so there is no
// way to put a byte into the input of the terminal. Answering false is what
// the C code does on a system with no such request, and the caller falls back
// to waiting for the read to end on its own.
func (d *ttyDevice) asyncStop() bool {
	return false
}
