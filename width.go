package rline

import "github.com/mattn/go-runewidth"

// Column width of a character.
//
// isocline carries its own copy of the wcwidth function of Markus Kuhn, in
// wcwidth.c. The port does not carry that table. It calls go-runewidth
// instead, which follows a newer version of Unicode and which the author of
// this package already depends on elsewhere.
//
// The two do not agree everywhere. testdata/wcwidth.txt records the width that
// the C code gives every code point, and testdata/wcwidth-delta.txt records
// every range where go-runewidth gives a different answer. TestWidthDelta
// keeps that record current, so an upgrade of go-runewidth shows up as a
// change to a committed file rather than as a silent shift in cursor
// positions.
//
// One difference is systematic. The C function returns -1 for a character that
// a terminal cannot print, such as a control character. go-runewidth returns 0
// for those. The callers in stringbuf.c treat a negative width as an error, so
// the port must make that check where it uses this function, not here.

// runeWidth returns the number of terminal columns that r occupies. The result
// is 0, 1 or 2.
func runeWidth(r rune) int {
	return runewidth.RuneWidth(r)
}
