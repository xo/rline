//go:build !linux

package capture

import "context"

// Record reports that this platform cannot record a session yet.
//
// macOS needs a pseudo-terminal opened with posix_openpt, and Windows needs a
// pseudo console, which it creates with CreatePseudoConsole.
func Record(_ context.Context, _ string, _ Session) (*Transcript, error) {
	return nil, ErrUnsupported
}
