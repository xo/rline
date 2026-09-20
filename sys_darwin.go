//go:build darwin

// What macOS does differently: the terminal device, the key names the help
// screen shows, and the mark drawn where a line wraps.

package rline

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
