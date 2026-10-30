//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package rline

import (
	"testing"

	"golang.org/x/sys/unix"
)

// TestOutputSpeedReadsTheSpeed checks that the BSD family reads the output
// speed itself, and not the input speed.
func TestOutputSpeedReadsTheSpeed(t *testing.T) {
	t.Parallel()
	mode := unix.Termios{Ispeed: 9600, Ospeed: 38400}
	if got := outputSpeed(&mode); got != 38400 {
		t.Errorf("the output speed is %d, want 38400", got)
	}
}
