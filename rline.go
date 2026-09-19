// Package rline is a readline package written in pure Go. A readline package
// reads a line of text from a terminal, and gives the user editing, history,
// and completion.
//
// rline is a port of isocline, a readline replacement written in C by Daan
// Leijen. isocline carries the MIT license, and this copyright notice covers
// every part of this package that derives from it:
//
//	Copyright (c) 2021, Daan Leijen
//
// The port does not use cgo. It keeps the behavior of the C code, including
// the places where that behavior departs from a standard, because recorded
// sessions from the C build are the test corpus. Each such departure carries a
// comment where the code makes it.
package rline

import "errors"

// Error values.
var (
	// ErrClosed is returned by a Reader that has been closed.
	ErrClosed = errors.New("the reader is closed")

	// ErrInterrupted is returned when the user abandoned what was being read,
	// which is Ctrl-C or Ctrl-G, and which asks for the reading to be given
	// up rather than for the input to end.
	//
	// The C has no way to say this. It clears the line and hands back an
	// empty string, so a caller cannot tell an abandoned line from Enter on
	// an empty one. A shell has to: usql resets its statement buffer on an
	// interrupt and carries on, and would otherwise run whatever Ctrl-C left
	// behind. This is the one place where the port departs from the C over a
	// behaviour rather than a fault.
	ErrInterrupted = errors.New("interrupted")
)
