// Package capture records terminal sessions from a program that runs under a
// pseudo-terminal. A pseudo-terminal is a pair of file descriptors that acts
// as a terminal for another program.
//
// The recordings are golden files. A golden file holds the expected output of
// a test. The C implementation of isocline produces the first recording, and
// the Go port must reproduce it byte for byte.
//
// Only Linux records sessions today. On every other platform Record returns
// ErrUnsupported. macOS and Windows come later, and Windows needs a different
// mechanism, because Windows has no pseudo-terminal device file.
package capture

import (
	"errors"
	"time"
)

// Error values.
var (
	// ErrNoSteps is returned when a session carries no input.
	ErrNoSteps = errors.New("session has no steps")

	// ErrUnsupported is returned by Record on a platform that cannot record.
	ErrUnsupported = errors.New("recording is not supported on this platform")
)

// Default limits, used when a Session leaves them at zero.
const (
	DefaultQuiet = 200 * time.Millisecond
	DefaultLimit = 10 * time.Second
	DefaultTerm  = "xterm-256color"
	DefaultCols  = 80
	DefaultRows  = 24
)

// Session describes one terminal session to record.
type Session struct {
	// Name identifies the session and names its golden file.
	Name string

	// Terminal settings. These stay fixed so that the output does not change
	// between runs.
	Term string
	Cols int
	Rows int

	// Steps are the input chunks, sent in order.
	Steps []Step

	// Quiet is how long the program must write nothing before the next step.
	// Limit is the longest a recording can run.
	Quiet time.Duration
	Limit time.Duration
}

// Step is one chunk of input, and the quiet period that follows it.
type Step struct {
	// Send holds the bytes to write to the terminal.
	Send string

	// Wait overrides the quiet period of the session for this step.
	Wait time.Duration
}

// withDefaults returns a copy of s with every zero limit filled in.
func (s Session) withDefaults() Session {
	if s.Term == "" {
		s.Term = DefaultTerm
	}
	if s.Cols == 0 {
		s.Cols = DefaultCols
	}
	if s.Rows == 0 {
		s.Rows = DefaultRows
	}
	if s.Quiet == 0 {
		s.Quiet = DefaultQuiet
	}
	if s.Limit == 0 {
		s.Limit = DefaultLimit
	}
	return s
}
