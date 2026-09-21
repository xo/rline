//go:build (!unix || aix) && !windows

package rline

import (
	"fmt"
	"os"
)

// fileIsTerminal reports whether f is a terminal, which nothing is here:
// there is no terminal layer on this platform, so no file can be one.
func fileIsTerminal(*os.File) bool {
	return false
}

// errUnsupported is returned by a platform that has no terminal layer of its
// own. It lives here rather than beside the other error values because this
// is the only file that uses it, and a constant declared where nothing on
// the building platform reads it is one the linter reports.
const errUnsupported Error = "unsupported"

// Reading keys from a terminal is written for Linux, macOS and Windows. This
// file is what is left: a system with none of those, where nothing can read
// a key yet.
//
// It exists so that such a system still compiles and still reads plain lines.
// So every function the rest of the package calls needs an answer here, and a
// missing one is invisible to everyone: go vet for the three real targets
// passes, and nobody builds this. See the Checks section of PLAN.md.

// isTerminal reports whether fd is a terminal. Nothing can tell on this system
// yet, so it answers no.
func isTerminal(_ int) bool {
	return false
}

// openDecoder reports that this platform cannot read keys yet.
func openDecoder(_ int) (*keyDecoder, error) {
	return nil, fmt.Errorf("reading keys on this platform: %w", errUnsupported)
}
