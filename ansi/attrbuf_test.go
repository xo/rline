// Tests for the buffer of attributes, replayed against the C recordings.

package ansi

import (
	"os"
	"strconv"
	"testing"
)

// attrDeltaPath holds the cases where the port answers differently on purpose,
// which is the deletion fault described on AttrBuf.DeleteAt.
const attrDeltaPath = "testdata/attr-delta.txt"

// attrBufReplay runs the same operation sequences that tools/probe-attr.c
// runs, and records what the buffer held after every step.
//
// The probe also appends text to a text buffer, which rline owns, so those
// steps are replayed there instead.
func attrBufReplay() map[string][]string {
	out := make(map[string][]string, 16)
	dump := func(tag string, step int, ab *AttrBuf) {
		n := ab.Length()
		row := make([]string, 0, n+1)
		row = append(row, strconv.Itoa(n))
		for _, a := range ab.Extend(n) {
			row = append(row, attrString(a))
		}
		out[tag+" "+strconv.Itoa(step)] = row
	}

	ab := &AttrBuf{}
	step := 0
	dump("fresh", step, ab)
	step++
	ab.SetAt(0, 4, ParseSGR("31"))
	dump("fresh", step, ab)
	step++
	ab.SetAt(2, 3, ParseSGR("32"))
	dump("fresh", step, ab)
	step++
	ab.UpdateAt(1, 3, ParseSGR("1"))
	dump("fresh", step, ab)
	step++
	ab.InsertAt(2, 2, ParseSGR("4"))
	dump("fresh", step, ab)
	step++
	ab.Clear()
	dump("fresh", step, ab)

	ab = &AttrBuf{}
	for i := range 24 {
		ab.SetAt(i, 1, Attr{Fg: Code(i + 1)})
	}
	step = 0
	dump("del", step, ab)
	step++
	ab.DeleteAt(4, 2)
	dump("del", step, ab)
	step++
	ab.DeleteAt(0, 1)
	dump("del", step, ab)
	step++
	ab.DeleteAt(10, 100)
	dump("del", step, ab)
	step++
	ab.DeleteAt(100, 1)
	dump("del", step, ab)
	step++
	ab.DeleteAt(0, 0)
	dump("del", step, ab)
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

// TestNilAttrBufAcceptsEveryMethod pins the contract the type mostly exists
// for: rline threads a nil *AttrBuf down ten function signatures to mean "do
// not record attributes", instead of branching at each one.
//
// Three of these guards are already load-bearing, measured by taking each out
// and watching the suite panic on a nil dereference: Length, At and the fill
// behind SetAt and UpdateAt. Extend and Clear are not reached that way today,
// so without this they would be the two places where a contract that says
// "every method" quietly stopped being true.
func TestNilAttrBufAcceptsEveryMethod(t *testing.T) {
	t.Parallel()
	var ab *AttrBuf
	if got := ab.Length(); got != 0 {
		t.Errorf("Length is %d, want 0", got)
	}
	if got := ab.At(0); got != (Attr{}) {
		t.Errorf("At gave %+v, want the zero attribute", got)
	}
	if got := ab.Extend(8); got != nil {
		t.Errorf("Extend gave %v, want nil", got)
	}
	// These record nothing and must not panic.
	ab.Clear()
	ab.SetAt(0, 4, DefaultAttr())
	ab.UpdateAt(0, 4, DefaultAttr())
	ab.InsertAt(0, 4, DefaultAttr())
	ab.DeleteAt(0, 4)
	if got := ab.Length(); got != 0 {
		t.Errorf("Length is %d after writing to a nil buffer, want 0", got)
	}
}
