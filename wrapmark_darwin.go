//go:build darwin

package rline

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
