//go:build darwin

// What macOS does differently: the terminal device, the key names the help
// screen shows, and the mark drawn where a line wraps.

package rline

import (
	"time"

	"golang.org/x/sys/unix"
)

// --------------------------------------------------------------------------
// helpkeys_darwin.go

// The parts of the help that name a different key on macOS.
//
// The C chooses these at compile time with "#if __APPLE__", so the port
// keeps the branch rather than asking at run time. See helpkeys_other.go for
// the other side, and wrapmark_darwin.go for why a compile time choice in
// the C stays one here.

// helpPrevWord and helpNextWord are the keys that move a word at a time.
const (
	helpPrevWord = "shift-left"
	helpNextWord = "shift-right"
)

// helpNewlineRows are the rows for starting a new line in the middle of an
// input. macOS names both keys on one row, where the other systems need two.
var helpNewlineRows = []helpRow{
	{"shift-tab,^j", "create a new line for multi-line input"},
}

// helpOverviewArrows is the line of the drawing that names the word keys.
// The drawing disagrees with the table above it: it says alt where the table
// says shift. That is what the C does, and it is not worth correcting in a
// port.
const helpOverviewArrows = "         │     alt-left   │   alt-right   │\n"

// --------------------------------------------------------------------------
// ttydev_darwin.go

// The ioctl requests that read and write the terminal settings. Linux and
// macOS name them differently. The flush variant waits for output to drain
// and throws away input that has not been read, which is what the C code
// asks for with TCSAFLUSH.
const (
	termiosGet      = unix.TIOCGETA
	termiosSetFlush = unix.TIOCSETAF
)

// defaultEscInitial is how long to wait for the byte after an escape before
// deciding the user pressed the Escape key.
//
// macOS waits twice as long as Linux, because it sends an escape and then the
// key for alt and a key, so a real sequence can arrive slowly enough to look
// like a lone Escape.
const defaultEscInitial = 200 * time.Millisecond

// --------------------------------------------------------------------------
// wrapmark_darwin.go

// wrapMark is drawn at the end of a row that filled the terminal rather than
// being ended by the user.
//
// The C picks the glyph at compile time: a return symbol on macOS and a left
// arrow everywhere else. The port keeps the branch, because a recorded redraw
// holds whichever glyph the build that produced it drew.
const wrapMark = "[ic-dim]↵"

// corpusVariant names the recordings this build compares against. It is the
// branch above rather than the system, because the branch is what changes the
// bytes. See refreshCorpusPath.
const corpusVariant = "darwin"
