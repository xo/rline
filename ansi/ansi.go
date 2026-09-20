// Package ansi holds colors as a terminal understands them, and reduces one
// to what the terminal at hand can actually show.
//
// A [Code] is either a palette code, such as [Black], or a 24 bit RGB value
// made with [RGB] or [RGBHex]. A [Palette] says how much color a terminal
// understands, and [Format] turns a Code into the escape sequence that
// terminal will accept, matching it to the nearest color it has when the
// terminal understands less than the color asks for.
//
// An [Attr] is a color, a background color, and four switches such as bold.
// Each switch is three valued, so that [Attr.Merge] can lay one set over
// another and leave alone whatever the top set says nothing about. An
// [AttrBuf] holds one Attr per byte of some text.
//
// A color or an attribute can be read out of text: [ParseSGR] from an escape
// sequence, [ParseColor] from a name or a hex value, [ParseANSI256] from a
// palette index. Each reads the way the C's sscanf does, quirks and all.
//
// Which palette a terminal has is not decided here. That is a question about
// the environment rather than about color, so the caller works it out and
// passes the answer in. Neither is drawing: this package says what bytes mean
// a color, and nothing about when to write them.
//
// Ported from isocline/src/term_color.c, attr.c and common.h.
package ansi

import (
	"fmt"
	"image/color"
)

// Code is a text color: either a palette code or a 24 bit RGB value.
type Code uint32

// The palette, and the two codes that are not colors. A terminal theme
// decides what the palette entries look like.
const (
	// None means that no color was given.
	None Code = 0

	Black   Code = 30
	Maroon  Code = 31
	Green   Code = 32
	Olive   Code = 33
	Navy    Code = 34
	Purple  Code = 35
	Teal    Code = 36
	Silver  Code = 37
	Default Code = 39

	Gray    Code = 90
	Red     Code = 91
	Lime    Code = 92
	Yellow  Code = 93
	Blue    Code = 94
	Fuchsia Code = 95
	Aqua    Code = 96
	White   Code = 97

	DarkGray  = Gray
	LightGray = Silver
	Magenta   = Fuchsia
	Cyan      = Aqua

	// rgbFlag is bit 24. It marks a Code as an RGB value rather than a
	// palette code, which is what tells the two apart.
	rgbFlag Code = 0x1000000
)

// RGBHex returns the color with the 24 bit value hex, written the way a web
// color is: 0xFF8800 is orange.
func RGBHex(hex uint32) Code {
	return rgbFlag | Code(hex&0xFFFFFF)
}

// RGB returns the color with the red, green and blue components r, g and b.
// Each component is capped to the range 0 to 255.
func RGB(r, g, b int) Code {
	return RGBHex(cap8(r)<<16 | cap8(g)<<8 | cap8(b))
}

// FromColor returns the nearest Code to c, which lets a caller pass a color
// from image/color or from any package that builds on it.
//
// The alpha channel is dropped: a terminal has no transparency, so a color is
// used whatever its alpha says, and a fully transparent color comes out black
// rather than invisible.
func FromColor(c color.Color) Code {
	r, g, b, _ := c.RGBA()
	// RGBA returns each channel in the range 0 to 0xFFFF.
	return RGB(int(r>>8), int(g>>8), int(b>>8))
}

// RGBA satisfies color.Color, so a Code can be handed to anything that works
// with the standard library's colors.
//
// A palette color answers what that slot looks like in the usual 256 color
// table, because the terminal's own theme decides the real answer and is not
// knowable from here. None and Default both answer transparent black, which is
// the only honest answer for "the terminal decides".
//
// The four results are named because image/color.Color names them, and
// because four bare uint32 in a row say nothing about which is which.
func (c Code) RGBA() (r, g, b, a uint32) {
	var hex uint32
	switch {
	case c == None || c == Default:
		return 0, 0, 0, 0
	case c.IsRGB():
		hex = uint32(c) & 0xFFFFFF
	case c >= Black && c <= Silver:
		hex = ansi256[c-Black]
	case c >= Gray && c <= White:
		hex = ansi256[8+c-Gray]
	default:
		return 0, 0, 0, 0
	}
	to16 := func(v uint32) uint32 { return v<<8 | v }
	return to16((hex >> 16) & 0xFF), to16((hex >> 8) & 0xFF), to16(hex & 0xFF), 0xFFFF
}

// cap8 limits i to the range 0 to 255.
func cap8(i int) uint32 {
	switch {
	case i < 0:
		return 0
	case i > 255:
		return 255
	}
	return uint32(i)
}

// IsRGB reports whether the color carries a 24 bit RGB value rather than a
// palette code.
func (c Code) IsRGB() bool {
	return c >= rgbFlag
}

// rgb returns the red, green and blue components of an RGB color.
func (c Code) rgb() (int, int, int) {
	return int(c>>16) & 0xFF, int(c>>8) & 0xFF, int(c) & 0xFF
}

// FromANSI256 returns the color that the 256 color palette gives the
// index i. The first 16 entries are palette codes, and the rest are RGB. An
// index outside 0 to 256 gives Default, which is what the C code does.
func FromANSI256(i int) Code {
	switch {
	case i >= 0 && i < 8:
		return Black + Code(i)
	case i >= 8 && i < 16:
		return DarkGray + Code(i-8)
	case i >= 16 && i <= 255:
		return RGBHex(ansi256[i])
	}
	return Default
}

// ansi256 is the 256 color palette, taken from isocline/src/term_color.c. The
// first 16 entries are unused, because FromANSI256 answers those from the
// palette codes.
var ansi256 = [256]uint32{
	0x000000, 0x800000, 0x008000, 0x808000, 0x000080, 0x800080,
	0x008080, 0xc0c0c0, 0x808080, 0xff0000, 0x00ff00, 0xffff00,
	0x0000ff, 0xff00ff, 0x00ffff, 0xffffff, 0x000000, 0x00005f,
	0x000087, 0x0000af, 0x0000d7, 0x0000ff, 0x005f00, 0x005f5f,
	0x005f87, 0x005faf, 0x005fd7, 0x005fff, 0x008700, 0x00875f,
	0x008787, 0x0087af, 0x0087d7, 0x0087ff, 0x00af00, 0x00af5f,
	0x00af87, 0x00afaf, 0x00afd7, 0x00afff, 0x00d700, 0x00d75f,
	0x00d787, 0x00d7af, 0x00d7d7, 0x00d7ff, 0x00ff00, 0x00ff5f,
	0x00ff87, 0x00ffaf, 0x00ffd7, 0x00ffff, 0x5f0000, 0x5f005f,
	0x5f0087, 0x5f00af, 0x5f00d7, 0x5f00ff, 0x5f5f00, 0x5f5f5f,
	0x5f5f87, 0x5f5faf, 0x5f5fd7, 0x5f5fff, 0x5f8700, 0x5f875f,
	0x5f8787, 0x5f87af, 0x5f87d7, 0x5f87ff, 0x5faf00, 0x5faf5f,
	0x5faf87, 0x5fafaf, 0x5fafd7, 0x5fafff, 0x5fd700, 0x5fd75f,
	0x5fd787, 0x5fd7af, 0x5fd7d7, 0x5fd7ff, 0x5fff00, 0x5fff5f,
	0x5fff87, 0x5fffaf, 0x5fffd7, 0x5fffff, 0x870000, 0x87005f,
	0x870087, 0x8700af, 0x8700d7, 0x8700ff, 0x875f00, 0x875f5f,
	0x875f87, 0x875faf, 0x875fd7, 0x875fff, 0x878700, 0x87875f,
	0x878787, 0x8787af, 0x8787d7, 0x8787ff, 0x87af00, 0x87af5f,
	0x87af87, 0x87afaf, 0x87afd7, 0x87afff, 0x87d700, 0x87d75f,
	0x87d787, 0x87d7af, 0x87d7d7, 0x87d7ff, 0x87ff00, 0x87ff5f,
	0x87ff87, 0x87ffaf, 0x87ffd7, 0x87ffff, 0xaf0000, 0xaf005f,
	0xaf0087, 0xaf00af, 0xaf00d7, 0xaf00ff, 0xaf5f00, 0xaf5f5f,
	0xaf5f87, 0xaf5faf, 0xaf5fd7, 0xaf5fff, 0xaf8700, 0xaf875f,
	0xaf8787, 0xaf87af, 0xaf87d7, 0xaf87ff, 0xafaf00, 0xafaf5f,
	0xafaf87, 0xafafaf, 0xafafd7, 0xafafff, 0xafd700, 0xafd75f,
	0xafd787, 0xafd7af, 0xafd7d7, 0xafd7ff, 0xafff00, 0xafff5f,
	0xafff87, 0xafffaf, 0xafffd7, 0xafffff, 0xd70000, 0xd7005f,
	0xd70087, 0xd700af, 0xd700d7, 0xd700ff, 0xd75f00, 0xd75f5f,
	0xd75f87, 0xd75faf, 0xd75fd7, 0xd75fff, 0xd78700, 0xd7875f,
	0xd78787, 0xd787af, 0xd787d7, 0xd787ff, 0xd7af00, 0xd7af5f,
	0xd7af87, 0xd7afaf, 0xd7afd7, 0xd7afff, 0xd7d700, 0xd7d75f,
	0xd7d787, 0xd7d7af, 0xd7d7d7, 0xd7d7ff, 0xd7ff00, 0xd7ff5f,
	0xd7ff87, 0xd7ffaf, 0xd7ffd7, 0xd7ffff, 0xff0000, 0xff005f,
	0xff0087, 0xff00af, 0xff00d7, 0xff00ff, 0xff5f00, 0xff5f5f,
	0xff5f87, 0xff5faf, 0xff5fd7, 0xff5fff, 0xff8700, 0xff875f,
	0xff8787, 0xff87af, 0xff87d7, 0xff87ff, 0xffaf00, 0xffaf5f,
	0xffaf87, 0xffafaf, 0xffafd7, 0xffafff, 0xffd700, 0xffd75f,
	0xffd787, 0xffd7af, 0xffd7d7, 0xffd7ff, 0xffff00, 0xffff5f,
	0xffff87, 0xffffaf, 0xffffd7, 0xffffff, 0x080808, 0x121212,
	0x1c1c1c, 0x262626, 0x303030, 0x3a3a3a, 0x444444, 0x4e4e4e,
	0x585858, 0x626262, 0x6c6c6c, 0x767676, 0x808080, 0x8a8a8a,
	0x949494, 0x9e9e9e, 0xa8a8a8, 0xb2b2b2, 0xbcbcbc, 0xc6c6c6,
	0xd0d0d0, 0xdadada, 0xe4e4e4, 0xeeeeee,
}

// Reducing a color to what a terminal accepts.
//
// A terminal may understand 24 bit color, or a 256 entry palette, or only the
// 8 or 16 codes that ANSI defines. When it understands less than the color
// asks for, the color has to be matched to the nearest one the terminal has.
//
// Ported from isocline/src/term_color.c.

// CSI is the control sequence introducer, which starts an escape sequence.
const CSI = "\x1b["

// Palette says how much color a terminal understands.
type Palette int

// Palette values, from least to most capable.
//
// The names carry the Palette prefix although the package name already says
// ansi, because a Code constant and a Palette constant share this one
// namespace: unprefixed, ansi.ANSI16 reads as a color beside ansi.Red rather
// than as what a terminal can do.
const (
	PaletteMono      Palette = iota // no color at all
	PaletteANSI8                    // the 8 basic codes, 30 to 37
	PaletteANSI16                   // the basic codes and the bright ones, 90 to 97
	PaletteANSI256                  // a 256 entry palette
	PaletteTrueColor                // 24 bit color
)

// Bits returns how many bits of color the palette carries. The edit loop uses
// this to decide how much of a highlight it can show.
func (p Palette) Bits() int {
	switch p {
	case PaletteMono:
		return 1
	case PaletteANSI8:
		return 3
	case PaletteANSI16:
		return 4
	case PaletteANSI256:
		return 8
	case PaletteTrueColor:
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
	r1, g1, b1 := RGBHex(paletteColor).rgb()
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

// rgbMatch returns the index of the entry in the palette closest to c,
// looking only at the entries from start up to but not including end.
//
// The C keeps a sixteen entry cache in front of this, one per palette. The
// answer is the same either way, because the match depends on nothing but the
// palette and the color, so the port leaves the cache out.
func rgbMatch(table *[256]uint32, start, end int, c Code) int {
	r, g, b := c.rgb()
	gray := isGrayish(r, g, b)
	best := start
	// The C starts from the largest int32 divided by four, which leaves room
	// for the penalty below to multiply a distance without overflowing.
	bestDist := int32(2147483647) / 4
	for i := start; i < end; i++ {
		dist := rgbDistance(table[i], r, g, b)
		pr, pg, pb := RGBHex(table[i]).rgb()
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

// toANSI256 returns the palette index closest to an RGB color.
//
// The search skips the first sixteen entries, because a terminal theme is free
// to draw those however it likes, so they are not reliable targets.
func toANSI256(c Code) int {
	return rgbMatch(&ansi256, 16, 256, c)
}

// toANSI16 returns the ANSI code closest to c, from 30 to 37 and 90 to 97.
// A color that is already a palette code passes through.
func toANSI16(c Code) int {
	if !c.IsRGB() {
		return int(c)
	}
	n := rgbMatch(&ansi256, 0, 16, c)
	if n < 8 {
		return 30 + n
	}
	return 90 + n - 8
}

// toANSI8 returns the ANSI code closest to c, for a terminal that has
// only the eight basic colors and makes the bright ones with bold.
func toANSI8(c Code) int {
	if !c.IsRGB() {
		return int(c)
	}
	n := 30 + rgbMatch(&ansi256, 0, 8, c)
	r, g, b := c.rgb()
	if r >= 196 || g >= 196 || b >= 196 {
		n += 60
	}
	return n
}

// formatANSI8 returns the escape sequence for a terminal with eight colors.
// A bright color becomes bold plus the matching dim code.
func formatANSI8(c Code, bg bool) string {
	n := toANSI8(c)
	if bg {
		n += 10
	}
	if n >= 90 {
		return fmt.Sprintf("%s1;%dm", CSI, n-60)
	}
	return fmt.Sprintf("%s22;%dm", CSI, n)
}

// formatANSI16 returns the escape sequence for a terminal with sixteen
// colors.
func formatANSI16(c Code, bg bool) string {
	n := toANSI16(c)
	if bg {
		n += 10
	}
	return fmt.Sprintf("%s%dm", CSI, n)
}

// formatANSI256 returns the escape sequence for a terminal with a 256 entry
// palette.
func formatANSI256(c Code, bg bool) string {
	if !c.IsRGB() {
		return formatANSI16(c, bg)
	}
	return fmt.Sprintf("%s%d;5;%dm", CSI, selector(bg), toANSI256(c))
}

// formatRGB returns the escape sequence for a terminal with 24 bit color.
func formatRGB(c Code, bg bool) string {
	if !c.IsRGB() {
		return formatANSI16(c, bg)
	}
	r, g, b := c.rgb()
	return fmt.Sprintf("%s%d;2;%d;%d;%dm", CSI, selector(bg), r, g, b)
}

// selector returns the SGR command that introduces an extended color, which is
// 38 for the text and 48 for the background.
func selector(bg bool) int {
	if bg {
		return 48
	}
	return 38
}

// Format returns the escape sequence that sets color on a terminal with the
// given palette, or an empty string when there is nothing to set.
//
// The C leaves its buffer untouched in that last case, and one of its two
// callers passes a buffer it never initialized, so the C writes whatever the
// stack held. There is nothing to reproduce in that, and the port writes
// nothing.
func Format(p Palette, c Code, bg bool) string {
	switch {
	case c == None || p == PaletteMono:
		return ""
	case p == PaletteANSI8:
		return formatANSI8(c, bg)
	case !c.IsRGB() || p == PaletteANSI16:
		return formatANSI16(c, bg)
	case p == PaletteANSI256:
		return formatANSI256(c, bg)
	}
	return formatRGB(c, bg)
}
