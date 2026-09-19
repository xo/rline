//go:build !linux && !darwin

package rline

import "errors"

// errUnsupported says this platform cannot read keys from a terminal yet.
var errUnsupported = errors.New("reading keys is not supported on this platform")

// Reading keys from a terminal is written for Linux and macOS only. Windows
// has no termios and no pseudo-terminal device file. It needs the console
// API, which reads key events rather than bytes, so tty.c pushes escape
// sequences back into its own buffer there and decodes those.

// isATTY reports whether fd is a terminal. Nothing can tell on this system
// yet, so it answers no.
func isATTY(_ int) bool {
	return false
}

// openTTY reports that this platform cannot read keys yet.
func openTTY(_ int) (*tty, error) {
	return nil, errUnsupported
}
