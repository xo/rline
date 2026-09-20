// Helpers shared by the tests in this package: the -update flag, and reading
// the hex fields a C probe prints.

package rline

import (
	"encoding/hex"
	"flag"
	"strconv"
	"strings"
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

// fieldReader walks the fields of a recorded line in order. Several kinds of
// line carry a list whose length is given by the field in front of it, so
// reading them by position is easier to get wrong than reading them in turn.
type fieldReader struct {
	t *testing.T
	f []string
	i int
}

// next returns the next field as it was written.
func (p *fieldReader) next() string {
	p.t.Helper()
	if p.i >= len(p.f) {
		p.t.Fatalf("the line ran out after %d fields: %s", len(p.f), strings.Join(p.f, " "))
	}
	s := p.f[p.i]
	p.i++
	return s
}

// str returns the next field as the bytes it stands for.
func (p *fieldReader) str() string {
	p.t.Helper()
	return string(mustHex(p.t, p.next()))
}

// num returns the next field as a number.
func (p *fieldReader) num() int {
	p.t.Helper()
	return mustInt(p.t, p.next())
}

// flag returns the next field as a yes or no.
func (p *fieldReader) flag() bool {
	p.t.Helper()
	return p.num() == 1
}

// list returns a list of strings, led by how many there are.
func (p *fieldReader) list() []string {
	p.t.Helper()
	n := p.num()
	out := make([]string, n)
	for i := range n {
		out[i] = p.str()
	}
	return out
}

// rest returns every field that is left, as written.
func (p *fieldReader) rest() []string {
	return p.f[p.i:]
}

// rawList returns a list of fields as they were written, led by how many
// there are. Some lists hold fields that join several values with a colon,
// which are not hexadecimal on their own.
func (p *fieldReader) rawList() []string {
	p.t.Helper()
	n := p.num()
	out := make([]string, n)
	for i := range n {
		out[i] = p.next()
	}
	return out
}

// nullableStr reads a field that the probe may have written as a null
// pointer, which becomes an empty string here.
func (p *fieldReader) nullableStr() string {
	p.t.Helper()
	s := p.next()
	if s == "!" {
		return ""
	}
	if s == "-" {
		return ""
	}
	return string(mustHex(p.t, s))
}

// envStr reads a field that names an environment variable's value, where a
// null pointer means the variable was not set at all.
func (p *fieldReader) envStr() string {
	p.t.Helper()
	s := p.next()
	if s == "!" {
		return "\x00unset"
	}
	if s == "-" {
		return ""
	}
	return string(mustHex(p.t, s))
}
