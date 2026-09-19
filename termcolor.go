package rline

import "fmt"

// Reducing a color to what a terminal accepts.
//
// A terminal may understand 24 bit color, or a 256 entry palette, or only the
// 8 or 16 codes that ANSI defines. When it understands less than the color
// asks for, the color has to be matched to the nearest one the terminal has.
//
// Ported from isocline/src/term_color.c.

// csi is the control sequence introducer, which starts an escape sequence.
const csi = "\x1b["

// palette says how much color a terminal understands.
type palette int

// palette values, from least to most capable.
const (
	paletteMono    palette = iota // no color at all
	paletteANSI8                  // the 8 basic codes, 30 to 37
	paletteANSI16                 // the basic codes and the bright ones, 90 to 97
	paletteANSI256                // a 256 entry palette
	paletteRGB                    // 24 bit color
)

// bits returns how many bits of color the palette carries. The edit loop uses
// this to decide how much of a highlight it can show.
func (p palette) bits() int {
	switch p {
	case paletteMono:
		return 1
	case paletteANSI8:
		return 3
	case paletteANSI16:
		return 4
	case paletteANSI256:
		return 8
	case paletteRGB:
		return 24
	}
	return 4
}

// isGrayish reports whether the three components are close enough to each
// other to read as a shade of gray.
func isGrayish(r, g, b int) bool {
	return abs(r-g) <= 4 && abs((r+g)/2-b) <= 4
}

// abs returns the absolute value of i.
func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

// rgbDistance approximates the perceived distance between two colors.
//
// This is a weighted euclidean distance whose weights shift with how much red
// is in the color, from <https://www.compuphase.com/cmetric.htm>. The square
// root is left out, because only the smallest distance matters, and the whole
// thing is multiplied up to keep precision. The arithmetic needs 28 signed
// bits, and the widest value it reaches is far inside an int32.
func rgbDistance(paletteColor uint32, r2, g2, b2 int) int32 {
	r1, g1, b1 := RGB(paletteColor).rgb()
	rmean := int32(r1+r2) / 2
	dr2 := sqr(int32(r1 - r2))
	dg2 := sqr(int32(g1 - g2))
	db2 := sqr(int32(b1 - b2))
	return (512+rmean)*dr2 + 1024*dg2 + (767-rmean)*db2
}

// sqr returns x squared.
func sqr(x int32) int32 {
	return x * x
}

// rgbMatch returns the index of the entry in the palette closest to color,
// looking only at the entries from start up to but not including end.
//
// The C keeps a sixteen entry cache in front of this, one per palette. The
// answer is the same either way, because the match depends on nothing but the
// palette and the color, so the port leaves the cache out.
func rgbMatch(table *[256]uint32, start, end int, color Color) int {
	r, g, b := color.rgb()
	gray := isGrayish(r, g, b)
	best := start
	// The C starts from the largest int32 divided by four, which leaves room
	// for the penalty below to multiply a distance without overflowing.
	bestDist := int32(2147483647) / 4
	for i := start; i < end; i++ {
		dist := rgbDistance(table[i], r, g, b)
		pr, pg, pb := RGB(table[i]).rgb()
		if isGrayish(pr, pg, pb) != gray {
			// Swapping a gray for a color, or the other way, is worse than the
			// raw distance suggests. With few colors to choose from there is
			// less room to be fussy, so the penalty is heavier.
			if end-start <= 16 {
				dist *= 4
			} else {
				dist = (dist / 4) * 5
			}
		}
		if dist < bestDist {
			best, bestDist = i, dist
		}
	}
	return best
}

// rgbToANSI256 returns the palette index closest to an RGB color.
//
// The search skips the first sixteen entries, because a terminal theme is free
// to draw those however it likes, so they are not reliable targets.
func rgbToANSI256(color Color) int {
	return rgbMatch(&ansi256, 16, 256, color)
}

// colorToANSI16 returns the ANSI code closest to color, from 30 to 37 and 90
// to 97. A color that is already a palette code passes through.
func colorToANSI16(color Color) int {
	if !color.isRGB() {
		return int(color)
	}
	c := rgbMatch(&ansi256, 0, 16, color)
	if c < 8 {
		return 30 + c
	}
	return 90 + c - 8
}

// colorToANSI8 returns the ANSI code closest to color, for a terminal that has
// only the eight basic colors and makes the bright ones with bold.
func colorToANSI8(color Color) int {
	if !color.isRGB() {
		return int(color)
	}
	c := 30 + rgbMatch(&ansi256, 0, 8, color)
	r, g, b := color.rgb()
	if r >= 196 || g >= 196 || b >= 196 {
		c += 60
	}
	return c
}

// fmtColorANSI8 returns the escape sequence for a terminal with eight colors.
// A bright color becomes bold plus the matching dim code.
func fmtColorANSI8(color Color, bg bool) string {
	c := colorToANSI8(color)
	if bg {
		c += 10
	}
	if c >= 90 {
		return fmt.Sprintf("%s1;%dm", csi, c-60)
	}
	return fmt.Sprintf("%s22;%dm", csi, c)
}

// fmtColorANSI16 returns the escape sequence for a terminal with sixteen
// colors.
func fmtColorANSI16(color Color, bg bool) string {
	c := colorToANSI16(color)
	if bg {
		c += 10
	}
	return fmt.Sprintf("%s%dm", csi, c)
}

// fmtColorANSI256 returns the escape sequence for a terminal with a 256 entry
// palette.
func fmtColorANSI256(color Color, bg bool) string {
	if !color.isRGB() {
		return fmtColorANSI16(color, bg)
	}
	return fmt.Sprintf("%s%d;5;%dm", csi, selector(bg), rgbToANSI256(color))
}

// fmtColorRGB returns the escape sequence for a terminal with 24 bit color.
func fmtColorRGB(color Color, bg bool) string {
	if !color.isRGB() {
		return fmtColorANSI16(color, bg)
	}
	r, g, b := color.rgb()
	return fmt.Sprintf("%s%d;2;%d;%d;%dm", csi, selector(bg), r, g, b)
}

// selector returns the SGR command that introduces an extended color, which is
// 38 for the text and 48 for the background.
func selector(bg bool) int {
	if bg {
		return 48
	}
	return 38
}

// fmtColor returns the escape sequence that sets color on a terminal with the
// given palette, or an empty string when there is nothing to set.
//
// The C leaves its buffer untouched in that last case, and one of its two
// callers passes a buffer it never initialized, so the C writes whatever the
// stack held. There is nothing to reproduce in that, and the port writes
// nothing.
func fmtColor(p palette, color Color, bg bool) string {
	switch {
	case color == ColorNone || p == paletteMono:
		return ""
	case p == paletteANSI8:
		return fmtColorANSI8(color, bg)
	case !color.isRGB() || p == paletteANSI16:
		return fmtColorANSI16(color, bg)
	case p == paletteANSI256:
		return fmtColorANSI256(color, bg)
	}
	return fmtColorRGB(color, bg)
}
