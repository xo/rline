//go:build !darwin

// What every system except macOS does: the key names the help screen shows,
// and the mark drawn where a line wraps.

package rline

// --------------------------------------------------------------------------
// helpkeys_other.go

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

// --------------------------------------------------------------------------
// wrapmark_other.go

// wrapMark is drawn at the end of a row that filled the terminal rather than
// being ended by the user. See wrapmark_darwin.go for why this is a compile
// time choice rather than a runtime one.
const wrapMark = "[ic-dim]←"

// corpusVariant names the recordings this build compares against. Every system
// but macOS draws the same glyph, so they all share one set.
//
// "default" means two systems agreeing rather than one system's answer. The
// set is recorded on Linux and Windows compares against it, and Windows has
// been shown to match all 1584 redraws. That holds because the port draws the
// same bytes everywhere: it has no console emulation path of the kind term.c
// compiles on Windows, and assumes a console that reads escape sequences. If
// that ever stops being true, Windows needs a variant of its own, and the
// failure will be loud rather than quiet, because a Windows build comparing
// against a Linux recording is what would break.
const corpusVariant = "default"
