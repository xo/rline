//go:build windows

package rline

import (
	"os"

	"golang.org/x/sys/windows"
)

// fileSizer reports the size of the console a file is connected to.
type fileSizer struct {
	f *os.File
}

// size returns the width and height in characters.
//
// The window is measured rather than the buffer. A console buffer is often
// taller than the window showing it, and what matters is what can be seen.
func (s fileSizer) size() (int, int, bool) {
	if s.f == nil {
		return 0, 0, false
	}
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(s.f.Fd()), &info); err != nil {
		return 0, 0, false
	}
	cols := int(info.Window.Right) - int(info.Window.Left) + 1
	rows := int(info.Window.Bottom) - int(info.Window.Top) + 1
	if cols <= 0 || rows <= 0 {
		return 0, 0, false
	}
	return cols, rows, true
}
