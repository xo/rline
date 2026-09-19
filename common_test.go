package rline

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// update regenerates the corpus by running the C probe. It needs a compiler
// and tools/build-probe.sh, so it runs only when asked.
var update = flag.Bool("update", false, "regenerate testdata/common.txt from the C probe")

const (
	// corpusPath holds what the C functions in common.c returned.
	corpusPath = "testdata/common.txt"

	// widthPath holds the column width that the C code gives every code point.
	widthPath = "testdata/wcwidth.txt"

	// deltaPath holds every range where go-runewidth disagrees with the C code.
	deltaPath = "testdata/wcwidth-delta.txt"

	// probePath is where tools/build-probe.sh puts the compiled probe.
	probePath = ".build/probe-common"

	// maxReported caps how many differences a failing run prints.
	maxReported = 20
)

// TestPortMatchesC replays every recorded call to a C function in common.c and
// checks that the Go port returns the same answer.
func TestPortMatchesC(t *testing.T) {
	if *update {
		regenerate(t)
	}
	b, err := os.ReadFile(corpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) < 1000 {
		t.Fatalf("the corpus holds %d lines, which is too few to prove anything", len(lines))
	}
	counts := make(map[string]int, 16)
	bad := 0
	for i, line := range lines {
		kind, diff := checkLine(t, line)
		counts[kind]++
		if diff == "" {
			continue
		}
		bad++
		if bad <= maxReported {
			t.Errorf("%s:%d: %s\n  %s", corpusPath, i+1, diff, line)
		}
	}
	if bad > maxReported {
		t.Errorf("%d differences in total, %d shown", bad, maxReported)
	}
	// Guard the corpus itself. A probe that stops emitting a section would
	// otherwise make this test pass while checking nothing.
	for _, kind := range []string{
		"decode", "encode", "tolower", "stricmp", "strnicmp",
		"istarts", "starts", "icontains", "contains",
	} {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
		}
	}
	t.Logf("checked %d calls: %v", len(lines), counts)
}

// checkLine checks one recorded call. It returns the kind of call, and a
// description of the difference, or an empty string when the port agrees.
func checkLine(t *testing.T, line string) (string, string) {
	t.Helper()
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", "empty line"
	}
	switch f[0] {
	case "decode":
		in, count, want := mustHex(t, f[1]), mustInt(t, f[2]), mustUint(t, f[3])
		got, n := decodeRune(in)
		if uint32(got) != want || n != count {
			return f[0], fmt.Sprintf("decodeRune gave %08x count %d, want %08x count %d",
				uint32(got), n, want, count)
		}
	case "encode":
		r, want := rune(mustUint(t, f[1])), mustHex(t, f[2])
		got := make([]byte, 5)
		copy(got, appendRune(nil, r))
		if !equalBytes(got, want) {
			return f[0], fmt.Sprintf("appendRune gave %x, want %x", got, want)
		}
	case "tolower":
		in, want := mustHex(t, f[1]), mustHex(t, f[2])
		if got := asciiLower(in[0]); got != want[0] {
			return f[0], fmt.Sprintf("asciiLower gave %02x, want %02x", got, want[0])
		}
	case "stricmp":
		a, b, want := mustStr(t, f[1]), mustStr(t, f[2]), mustInt(t, f[3])
		if got := compareFold(a, b); got != want {
			return f[0], fmt.Sprintf("compareFold(%q, %q) = %d, want %d", a, b, got, want)
		}
	case "strnicmp":
		a, b := mustStr(t, f[1]), mustStr(t, f[2])
		n, want := mustInt(t, f[3]), mustInt(t, f[4])
		if got := compareFoldN(a, b, n); got != want {
			return f[0], fmt.Sprintf("compareFoldN(%q, %q, %d) = %d, want %d", a, b, n, got, want)
		}
	case "istarts":
		a, b, want := mustStr(t, f[1]), mustStr(t, f[2]), mustInt(t, f[3]) == 1
		if got := hasPrefixFold(a, b); got != want {
			return f[0], fmt.Sprintf("hasPrefixFold(%q, %q) = %v, want %v", a, b, got, want)
		}
	case "starts":
		a, b, want := mustStr(t, f[1]), mustStr(t, f[2]), mustInt(t, f[3]) == 1
		if got := strings.HasPrefix(a, b); got != want {
			return f[0], fmt.Sprintf("strings.HasPrefix(%q, %q) = %v, want %v", a, b, got, want)
		}
	case "icontains":
		a, b, want := mustStr(t, f[1]), mustStr(t, f[2]), mustInt(t, f[3]) == 1
		if got := containsFold(a, b); got != want {
			return f[0], fmt.Sprintf("containsFold(%q, %q) = %v, want %v", a, b, got, want)
		}
	case "contains":
		a, b, want := mustStr(t, f[1]), mustStr(t, f[2]), mustInt(t, f[3]) == 1
		if got := strings.Contains(a, b); got != want {
			return f[0], fmt.Sprintf("strings.Contains(%q, %q) = %v, want %v", a, b, got, want)
		}
	default:
		return f[0], "unknown kind of call"
	}
	return f[0], ""
}

// regenerate runs the C probe and writes the two corpus files. The probe
// prints both, so splitting them here keeps one build step.
func regenerate(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(probePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe.sh", probePath)
	}
	out, err := exec.Command(probePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	var calls, widths strings.Builder
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if strings.HasPrefix(line, "width ") {
			widths.WriteString(line)
			widths.WriteByte('\n')
			continue
		}
		calls.WriteString(line)
		calls.WriteByte('\n')
	}
	for _, f := range []struct {
		path string
		body string
	}{
		{corpusPath, calls.String()},
		{widthPath, widths.String()},
	} {
		if err := os.WriteFile(f.path, []byte(f.body), 0o644); err != nil {
			t.Fatalf("writing %s: %v", f.path, err)
		}
		t.Logf("wrote %s: %d bytes", f.path, len(f.body))
	}
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

// mustStr reads a hex field as a string.
func mustStr(t *testing.T, s string) string {
	t.Helper()
	return string(mustHex(t, s))
}

func mustInt(t *testing.T, s string) int {
	t.Helper()
	v, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("reading number %q: %v", s, err)
	}
	return v
}

func mustUint(t *testing.T, s string) uint32 {
	t.Helper()
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		t.Fatalf("reading hex number %q: %v", s, err)
	}
	return uint32(v)
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
