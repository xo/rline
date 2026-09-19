package rline

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const (
	// attrCorpusPath holds what the C functions in attr.c returned.
	attrCorpusPath = "testdata/attr.txt"

	// attrDeltaPath holds the cases where the port answers differently on
	// purpose, which is the deletion fault described on attrBuf.deleteAt.
	attrDeltaPath = "testdata/attr-delta.txt"

	// attrProbePath is where tools/build-probe-attr.sh puts the probe.
	attrProbePath = ".build/probe-attr"
)

// TestAttrPortMatchesC replays every recorded call to a C function in attr.c
// and checks that the Go port answers the same.
//
// One group of cases is allowed to differ, and only one. The C deletion moves
// the wrong number of bytes, and the port does the intended thing instead.
// Those differences are written to a delta file and compared against what is
// committed. A difference anywhere else is a hard failure, so the delta file
// cannot grow to cover a mistake in the port.
func TestAttrPortMatchesC(t *testing.T) {
	if *update {
		attrRegenerate(t)
	}
	b, err := os.ReadFile(attrCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-attr.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) < 500 {
		t.Fatalf("the corpus holds %d lines, which is too few", len(lines))
	}
	dumps := attrReplay()
	counts := make(map[string]int, 16)
	var delta strings.Builder
	bad := 0
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) == 0 {
			t.Fatalf("%s:%d: empty line", attrCorpusPath, i+1)
		}
		counts[f[0]]++
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
			t.Errorf("%s:%d: %s\n  got:  %s\n  want: %s", attrCorpusPath, i+1, strings.Join(f[:min(3, len(f))], " "), got, want)
		}
	}
	if bad > 20 {
		t.Errorf("%d differences in total, 20 shown", bad)
	}
	for _, kind := range []string{
		"rgb", "rgbx", "ansi256", "none", "default", "fromcolor",
		"sgr", "escsgr", "update", "iseq", "abuf", "append",
	} {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
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
		return fmt.Sprintf("%08x", uint32(RGB(mustU32(t, f[1])))), f[2]
	case "rgbx":
		r, g, b := mustInt(t, f[1]), mustInt(t, f[2]), mustInt(t, f[3])
		return fmt.Sprintf("%08x", uint32(RGBX(r, g, b))), f[4]
	case "ansi256":
		return fmt.Sprintf("%08x", uint32(colorFromANSI256(mustInt(t, f[1])))), f[2]
	case "none":
		return attrString(attr{}), strings.Join(f[1:], " ")
	case "default":
		return attrString(attrDefault()), strings.Join(f[1:], " ")
	case "fromcolor":
		return attrString(attrFromColor(Color(mustInt(t, f[1])))), strings.Join(f[2:], " ")
	case "isnone":
		return fmt.Sprintf("%d %d", boolInt(attr{}.isNone()), boolInt(attrDefault().isNone())),
			strings.Join(f[1:], " ")
	case "sgr":
		return attrString(attrFromSGR(mustStr(t, f[1]))), strings.Join(f[2:], " ")
	case "escsgr":
		return attrString(attrFromEscSGR(mustStr(t, f[1]))), strings.Join(f[2:], " ")
	case "update":
		a, b := attrFromSGR(mustStr(t, f[1])), attrFromSGR(mustStr(t, f[2]))
		return attrString(a.updateWith(b)), strings.Join(f[3:], " ")
	case "iseq":
		a, b := attrFromSGR(mustStr(t, f[1])), attrFromSGR(mustStr(t, f[2]))
		return strconv.Itoa(boolInt(a == b)), f[3]
	case "abuf":
		key := f[1] + " " + f[2]
		got, ok := dumps[key]
		if !ok {
			t.Fatalf("the Go replay has no step %q", key)
		}
		return strings.Join(got, " "), strings.Join(f[3:], " ")
	case "append":
		key := "append " + f[1]
		got, ok := dumps[key]
		if !ok {
			t.Fatalf("the Go replay has no step %q", key)
		}
		return got[0], f[2]
	case "appendstr":
		got := dumps["appendstr"]
		return hex.EncodeToString([]byte(got[0])), f[1]
	}
	t.Fatalf("unknown kind of call %q", f[0])
	return "", ""
}

// attrReplay runs the same operation sequences that tools/probe-attr.c runs,
// and records what the buffer held after every step.
func attrReplay() map[string][]string {
	out := make(map[string][]string, 32)
	dump := func(tag string, step int, ab *attrBuf) {
		n := ab.length()
		row := make([]string, 0, n+1)
		row = append(row, strconv.Itoa(n))
		for _, a := range ab.slice(n) {
			row = append(row, attrString(a))
		}
		out[tag+" "+strconv.Itoa(step)] = row
	}
	sgr := attrFromSGR

	ab := &attrBuf{}
	step := 0
	dump("fresh", step, ab)
	step++
	ab.setAt(0, 4, sgr("31"))
	dump("fresh", step, ab)
	step++
	ab.setAt(2, 3, sgr("32"))
	dump("fresh", step, ab)
	step++
	ab.updateAt(1, 3, sgr("1"))
	dump("fresh", step, ab)
	step++
	ab.insertAt(2, 2, sgr("4"))
	dump("fresh", step, ab)
	step++
	ab.clear()
	dump("fresh", step, ab)

	ab = &attrBuf{}
	for i := range 24 {
		ab.setAt(i, 1, attrFromColor(Color(i+1)))
	}
	step = 0
	dump("del", step, ab)
	step++
	ab.deleteAt(4, 2)
	dump("del", step, ab)
	step++
	ab.deleteAt(0, 1)
	dump("del", step, ab)
	step++
	ab.deleteAt(10, 100)
	dump("del", step, ab)
	step++
	ab.deleteAt(100, 1)
	dump("del", step, ab)
	step++
	ab.deleteAt(0, 0)
	dump("del", step, ab)

	ab = &attrBuf{}
	sb := &buffer{}
	for i, c := range []struct {
		s string
		a string
		n bool
	}{
		{"abc", "31", true},
		{"de", "1", true},
		{"", "32", true},
		{"fg", "32", false},
	} {
		target := ab
		if !c.n {
			target = nil
		}
		out["append "+strconv.Itoa(i)] = []string{strconv.Itoa(target.appendTo(sb, c.s, sgr(c.a)))}
		dump("app", i, ab)
	}
	out["appendstr"] = []string{sb.string()}
	return out
}

// attrCompareDelta checks the recorded departures against what is committed.
func attrCompareDelta(t *testing.T, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(attrDeltaPath, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", attrDeltaPath, err)
		}
		t.Logf("wrote %s: %d bytes", attrDeltaPath, len(got))
		return
	}
	want, err := os.ReadFile(attrDeltaPath)
	if err != nil {
		t.Fatalf("reading %s: %v (run go test -update)", attrDeltaPath, err)
	}
	if got != string(want) {
		t.Errorf("the departures from the C code changed.\nSee %s.\n got %d bytes, want %d",
			attrDeltaPath, len(got), len(want))
	}
}

// attrRegenerate runs the C probe and writes the corpus.
func attrRegenerate(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(attrProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-attr.sh", attrProbePath)
	}
	out, err := exec.Command(attrProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(attrCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", attrCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", attrCorpusPath, len(out))
}

// attrString renders an attribute the way the C probe prints one.
func attrString(a attr) string {
	return fmt.Sprintf("%08x %08x %d %d %d %d",
		uint32(a.color), uint32(a.bgColor), a.bold, a.italic, a.reverse, a.underline)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func mustU32(t *testing.T, s string) uint32 {
	t.Helper()
	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		t.Fatalf("reading hex %q: %v", s, err)
	}
	return uint32(v)
}
