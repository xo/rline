package rline

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// widthRange is one run of code points that share a column width.
type widthRange struct {
	// Lo and Hi are the first and last code point of the run.
	Lo rune
	Hi rune

	// Width is what the C code returns for every code point in the run. It is
	// -1 for a character that a terminal cannot print.
	Width int
}

// TestWidthDelta records where go-runewidth disagrees with the wcwidth table
// that isocline carries.
//
// The test does not demand agreement, because the port uses go-runewidth on
// purpose and go-runewidth follows a newer version of Unicode. It demands that
// the disagreement stay the one that is written down. An upgrade of
// go-runewidth then shows up as a change to a committed file, rather than as a
// silent shift in where the cursor lands.
func TestWidthDelta(t *testing.T) {
	cRanges := readWidthRanges(t, widthPath)
	if len(cRanges) < 100 {
		t.Fatalf("%s holds %d ranges, which is too few", widthPath, len(cRanges))
	}
	if got := cRanges[len(cRanges)-1].Hi; got != 0x10FFFF {
		t.Fatalf("%s stops at %06x, want 10ffff", widthPath, got)
	}
	var (
		sb      strings.Builder
		open    bool
		lo      rune
		cw, gw  int
		differs int
	)
	flush := func(hi rune) {
		if open {
			fmt.Fprintf(&sb, "delta %06x %06x c=%d go=%d\n", lo, hi, cw, gw)
			open = false
		}
	}
	for _, r := range cRanges {
		for u := r.Lo; u <= r.Hi; u++ {
			g := runeWidth(u)
			if g == r.Width {
				flush(u - 1)
				continue
			}
			differs++
			if open && cw == r.Width && gw == g {
				continue
			}
			flush(u - 1)
			open, lo, cw, gw = true, u, r.Width, g
		}
	}
	flush(0x10FFFF)
	got := sb.String()

	if *update {
		if err := os.WriteFile(deltaPath, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", deltaPath, err)
		}
		t.Logf("wrote %s: %d code points differ", deltaPath, differs)
		return
	}
	want, err := os.ReadFile(deltaPath)
	if err != nil {
		t.Fatalf("reading %s: %v (run go test -update)", deltaPath, err)
	}
	if got == string(want) {
		t.Logf("%d code points differ, in %d ranges", differs, strings.Count(got, "\n"))
		return
	}
	a, b := strings.Split(got, "\n"), strings.Split(string(want), "\n")
	for i := range max(len(a), len(b)) {
		x, y := lineAt(a, i), lineAt(b, i)
		if x != y {
			t.Fatalf("the difference against go-runewidth changed.\n"+
				"%s line %d\n  now:      %s\n  recorded: %s\n"+
				"If go-runewidth was upgraded, check what moved, then run go test -update.",
				deltaPath, i+1, x, y)
		}
	}
}

// TestWidthIsSane checks the few widths that no Unicode version changes.
func TestWidthIsSane(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		r    rune
		want int
	}{
		{"space", ' ', 1},
		{"letter", 'A', 1},
		{"combining acute", 0x0301, 0},
		{"zero width space", 0x200B, 0},
		{"han", '日', 2},
		{"hangul", '한', 2},
		{"fullwidth A", 0xFF21, 2},
		{"halfwidth katakana", 0xFF66, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := runeWidth(test.r); got != test.want {
				t.Errorf("runeWidth(%#x) = %d, want %d", test.r, got, test.want)
			}
		})
	}
}

// readWidthRanges reads the ranges that the C probe printed.
func readWidthRanges(t *testing.T, path string) []widthRange {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v (run tools/build-probe.sh, then go test -update)", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	out := make([]widthRange, 0, len(lines))
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) != 4 || f[0] != "width" {
			t.Fatalf("%s:%d: cannot read %q", path, i+1, line)
		}
		lo, err := strconv.ParseUint(f[1], 16, 32)
		if err != nil {
			t.Fatalf("%s:%d: %v", path, i+1, err)
		}
		hi, err := strconv.ParseUint(f[2], 16, 32)
		if err != nil {
			t.Fatalf("%s:%d: %v", path, i+1, err)
		}
		w, err := strconv.Atoi(f[3])
		if err != nil {
			t.Fatalf("%s:%d: %v", path, i+1, err)
		}
		out = append(out, widthRange{Lo: rune(lo), Hi: rune(hi), Width: w})
	}
	return out
}

// lineAt returns the line at index i, or a marker when the text ended.
func lineAt(lines []string, i int) string {
	if i >= len(lines) {
		return "<end of text>"
	}
	return lines[i]
}
