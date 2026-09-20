// Tests for the attributes and for reading them out of an SGR sequence.

package ansi

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	// attrCorpusPath holds what the C functions in attr.c returned. The C
	// probe also prints the attribute buffer cases, which rline checks,
	// because the buffer stayed there.
	attrCorpusPath = "testdata/attr.txt"

	// attrProbePath is where tools/build-probe-attr.sh puts the probe. The
	// probe is built at the root of the repository, which is why this reaches
	// out of the package.
	attrProbePath = "../.build/probe-attr"
)

// attrKinds are the recorded calls this package answers. The rest of what the
// probe prints belongs to rline's attribute buffer.
var attrKinds = []string{
	"rgb", "rgbx", "ansi256", "none", "default", "fromcolor",
	"isnone", "sgr", "escsgr", "update", "iseq",
	// The buffer cases are named by two fields, because rline keeps the
	// third, "abuf app", where the buffer meets its own text buffer.
	"abuf fresh", "abuf del",
}

// attrKind names a recorded call. The buffer cases split across the two
// packages, so those are named by what they do as well.
func attrKind(f []string) string {
	if f[0] == "abuf" && len(f) > 1 {
		return f[0] + " " + f[1]
	}
	return f[0]
}

// TestAttrMatchesC replays every recorded call to a C function in attr.c that
// this package answers, and checks that the Go port answers the same.
//
// One group of cases is allowed to differ, and only one. The C deletion moves
// the wrong number of bytes, and the port does the intended thing instead.
// Those differences are written to a delta file and compared against what is
// committed. A difference anywhere else is a hard failure, so the delta file
// cannot grow to cover a mistake in the port.
func TestAttrMatchesC(t *testing.T) {
	if *update {
		attrRegenerate(t)
	}
	b, err := os.ReadFile(attrCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-attr.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	// A floor would let the corpus shrink. This test is driven by the corpus
	// rather than replaying a script against it, so a line that goes missing
	// is one case fewer checked and nothing else: measured by deleting a
	// line from the middle and watching the suite stay green. The exact
	// count is the smallest thing that notices, and changing it is a
	// deliberate edit beside the corpus it describes.
	if len(lines) != 1669 {
		t.Fatalf("ansi/testdata/attr.txt holds %d lines, want %d: a corpus that changed size was "+
			"either regenerated on purpose, in which case set this number, or "+
			"lost lines, in which case it now checks less than it says",
			len(lines), 1669)
	}
	dumps := attrBufReplay()
	counts := make(map[string]int, 16)
	var delta strings.Builder
	bad := 0
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) == 0 {
			t.Fatalf("%s:%d: empty line", attrCorpusPath, i+1)
		}
		counts[attrKind(f)]++
		got, want := attrCheck(t, f, dumps)
		if got == want {
			continue
		}
		// The deletion sequence is the one place the port departs on purpose.
		if f[0] == "abuf" && f[1] == "del" {
			fmt.Fprintf(&delta, "%s\n  c:  %s\n  go: %s\n", strings.Join(f[:3], " "), want, got)
			continue
		}
		bad++
		if bad <= 20 {
			t.Errorf("%s:%d: %s\n  got:  %s\n  want: %s",
				attrCorpusPath, i+1, strings.Join(f[:min(3, len(f))], " "), got, want)
		}
	}
	if bad > 20 {
		t.Errorf("%d differences in total, 20 shown", bad)
	}
	// Guard the corpus. A probe that stopped printing a section, or a split
	// that dropped one, would otherwise leave this passing over less.
	for _, kind := range attrKinds {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
		}
	}
	for kind := range counts {
		if !slices.Contains(attrKinds, kind) {
			t.Errorf("the corpus holds %d %s cases, which belong to rline", counts[kind], kind)
		}
	}
	attrCompareDelta(t, delta.String())
	t.Logf("checked %d calls: %v", len(lines), counts)
}

// attrCheck computes the Go answer for one recorded call, and returns it with
// the recorded one.
func attrCheck(t *testing.T, f []string, dumps map[string][]string) (string, string) {
	t.Helper()
	switch f[0] {
	case "rgb":
		return fmt.Sprintf("%08x", uint32(RGBHex(mustU32(t, f[1])))), f[2]
	case "rgbx":
		r, g, b := mustInt(t, f[1]), mustInt(t, f[2]), mustInt(t, f[3])
		return fmt.Sprintf("%08x", uint32(RGB(r, g, b))), f[4]
	case "ansi256":
		return fmt.Sprintf("%08x", uint32(FromANSI256(mustInt(t, f[1])))), f[2]
	case "none":
		return attrString(Attr{}), strings.Join(f[1:], " ")
	case "default":
		return attrString(DefaultAttr()), strings.Join(f[1:], " ")
	case "fromcolor":
		// The C has ic_attr_from_color. In Go that is a struct literal, so
		// what this checks is that setting only the color leaves the other
		// five fields saying nothing, which is what the C recorded.
		return attrString(Attr{Fg: Code(mustInt(t, f[1]))}), strings.Join(f[2:], " ")
	case "isnone":
		return fmt.Sprintf("%d %d", boolInt(Attr{}.IsZero()), boolInt(DefaultAttr().IsZero())),
			strings.Join(f[1:], " ")
	case "sgr":
		return attrString(ParseSGR(mustStr(t, f[1]))), strings.Join(f[2:], " ")
	case "escsgr":
		return attrString(ParseEscapeSGR(mustStr(t, f[1]))), strings.Join(f[2:], " ")
	case "update":
		a, b := ParseSGR(mustStr(t, f[1])), ParseSGR(mustStr(t, f[2]))
		return attrString(a.Merge(b)), strings.Join(f[3:], " ")
	case "iseq":
		a, b := ParseSGR(mustStr(t, f[1])), ParseSGR(mustStr(t, f[2]))
		return strconv.Itoa(boolInt(a == b)), f[3]
	case "abuf":
		key := f[1] + " " + f[2]
		got, ok := dumps[key]
		if !ok {
			t.Fatalf("the Go replay has no step %q", key)
		}
		return strings.Join(got, " "), strings.Join(f[3:], " ")
	}
	t.Fatalf("unknown kind of call %q", f[0])
	return "", ""
}

// attrRegenerate runs the C probe and keeps the lines this package checks. The
// probe prints rline's buffer cases too, and rline's own -update keeps those.
func attrRegenerate(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(attrProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-attr.sh", attrProbePath)
	}
	out, err := exec.Command(attrProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	var b strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		if slices.Contains(attrKinds, attrKind(strings.Fields(line))) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if err := os.WriteFile(attrCorpusPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("writing %s: %v", attrCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", attrCorpusPath, b.Len())
}

// attrString renders an attribute the way the C probe prints one.
func attrString(a Attr) string {
	return fmt.Sprintf("%08x %08x %d %d %d %d",
		uint32(a.Fg), uint32(a.Bg), a.Bold, a.Italic, a.Reverse, a.Underline)
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
