// Package capture records terminal sessions from a program that runs under a
// pseudo-terminal. A pseudo-terminal is a pair of file descriptors that acts
// as a terminal for another program.
//
// A recording is a Transcript, which pairs each chunk of input with everything
// the program wrote in answer. Transcripts are stored as golden files. A
// golden file holds the expected output of a test. The C implementation of
// isocline produces the first transcripts, and the Go port must reproduce them
// byte for byte.
//
// Only Linux records sessions today. On every other platform Record returns
// ErrUnsupported. macOS needs a pseudo-terminal opened with posix_openpt.
// Windows has no pseudo-terminal device file, so it needs a pseudo console,
// which it creates with CreatePseudoConsole.
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

// Key sequences that a terminal sends. These name the bytes that the sessions
// send, so that a session reads as a description of what a person types.
const (
	KeyEnter     = "\r"
	KeyTab       = "\t"
	KeyShiftTab  = "\x1b[Z"
	KeyEscape    = "\x1b"
	KeyBackspace = "\x7f"
	KeyUp        = "\x1b[A"
	KeyDown      = "\x1b[B"
	KeyRight     = "\x1b[C"
	KeyLeft      = "\x1b[D"
	KeyHome      = "\x1b[H"
	KeyEnd       = "\x1b[F"
	KeyF1        = "\x1bOP"

	CtrlA = "\x01" // start of line
	CtrlD = "\x04" // end of input
	CtrlE = "\x05" // end of line
	CtrlJ = "\n"   // insert a line break
	CtrlK = "\x0b" // delete to end of line
	CtrlR = "\x12" // search the history
	CtrlU = "\x15" // delete to start of line
	CtrlW = "\x17" // delete the word before the cursor
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

	// About says what the session exercises. It is written into the
	// transcript, so that a reader knows why the recording exists.
	About string

	// Terminal settings. These stay fixed so that the output does not change
	// between runs.
	Term string
	Cols int
	Rows int

	// Steps are the input chunks, sent in order. Record always collects the
	// output of the program before the first step, so a session does not need
	// an empty step to wait for a banner.
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

// Transcript is a recorded session.
type Transcript struct {
	// The settings that produced the recording.
	Session string
	About   string
	Term    string
	Cols    int
	Rows    int

	// Exchanges are the input chunks and their answers, in order. The first
	// exchange sends nothing and holds whatever the program wrote at startup.
	Exchanges []Exchange
}

// Exchange is one chunk of input and everything the program wrote in answer.
type Exchange struct {
	// Send holds the bytes that went to the program.
	Send []byte

	// Recv holds the bytes that the program wrote before it went quiet.
	Recv []byte
}

// Bytes returns every byte that the program wrote, in order.
func (t *Transcript) Bytes() []byte {
	var out []byte
	for _, e := range t.Exchanges {
		out = append(out, e.Recv...)
	}
	return out
}

// withDefaults returns a copy of s with every zero limit filled in.
//
// Only Record calls this, and Record records on Linux and macOS alone, so a
// build for any other system has no caller for it. The unused linter is told
// so here rather than moving the method, because the Windows implementation
// will call it too.
//
//nolint:unused // used by Record on the systems that can record
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
