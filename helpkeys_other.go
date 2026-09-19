//go:build !darwin

package rline

// The parts of the help that name a different key away from macOS. See
// helpkeys_darwin.go for why this is a compile time choice.

// helpPrevWord and helpNextWord are the keys that move a word at a time.
const (
	helpPrevWord = "^left"
	helpNextWord = "^right"
)

// helpNewlineRows are the rows for starting a new line in the middle of an
// input. Two keys do it here and they are named on two rows, the first
// without a description of its own.
var helpNewlineRows = []helpRow{
	{"^enter, ^j", ""},
	{"shift-tab", "create a new line for multi-line input"},
}

// helpOverviewArrows is the line of the drawing that names the word keys.
const helpOverviewArrows = "         │    ctrl-left   │  ctrl-right   │\n"
