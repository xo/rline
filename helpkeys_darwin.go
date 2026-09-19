//go:build darwin

package rline

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
