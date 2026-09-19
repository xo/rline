//go:build darwin

package rline

// wrapMark is drawn at the end of a row that filled the terminal rather than
// being ended by the user.
//
// The C picks the glyph at compile time: a return symbol on macOS and a left
// arrow everywhere else. The port keeps the branch, because the recorded
// sessions are kept per system and each one holds the glyph its own C build
// produced.
const wrapMark = "[ic-dim]↵"
