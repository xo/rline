//go:build !linux && !darwin && !windows

package rline

// errUnsupported is returned by a platform that has no terminal layer of its
// own. It lives here rather than beside the other error values because this
// is the only file that uses it, and a constant declared where nothing on
// the building platform reads it is one the linter reports.
const errUnsupported Error = "reading keys is not supported on this platform"

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
