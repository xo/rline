package rline

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const (
	// termColorCorpusPath holds what the C color reduction returned.
	termColorCorpusPath = "testdata/termcolor.txt"

	// termColorProbePath is where tools/build-probe-termcolor.sh puts the probe.
	termColorProbePath = ".build/probe-termcolor"
)

// TestTermColorMatchesC replays every recorded call to the color reduction in
// term_color.c and checks that the Go port answers the same.
func TestTermColorMatchesC(t *testing.T) {
	if *update {
		termColorRegenerate(t)
	}
	b, err := os.ReadFile(termColorCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-termcolor.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) < 1000 {
		t.Fatalf("the corpus holds %d lines, which is too few", len(lines))
	}
	counts := make(map[string]int, 8)
	bad := 0
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) < 3 {
			t.Fatalf("%s:%d: cannot read %q", termColorCorpusPath, i+1, line)
		}
		counts[f[0]]++
		var got, want string
		switch f[0] {
		case "ansi16":
			got, want = strconv.Itoa(colorToANSI16(Color(mustU32(t, f[1])))), f[2]
		case "ansi8":
			got, want = strconv.Itoa(colorToANSI8(Color(mustU32(t, f[1])))), f[2]
		case "to256":
			got, want = strconv.Itoa(rgbToANSI256(Color(mustU32(t, f[1])))), f[2]
		case "grayish":
			r, g, bl := Color(mustU32(t, f[1])).rgb()
			got, want = strconv.Itoa(boolInt(isGrayish(r, g, bl))), f[2]
		case "fmt":
			p := palette(mustInt(t, f[1]))
			color := Color(mustU32(t, f[2]))
			bg := f[3] == "1"
			got, want = escapeForCorpus(fmtColor(p, color, bg)), f[4]
		default:
			t.Fatalf("%s:%d: unknown kind of call %q", termColorCorpusPath, i+1, f[0])
		}
		if got == want {
			continue
		}
		bad++
		if bad <= 20 {
			t.Errorf("%s:%d: %s\n  got:  %s\n  want: %s", termColorCorpusPath, i+1, line, got, want)
		}
	}
	if bad > 20 {
		t.Errorf("%d differences in total, 20 shown", bad)
	}
	for _, kind := range []string{"ansi16", "ansi8", "to256", "fmt", "grayish"} {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
		}
	}
	t.Logf("checked %d calls: %v", len(lines), counts)
}

// TestPaletteBits checks how much color each palette carries.
func TestPaletteBits(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		p    palette
		want int
	}{
		{paletteMono, 1},
		{paletteANSI8, 3},
		{paletteANSI16, 4},
		{paletteANSI256, 8},
		{paletteRGB, 24},
		{palette(99), 4},
	} {
		if got := test.p.bits(); got != test.want {
			t.Errorf("palette(%d).bits() = %d, want %d", test.p, got, test.want)
		}
	}
}

// escapeForCorpus renders an escape sequence the way the C probe prints one.
func escapeForCorpus(s string) string {
	if s == "" {
		return "-"
	}
	return strings.ReplaceAll(s, "\x1b", `\e`)
}

// termColorRegenerate runs the C probe and writes the corpus.
func termColorRegenerate(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(termColorProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-termcolor.sh", termColorProbePath)
	}
	out, err := exec.Command(termColorProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(termColorCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", termColorCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", termColorCorpusPath, len(out))
}
