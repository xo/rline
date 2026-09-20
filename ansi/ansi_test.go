// Tests for the palette, the reduction, and the bridge to image/color.

package ansi

import (
	"flag"
	"image/color"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// update regenerates the corpus by running the C probe. It needs a compiler
// and tools/build-probe-termcolor.sh, so it runs only when asked.
var update = flag.Bool("update", false, "regenerate testdata/termcolor.txt from the C probe")

const (
	// termColorCorpusPath holds what the C color reduction returned.
	termColorCorpusPath = "testdata/termcolor.txt"

	// termColorProbePath is where tools/build-probe-termcolor.sh puts the probe.
	termColorProbePath = "../.build/probe-termcolor"
)

// TestReductionMatchesC replays every recorded call to the color reduction in
// term_color.c and checks that the Go port answers the same.
func TestReductionMatchesC(t *testing.T) {
	if *update {
		termColorRegenerate(t)
	}
	b, err := os.ReadFile(termColorCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-termcolor.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	// A floor would let the corpus shrink. This test is driven by the corpus
	// rather than replaying a script against it, so a line that goes missing
	// is one case fewer checked and nothing else: measured by deleting a
	// line from the middle and watching the suite stay green. The exact
	// count is the smallest thing that notices, and changing it is a
	// deliberate edit beside the corpus it describes.
	if len(lines) != 5522 {
		t.Fatalf("ansi/testdata/termcolor.txt holds %d lines, want %d: a corpus that changed size was "+
			"either regenerated on purpose, in which case set this number, or "+
			"lost lines, in which case it now checks less than it says",
			len(lines), 5522)
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
			got, want = strconv.Itoa(toANSI16(Code(mustU32(t, f[1])))), f[2]
		case "ansi8":
			got, want = strconv.Itoa(toANSI8(Code(mustU32(t, f[1])))), f[2]
		case "to256":
			got, want = strconv.Itoa(toANSI256(Code(mustU32(t, f[1])))), f[2]
		case "grayish":
			r, g, bl := Code(mustU32(t, f[1])).rgb()
			got, want = strconv.Itoa(boolInt(isGrayish(r, g, bl))), f[2]
		case "fmt":
			p := Palette(mustInt(t, f[1]))
			c := Code(mustU32(t, f[2]))
			bg := f[3] == "1"
			got, want = escapeForCorpus(Format(p, c, bg)), f[4]
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
		p    Palette
		want int
	}{
		{PaletteMono, 1},
		{PaletteANSI8, 3},
		{PaletteANSI16, 4},
		{PaletteANSI256, 8},
		{PaletteTrueColor, 24},
		{Palette(99), 4},
	} {
		if got := test.p.Bits(); got != test.want {
			t.Errorf("Palette(%d).Bits() = %d, want %d", test.p, got, test.want)
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

// ----------------------------------------------------------------------------
// Bridging to image/color

// TestCodeSatisfiesImageColor checks the two ways a Code meets the standard
// library: answering RGBA, and being made from anything that does.
func TestCodeSatisfiesImageColor(t *testing.T) {
	t.Parallel()

	t.Run("an RGB color answers its own components", func(t *testing.T) {
		t.Parallel()
		// RGBA reports each channel in the range 0 to 0xFFFF, so a byte of
		// 0xFF becomes 0xFFFF and 0x00 stays 0.
		r, g, b, a := RGB(0x12, 0x34, 0x56).RGBA()
		for _, test := range []struct {
			name string
			got  uint32
			want uint32
		}{
			{"red", r, 0x1212},
			{"green", g, 0x3434},
			{"blue", b, 0x5656},
			{"alpha", a, 0xFFFF},
		} {
			if test.got != test.want {
				t.Errorf("%s is %#04x, want %#04x", test.name, test.got, test.want)
			}
		}
	})

	t.Run("no color is transparent", func(t *testing.T) {
		t.Parallel()
		// None and Default both mean the terminal decides, and
		// transparent black is the only honest answer to that.
		for _, c := range []Code{None, Default} {
			if r, g, b, a := c.RGBA(); r|g|b|a != 0 {
				t.Errorf("%v gave %v %v %v %v, want all zero", c, r, g, b, a)
			}
		}
	})

	t.Run("a palette color answers the usual table", func(t *testing.T) {
		t.Parallel()
		// Not the terminal's theme, which is not knowable here, but the
		// table the 256 color palette uses.
		for _, c := range []Code{Black, Maroon, Silver, Gray, Red, White} {
			if _, _, _, a := c.RGBA(); a != 0xFFFF {
				t.Errorf("%v is not opaque", c)
			}
		}
		if r, _, _, _ := Maroon.RGBA(); r == 0 {
			t.Error("dark red has no red in it")
		}
	})

	t.Run("FromColor takes a standard color", func(t *testing.T) {
		t.Parallel()
		// color.RGBA is the ordinary one a caller will have.
		got := FromColor(color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xFF})
		if want := RGB(0x12, 0x34, 0x56); got != want {
			t.Errorf("FromColor gave %#08x, want %#08x", uint32(got), uint32(want))
		}
	})

	t.Run("FromColor drops the alpha rather than the color", func(t *testing.T) {
		t.Parallel()
		// A terminal has no transparency. A fully transparent color comes
		// out black, which is what color.RGBA's premultiplied channels say,
		// rather than coming out invisible.
		if got := FromColor(color.RGBA{}); got != RGB(0, 0, 0) {
			t.Errorf("a transparent color gave %#08x, want black", uint32(got))
		}
	})

	t.Run("a round trip through RGBA keeps the color", func(t *testing.T) {
		t.Parallel()
		for _, want := range []Code{RGB(0, 0, 0), RGB(255, 255, 255), RGBHex(0xFF8800)} {
			if got := FromColor(want); got != want {
				t.Errorf("%#08x came back as %#08x", uint32(want), uint32(got))
			}
		}
	})
}

// boolInt renders a bool the way the C probe prints one.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// mustInt reads a decimal field.
func mustInt(t *testing.T, s string) int {
	t.Helper()
	v, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("reading number %q: %v", s, err)
	}
	return v
}

// mustU32 reads a hex field.
func mustU32(t *testing.T, s string) uint32 {
	t.Helper()
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		t.Fatalf("reading hex number %q: %v", s, err)
	}
	return uint32(v)
}
