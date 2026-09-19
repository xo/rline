//go:build !linux && !darwin && !windows

package rline

import "errors"

// errUnsupported says this platform cannot read keys from a terminal yet.
var errUnsupported = errors.New("reading keys is not supported on this platform")

// Reading keys from a terminal is written for Linux, macOS and Windows. This
// file is what is left: a system with none of those, where nothing can read
// a key yet.

// isATTY reports whether fd is a terminal. Nothing can tell on this system
// yet, so it answers no.
func isATTY(_ int) bool {
	return false
}

// openTTY reports that this platform cannot read keys yet.
func openTTY(_ int) (*tty, error) {
	return nil, errUnsupported
}
