//go:build !linux && !darwin && !windows

package rline

import "os"

// fileSizer reports the size of the terminal a file is connected to. On a
// system with no way to ask, the size stays at whatever it was given.
type fileSizer struct {
	f *os.File
}

// size reports that the size is unknown.
func (s fileSizer) size() (int, int, bool) {
	return 0, 0, false
}
