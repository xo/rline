//go:build linux || solaris

package rline

import (
	"testing"

	"golang.org/x/sys/unix"
)

// TestOutputSpeedReadsTheControlFlags checks that the System V family reads
// the output speed from its code in the control flags, among the other flags
// there, and that a code it does not know gives 0.
func TestOutputSpeedReadsTheControlFlags(t *testing.T) {
	t.Parallel()
	for code, want := range map[uint32]int{unix.B38400: 38400, unix.B9600: 9600, unix.B0: 0} {
		mode := unix.Termios{Cflag: code | unix.CS8 | unix.CREAD}
		if got := outputSpeed(&mode); got != want {
			t.Errorf("the code %#x gave %d, want %d", code, got, want)
		}
	}
}
