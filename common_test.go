// Helpers shared by the tests in this package: the -update flag, and reading
// the hex fields a C probe prints.

package rline

import (
	"encoding/hex"
	"flag"
	"strconv"
	"testing"
)

// maxReported caps how many differences a failing run prints.
const maxReported = 20

// update regenerates a corpus by running its C probe. It needs a compiler and
// the build script beside it, so it runs only when asked.
var update = flag.Bool("update", false, "regenerate the corpora from the C probes")

// lineAt returns the line at index i, or a marker when the text ended.
func lineAt(lines []string, i int) string {
	if i >= len(lines) {
		return "<end of text>"
	}
	return lines[i]
}

// mustHex reads a hex field. The probe writes "-" for an empty string.
func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	if s == "-" {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("reading hex %q: %v", s, err)
	}
	return b
}

func mustInt(t *testing.T, s string) int {
	t.Helper()
	v, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("reading number %q: %v", s, err)
	}
	return v
}

// hexOrDash renders bytes the way the probe does, with "-" for empty.
func hexOrDash(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	return hex.EncodeToString(b)
}
