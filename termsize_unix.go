//go:build unix

package rline

import (
	"os"

	"golang.org/x/sys/unix"
)

// fileSizer reports the size of the terminal a file is connected to.
//
// The size comes from the file that is written to rather than the one that is
// read from, which is what the C asks as well. A terminal that answers nothing
// leaves the size as it was.
type fileSizer struct {
	f *os.File
}

// size returns the width and height in characters.
func (s fileSizer) size() (int, int, bool) {
	if s.f == nil {
		return 0, 0, false
	}
	ws, err := unix.IoctlGetWinsize(int(s.f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, false
	}
	return int(ws.Col), int(ws.Row), true
}
