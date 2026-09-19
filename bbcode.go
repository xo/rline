// Color: what a color is, what carries it, and how a line is marked with it.
//
// The file reads bottom up. A Color is an ANSI palette code or a 24 bit RGB
// value, and the reduction turns one into whatever the terminal at hand can
// show. An attr is a color and the switches that go with it, such as bold.
// Markup, written like [red]text[/red], is the way a program names a set of
// attributes in text. A highlighter marks stretches of a line with the same
// attributes and writes no tags at all.
//
// This is one subject rather than four, which is why it is one file although
// it is the longest here. Splitting it would cut between a color and the
// markup that names it.
//
// Ported from isocline/src/attr.c, term_color.c, bbcode.c and highlight.c.

package rline

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

// --------------------------------------------------------------------------
// color.go

// --------------------------------------------------------------------------
// color.go

// Colors.
//
// A Color is either an ANSI palette code or a 24 bit RGB value. Bit 24 is set
// on an RGB value, which is what tells the two apart.
//
// Ported from isocline/src/term_color.c and isocline/src/common.h.

// Color is a text color.
type Color uint32

// ColorNone means that no color was given.
const ColorNone Color = 0

// The ANSI palette. A terminal theme decides what these look like.
const (
	ANSIBlack   Color = 30
	ANSIMaroon  Color = 31
	ANSIGreen   Color = 32
	ANSIOlive   Color = 33
	ANSINavy    Color = 34
	ANSIPurple  Color = 35
	ANSITeal    Color = 36
	ANSISilver  Color = 37
	ANSIDefault Color = 39

	ANSIGray    Color = 90
	ANSIRed     Color = 91
	ANSILime    Color = 92
	ANSIYellow  Color = 93
	ANSIBlue    Color = 94
	ANSIFuchsia Color = 95
	ANSIAqua    Color = 96
	ANSIWhite   Color = 97

	ANSIDarkGray  = ANSIGray
	ANSILightGray = ANSISilver
	ANSIMagenta   = ANSIFuchsia
	ANSICyan      = ANSIAqua
)

// rgbFlag is bit 24. It marks a Color as an RGB value rather than a palette
// code.
const rgbFlag Color = 0x1000000

// RGBHex returns the color with the 24 bit value hex, written the way a web
// color is: 0xFF8800 is orange.
func RGBHex(hex uint32) Color {
	return rgbFlag | Color(hex&0xFFFFFF)
}

// RGB returns the color with the red, green and blue components r, g and b.
// Each component is capped to the range 0 to 255.
func RGB(r, g, b int) Color {
	return RGBHex(cap8(r)<<16 | cap8(g)<<8 | cap8(b))
}

// FromColor returns the nearest Color to c, which lets a caller pass a color
// from image/color or from any package that builds on it.
//
// The alpha channel is dropped: a terminal has no transparency, so a color is
// used whatever its alpha says, and a fully transparent color comes out black
// rather than invisible.
func FromColor(c color.Color) Color {
	r, g, b, _ := c.RGBA()
	// RGBA returns each channel in the range 0 to 0xFFFF.
	return RGB(int(r>>8), int(g>>8), int(b>>8))
}

// RGBA satisfies color.Color, so a Color can be handed to anything that works
// with the standard library's colors.
//
// A palette color answers what that slot looks like in the usual 256 color
// table, because the terminal's own theme decides the real answer and is not
// knowable from here. ColorNone and ANSIDefault both answer transparent
// black, which is the only honest answer for "the terminal decides".
func (c Color) RGBA() (r, g, b, a uint32) {
	var hex uint32
	switch {
	case c == ColorNone || c == ANSIDefault:
		return 0, 0, 0, 0
	case c.isRGB():
		hex = uint32(c) & 0xFFFFFF
	case c >= ANSIBlack && c <= ANSISilver:
		hex = ansi256[c-ANSIBlack]
	case c >= ANSIGray && c <= ANSIWhite:
		hex = ansi256[8+c-ANSIGray]
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

// isRGB reports whether the color carries an RGB value rather than a palette
// code.
func (c Color) isRGB() bool {
	return c >= rgbFlag
}

// rgb returns the red, green and blue components of an RGB color.
func (c Color) rgb() (int, int, int) {
	return int(c>>16) & 0xFF, int(c>>8) & 0xFF, int(c) & 0xFF
}

// colorFromANSI256 returns the color that the 256 color palette gives the
// index i. The first 16 entries are palette codes, and the rest are RGB. An
// index outside 0 to 256 gives ANSIDefault, which is what the C code does.
func colorFromANSI256(i int) Color {
	switch {
	case i >= 0 && i < 8:
		return ANSIBlack + Color(i)
	case i >= 8 && i < 16:
		return ANSIDarkGray + Color(i-8)
	case i >= 16 && i <= 255:
		return RGBHex(ansi256[i])
	}
	return ANSIDefault
}

// ansi256 is the 256 color palette, taken from isocline/src/term_color.c. The
// first 16 entries are unused, because colorFromANSI256 answers those from the
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

// --------------------------------------------------------------------------
// attr.go

// Text attributes.
//
// An attr carries a foreground color, a background color, and four flags. Each
// flag is three valued: turn it on, turn it off, or say nothing about it. That
// third value is what lets one attribute be laid over another, which is how
// nested markup builds up a final style.
//
// The C code packs all of this into a 64 bit union of bit fields, so that it
// can compare two attributes with one integer compare. A Go struct of
// comparable fields compares with == and needs no packing, so the port drops
// it. Nothing outside attr.c depends on the packed value.
//
// Ported from isocline/src/attr.c.

// attrFlag is a three valued flag.
type attrFlag int8

// attrFlag values. flagNone means the attribute says nothing, so whatever is
// underneath shows through.
const (
	flagNone attrFlag = 0
	flagOn   attrFlag = 1
	flagOff  attrFlag = -1
)

// attr is a set of text attributes.
//
// The C color fields are 28 bits wide, so a color above that would be cut
// short there. No path produces one, because every color this package makes is
// either a palette code or an RGB value with bit 24 set, which needs 25 bits.
type attr struct {
	color   Color
	bgColor Color

	bold      attrFlag
	italic    attrFlag
	reverse   attrFlag
	underline attrFlag
}

// attrDefault returns the attributes that turn everything back to the default
// of the terminal.
func attrDefault() attr {
	return attr{
		color:     ANSIDefault,
		bgColor:   ANSIDefault,
		bold:      flagOff,
		italic:    flagOff,
		reverse:   flagOff,
		underline: flagOff,
	}
}

// attrFromColor returns attributes that set the foreground color and say
// nothing else.
func attrFromColor(c Color) attr {
	return attr{color: c}
}

// isNone reports whether the attributes say nothing at all.
func (a attr) isNone() bool {
	return a == attr{}
}

// updateWith lays b over a. Every part of b that says nothing leaves a alone.
func (a attr) updateWith(b attr) attr {
	if b.color != ColorNone {
		a.color = b.color
	}
	if b.bgColor != ColorNone {
		a.bgColor = b.bgColor
	}
	if b.bold != flagNone {
		a.bold = b.bold
	}
	if b.italic != flagNone {
		a.italic = b.italic
	}
	if b.reverse != flagNone {
		a.reverse = b.reverse
	}
	if b.underline != flagNone {
		a.underline = b.underline
	}
	return a
}

// sgrIsDigit reports whether the byte at i is a digit. It answers false past
// the end of s, where the C code reads the terminating zero.
func sgrIsDigit(s string, i int) bool {
	return i < len(s) && s[i] >= '0' && s[i] <= '9'
}

// sgrIsSep reports whether the byte at i separates two SGR parameters. SGR
// allows either character, and the two mean the same thing.
func sgrIsSep(s string, i int) bool {
	return i < len(s) && (s[i] == ';' || s[i] == ':')
}

// sgrNextPar reads one parameter from s, starting at i. It returns the
// parameter and the index just after it.
//
// No digits at i is not an error. The parameter is then 0 and the index does
// not move, which is how an empty parameter such as the one in "1;;4" reads as
// a zero.
func sgrNextPar(s string, i int) (int, int, bool) {
	n := 0
	for sgrIsDigit(s, i+n) {
		n++
	}
	if n == 0 {
		return 0, i, true
	}
	par, ok := atoz(s[i:])
	return par, i + n, ok
}

// sgrNextPar3 reads three parameters separated by SGR separators. It returns
// the index it reached whether or not it read all three.
func sgrNextPar3(s string, i int) (int, int, int, int, bool) {
	var p1, p2, p3 int
	var ok bool
	if p1, i, ok = sgrNextPar(s, i); !ok || !sgrIsSep(s, i) {
		return p1, p2, p3, i, false
	}
	i++
	if p2, i, ok = sgrNextPar(s, i); !ok || !sgrIsSep(s, i) {
		return p1, p2, p3, i, false
	}
	i++
	p3, i, ok = sgrNextPar(s, i)
	return p1, p2, p3, i, ok
}

// attrFromSGR reads a Select Graphic Rendition parameter string, which is the
// part of an escape sequence between "\x1b[" and the final "m".
//
// An unknown parameter is skipped. The C code writes a debug line for it, and
// the port drops that, because nothing reads it.
func attrFromSGR(s string) attr {
	var a attr
	for i := 0; i < len(s) && s[i] != 0; i++ {
		cmd, next, ok := sgrNextPar(s, i)
		i = next
		if !ok {
			continue
		}
		switch {
		case cmd == 0:
			a = attrDefault()
		case cmd == 1:
			a.bold = flagOn
		case cmd == 3:
			a.italic = flagOn
		case cmd == 4:
			a.underline = flagOn
		case cmd == 7:
			a.reverse = flagOn
		case cmd == 22:
			a.bold = flagOff
		case cmd == 23:
			a.italic = flagOff
		case cmd == 24:
			a.underline = flagOff
		case cmd == 27:
			a.reverse = flagOff
		case cmd == 39:
			a.color = ANSIDefault
		case cmd == 49:
			a.bgColor = ANSIDefault
		case cmd >= 30 && cmd <= 37:
			a.color = ANSIBlack + Color(cmd-30)
		case cmd >= 40 && cmd <= 47:
			a.bgColor = ANSIBlack + Color(cmd-40)
		case cmd >= 90 && cmd <= 97:
			a.color = ANSIDarkGray + Color(cmd-90)
		case cmd >= 100 && cmd <= 107:
			a.bgColor = ANSIDarkGray + Color(cmd-100)
		case (cmd == 38 || cmd == 48) && sgrIsSep(s, i):
			// SGR 38 and 48 take their own parameters, which is the one place
			// where the format is not a flat list.
			i = sgrExtended(s, i+1, cmd, &a)
		}
	}
	return a
}

// sgrExtended reads the parameters of an SGR 38 or 48, which name a color
// either by an index into the 256 color palette or by three RGB components. It
// returns the index it reached.
func sgrExtended(s string, i, cmd int, a *attr) int {
	par, i, ok := sgrNextPar(s, i)
	if !ok {
		return i
	}
	set := func(c Color) {
		if cmd == 38 {
			a.color = c
		} else {
			a.bgColor = c
		}
	}
	switch {
	case par == 5 && sgrIsSep(s, i):
		i++
		if par, i, ok = sgrNextPar(s, i); ok && par >= 0 && par <= 0xFF {
			set(colorFromANSI256(par))
		}
	case par == 2 && sgrIsSep(s, i):
		i++
		var r, g, b int
		if r, g, b, i, ok = sgrNextPar3(s, i); ok {
			set(RGB(r, g, b))
		}
	}
	return i
}

// attrFromEscSGR reads a whole escape sequence, "\x1b[" then parameters then
// "m". Anything else gives no attributes at all.
func attrFromEscSGR(s string) attr {
	if len(s) <= 2 || s[0] != 0x1B || s[1] != '[' || s[len(s)-1] != 'm' {
		return attr{}
	}
	return attrFromSGR(s[2:])
}

// attrBuf holds one attribute for every byte of the text it describes. The
// edit loop builds one of these beside the line it is about to draw.
//
// The C version carries its own capacity and growth policy. A Go slice grows
// on demand, so that is gone.
//
// A nil attrBuf is usable, and every method accepts one. The C code passes a
// null pointer where a caller wants the text without the attributes.
type attrBuf struct {
	attrs []attr
}

// length returns how many attributes the buffer holds.
func (ab *attrBuf) length() int {
	if ab == nil {
		return 0
	}
	return len(ab.attrs)
}

// clear drops every attribute.
func (ab *attrBuf) clear() {
	if ab == nil {
		return
	}
	ab.attrs = ab.attrs[:0]
}

// grow extends the buffer to n attributes, filling any new one with nothing.
func (ab *attrBuf) grow(n int) {
	for len(ab.attrs) < n {
		ab.attrs = append(ab.attrs, attr{})
	}
}

// slice returns the attributes, extended to at least n of them.
func (ab *attrBuf) slice(n int) []attr {
	if ab == nil {
		return nil
	}
	ab.grow(n)
	return ab.attrs
}

// at returns the attribute at pos, or nothing when pos is outside the buffer.
//
// The C code tests pos against the count with the wrong comparison, so at the
// one position just past the end it reads a slot it never wrote. That is
// undefined behavior rather than a wrong answer, so there is nothing to
// reproduce. This returns the empty attribute there.
func (ab *attrBuf) at(pos int) attr {
	if ab == nil || pos < 0 || pos >= len(ab.attrs) {
		return attr{}
	}
	return ab.attrs[pos]
}

// setAt replaces count attributes from pos, extending the buffer if it has to.
func (ab *attrBuf) setAt(pos, count int, a attr) {
	ab.fill(pos, count, a, false)
}

// updateAt lays a over count attributes from pos, extending the buffer if it
// has to.
func (ab *attrBuf) updateAt(pos, count int, a attr) {
	ab.fill(pos, count, a, true)
}

// fill is the shared part of setAt and updateAt.
func (ab *attrBuf) fill(pos, count int, a attr, update bool) {
	if ab == nil || pos < 0 || count <= 0 {
		return
	}
	end := pos + count
	ab.grow(end)
	for i := pos; i < end; i++ {
		if update {
			ab.attrs[i] = ab.attrs[i].updateWith(a)
			continue
		}
		ab.attrs[i] = a
	}
}

// insertAt makes room for count attributes at pos and fills them with a.
func (ab *attrBuf) insertAt(pos, count int, a attr) {
	if ab == nil || pos < 0 || pos > len(ab.attrs) || count <= 0 {
		return
	}
	ab.attrs = append(ab.attrs, make([]attr, count)...)
	copy(ab.attrs[pos+count:], ab.attrs[pos:])
	ab.setAt(pos, count, a)
}

// deleteAt removes count attributes from pos. It removes fewer when the
// buffer ends first.
//
// The C code hands an attribute count to memmove where every other function
// hands it a byte count, so it shifts one eighth of what it should and leaves
// stale attributes behind. bbcode.c calls this, so the fault is live rather
// than dead code. The port does the intended thing instead, because the bytes
// that the fault leaves behind depend on how a compiler packs a bit field,
// which makes the wrong answer unportable and therefore useless as a
// reference. testdata/attr-delta.txt records what the C does.
func (ab *attrBuf) deleteAt(pos, count int) {
	if ab == nil || pos < 0 || pos > len(ab.attrs) {
		return
	}
	if pos+count > len(ab.attrs) {
		count = len(ab.attrs) - pos
	}
	if count <= 0 {
		return
	}
	ab.attrs = append(ab.attrs[:pos], ab.attrs[pos+count:]...)
}

// appendTo adds s to the text buffer and gives every byte of it the attribute
// a. It returns the length of the text buffer afterwards.
//
// The attribute buffer may be nil, which appends the text and records no
// attributes.
func (ab *attrBuf) appendTo(b *buffer, s string, a attr) int {
	if s == "" {
		return b.length()
	}
	ab.setAt(ab.length(), len(s), a)
	return b.appendString(s)
}

// --------------------------------------------------------------------------
// termcolor.go

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

// --------------------------------------------------------------------------
// bbcode.go

// --------------------------------------------------------------------------
// bbcode.go

// Markup for styled output, written like [red]text[/red].
//
// A tag opens a style, and a closing tag puts back what was there before, so
// tags nest. A tag can also name a width, which pads or cuts what it wraps.
//
// Ported from isocline/src/bbcode.c.

// align says where text sits inside a fixed width.
type align int

// align values.
const (
	alignLeft align = iota
	alignCenter
	alignRight
)

// widthSpec is a width given by a tag, written as
// <width>;<left|center|right>;<fill>;<cut>.
type widthSpec struct {
	// w is the width in columns. Zero means no width was given.
	w int

	// align says where the text sits when it is padded.
	align align

	// dots says to end cut text with "...".
	dots bool

	// fill is the byte to pad with. Zero means do not pad.
	fill byte
}

// bbTag is one open tag.
type bbTag struct {
	// name is what the tag was called.
	//
	// It is always empty. The C means to record the name here, but it guards
	// the assignment with a test on the field it is about to write rather
	// than on the value it is writing, and the field starts as a null
	// pointer, so the assignment never happens. See closeTag for what that
	// costs.
	name string

	// attr is what was in force before this tag opened.
	attr attr

	// width is the width the tag asks for.
	width widthSpec

	// pos is where the text this tag wraps starts in the output.
	pos int
}

// bbStyle is a named set of attributes.
type bbStyle struct {
	name string
	attr attr
}

// bbCode turns markup into text and attributes.
type bbCode struct {
	// term is where print writes.
	term *term

	// tags is the stack of open tags, and styles are the ones defined by name.
	tags   []bbTag
	styles []bbStyle

	// Working buffers, kept so that printing does not allocate each time.
	out      buffer
	outAttrs attrBuf
	vout     buffer
}

// newBBCode returns a bbCode that prints to term.
func newBBCode(t *term) *bbCode {
	return &bbCode{term: t}
}

// builtinStyles are the styles that need no definition.
var builtinStyles = []bbStyle{
	{"b", attr{bold: flagOn}},
	{"r", attr{reverse: flagOn}},
	{"u", attr{underline: flagOn}},
	{"i", attr{italic: flagOn}},
	{"em", attr{bold: flagOn}},
	{"url", attr{underline: flagOn}},
}

// styleAdd gives a name to a set of attributes.
func (bb *bbCode) styleAdd(name string, a attr) {
	bb.styles = append(bb.styles, bbStyle{name: name, attr: a})
}

// styleDef gives a name to the attributes that the markup spec describes.
func (bb *bbCode) styleDef(name, spec string) {
	bb.styleAdd(name, bb.parseTagContent(spec).attr)
}

// style returns the attributes that a style name stands for.
func (bb *bbCode) style(name string) attr {
	var t bbTag
	bb.updateWithStyles(&t, name, "", false)
	return t.attr
}

// pushTag puts a tag on the stack and returns where it landed.
func (bb *bbCode) pushTag(t bbTag) int {
	bb.tags = append(bb.tags, t)
	return len(bb.tags) - 1
}

// popTag takes the innermost tag off the stack.
func (bb *bbCode) popTag() bbTag {
	if len(bb.tags) == 0 {
		return bbTag{}
	}
	t := bb.tags[len(bb.tags)-1]
	bb.tags = bb.tags[:len(bb.tags)-1]
	return t
}

// openTag pushes the current attributes and returns what is in force inside
// the tag.
func (bb *bbCode) openTag(outPos int, t bbTag, current attr) attr {
	bb.pushTag(bbTag{name: t.name, attr: current, width: t.width, pos: outPos})
	return current.updateWith(t.attr)
}

// closeTag takes the innermost tag off the stack, so long as one was opened
// after base. It returns that tag and whether there was one.
//
// The C looks for a tag whose name matches the closing tag, and keeps a whole
// branch for the unbalanced case. None of it runs, because every tag name is
// empty, so the first tag it pops always matches. A closing tag therefore
// closes whatever is innermost, whatever it is called, and "[b][i]x[/b][/i]"
// behaves the same as "[b][i]x[/i][/b]".
func (bb *bbCode) closeTag(base int) (bbTag, bool) {
	if len(bb.tags) <= base {
		return bbTag{}, false
	}
	return bb.popTag(), true
}

//-------------------------------------------------------------
// Reading the parts of a tag
//-------------------------------------------------------------

// updateBool sets a three valued flag from a written value.
//
// It always sets the flag on. The C means to compare the value against "on",
// "true" and "1", but it uses each strcmp as a truth value rather than testing
// it against zero, and strcmp answers zero when the two are equal. So the
// first branch is taken for every value that is not equal to all three at
// once, which no value is. [bold=off] therefore turns bold on.
func updateBool(field *attrFlag, _ string) {
	*field = flagOn
}

// updateColor reads a color, which may be "none", a hex value such as
// "#ff0000", or an HTML color name.
func updateColor(field *Color, value string) {
	if value == "" || value == "none" {
		*field = ColorNone
		return
	}
	if value[0] == '#' {
		// The C reads this with sscanf, which takes as many hex digits as it
		// finds and does not widen a short value, so "#f00" is 0xf00 rather
		// than 0xff0000.
		if v, ok := scanHex(value[1:]); ok {
			*field = RGBHex(v)
		}
		return
	}
	if c, ok := htmlColors[value]; ok {
		*field = c
		return
	}
	*field = ColorNone
}

// scanHex reads the hex digits at the front of s, the way sscanf reads %x.
func scanHex(s string) (uint32, bool) {
	n := 0
	for n < len(s) && isHexDigit(s[n]) {
		n++
	}
	if n == 0 {
		return 0, false
	}
	// sscanf into a 32 bit value keeps the low bits of a longer number.
	v, err := strconv.ParseUint(s[:n], 16, 64)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}

// isHexDigit reports whether c is a hexadecimal digit.
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// updateANSIColor reads a palette index between 0 and 256.
func updateANSIColor(field *Color, value string) {
	if n, ok := atoz(value); ok && n >= 0 && n <= 256 {
		*field = colorFromANSI256(n)
	}
}

// updateWidth reads a width, written as
// <width>;<left|center|right>;<fill>;<cut>.
//
// A width that is not a number leaves everything at nothing, except the fill,
// which keeps the default it was given.
func updateWidth(out *widthSpec, defaultFill byte, value string) {
	w := widthSpec{fill: defaultFill}
	defer func() { *out = w }()
	n, ok := atoz(value)
	if !ok {
		return
	}
	w.w = n
	i := 0
	for i < len(value) && value[i] != ';' {
		i++
	}
	if i >= len(value) {
		return
	}
	i++
	field := func() string {
		start := i
		for i < len(value) && value[i] != ';' {
			i++
		}
		return value[start:i]
	}
	switch f := field(); {
	case len(f) == 4 && hasPrefixFold(f, "left"):
		w.align = alignLeft
	case len(f) == 5 && hasPrefixFold(f, "right"):
		w.align = alignRight
	case len(f) == 6 && hasPrefixFold(f, "center"):
		w.align = alignCenter
	}
	if i >= len(value) {
		return
	}
	i++
	if f := field(); len(f) == 1 {
		w.fill = f[0]
	}
	if i >= len(value) {
		return
	}
	i++
	f := field()
	if (len(f) == 2 && hasPrefixFold(f, "on")) || f == "1" {
		w.dots = true
	}
}

// updateProperty sets one named property on a tag. It returns the name the
// property is known by, or an empty string when the name is not a property at
// all.
func updateProperty(t *bbTag, name, value string) string {
	setFlag := func(field *attrFlag) string {
		b := flagNone
		updateBool(&b, value)
		if b != flagNone {
			*field = b
		}
		return name
	}
	setColor := func(field *Color, read func(*Color, string)) string {
		c := ColorNone
		read(&c, value)
		if c != ColorNone {
			*field = c
		}
		return name
	}
	switch name {
	case "bold":
		return setFlag(&t.attr.bold)
	case "italic":
		return setFlag(&t.attr.italic)
	case "underline":
		return setFlag(&t.attr.underline)
	case "reverse":
		return setFlag(&t.attr.reverse)
	case "color":
		return setColor(&t.attr.color, updateColor)
	case "bgcolor":
		return setColor(&t.attr.bgColor, updateColor)
	case "ansi-sgr":
		t.attr = t.attr.updateWith(attrFromSGR(value))
		return name
	case "ansi-color":
		return setColor(&t.attr.color, updateANSIColor)
	case "ansi-bgcolor":
		return setColor(&t.attr.bgColor, updateANSIColor)
	case "width":
		updateWidth(&t.width, ' ', value)
		return name
	case "max-width":
		updateWidth(&t.width, 0, value)
		return "width"
	}
	return ""
}

// updateWithStyles applies one name and value to a tag. The name may be a
// property, a style defined by the caller, a builtin style, or an HTML color.
func (bb *bbCode) updateWithStyles(t *bbTag, name, value string, useBgColor bool) {
	// A bare hex value names a color.
	if strings.HasPrefix(name, "#") && value == "" {
		value = name
		name = "color"
		if useBgColor {
			name = "bgcolor"
		}
	}
	if updateProperty(t, name, value) != "" {
		return
	}
	// The styles defined by the caller are searched from the newest back, so a
	// later definition of the same name wins.
	for i := len(bb.styles) - 1; i >= 0; i-- {
		if bb.styles[i].name == name {
			t.attr = t.attr.updateWith(bb.styles[i].attr)
			return
		}
	}
	for _, s := range builtinStyles {
		if s.name == name {
			t.attr = t.attr.updateWith(s.attr)
			return
		}
	}
	if c, ok := htmlColors[name]; ok {
		var ca attr
		if useBgColor {
			ca.bgColor = c
		} else {
			ca.color = c
		}
		t.attr = t.attr.updateWith(ca)
	}
}

//-------------------------------------------------------------
// Parsing a tag
//-------------------------------------------------------------

// isTagSpace reports whether c separates the parts of a tag.
func isTagSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// parseSkipWhite steps over the spaces at i, stopping at the end of the tag.
func parseSkipWhite(s string, i int) int {
	for i < len(s) && s[i] != ']' && isTagSpace(s[i]) {
		i++
	}
	return i
}

// parseSkipToWhite steps to the next space and then over it.
func parseSkipToWhite(s string, i int) int {
	for i < len(s) && s[i] != ']' && !isTagSpace(s[i]) {
		i++
	}
	return parseSkipWhite(s, i)
}

// parseAttrName steps over the name at i.
//
// A name that starts with "#" is a hex color, and there the upper case letters
// run all the way to Z rather than stopping at F, which is what parseValue
// does. The two disagree, and the port keeps both.
func parseAttrName(s string, i int) int {
	if i < len(s) && s[i] == '#' {
		for i++; i < len(s) && s[i] != ']' && isHexNameByte(s[i]); i++ {
		}
		return i
	}
	for ; i < len(s) && s[i] != ']' && isNameByte(s[i]); i++ {
	}
	return i
}

// isHexNameByte reports whether c may appear in a hex color used as a name.
// The upper case range runs to Z rather than to F, which is what the C does.
func isHexNameByte(c byte) bool {
	return (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// isNameByte reports whether c may appear in a tag name.
func isNameByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_' || c == '-'
}

// isValueByte reports whether c may appear in an unquoted value. The upper
// case range stops at F, unlike every other class here, so a value such as
// "RIGHT" reads as empty.
func isValueByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'F') ||
		(c >= '0' && c <= '9') || c == '-' || c == '_'
}

// parseValue steps over the value at i and returns where it starts and ends,
// and where to carry on from.
//
// An unquoted value takes upper case letters only as far as F, so a value such
// as "RIGHT" reads as empty. A quoted value takes anything up to the closing
// quote.
func parseValue(s string, i int) (int, int, int) {
	switch {
	case i < len(s) && s[i] == '"':
		i++
		start := i
		for i < len(s) && s[i] != '"' {
			i++
		}
		end := i
		if i < len(s) && s[i] == '"' {
			i++
		}
		return start, end, i
	case i < len(s) && s[i] == '#':
		start := i
		for i++; i < len(s) && isHexNameByte(s[i]); i++ {
		}
		return start, i, i
	}
	start := i
	for ; i < len(s) && isValueByte(s[i]); i++ {
	}
	return start, i, i
}

// nameLimit is how long a tag name or value may be. The C copies each into a
// buffer of this size and leaves the buffer alone when it does not fit, which
// means reading whatever was there before. The port cuts instead.
const nameLimit = 127

// lowerTagText cuts text to the length a tag name may be and lowers it by the
// ASCII rule, which is what the C does before it looks anything up.
func lowerTagText(s string) string {
	if len(s) > nameLimit {
		s = s[:nameLimit]
	}
	b := []byte(s)
	for i := range b {
		b[i] = asciiLower(b[i])
	}
	return string(b)
}

// parseTagValue reads one name and value from a tag and applies it. It returns
// the name it read and where to carry on from.
func (bb *bbCode) parseTagValue(t *bbTag, s string, i int) (string, int) {
	useBgColor := false
	idStart := i
	idEnd := parseAttrName(s, idStart)
	if idStart == idEnd {
		return "", parseSkipToWhite(s, idStart)
	}
	i = parseSkipWhite(s, idEnd)
	// "on" in front of a color name means the background, unless it is being
	// given a value of its own.
	if idEnd-idStart == 2 && compareFoldN(s[idStart:], "on", 2) == 0 &&
		(i >= len(s) || s[i] != '=') {
		useBgColor = true
		idStart = i
		idEnd = parseAttrName(s, idStart)
		if idStart == idEnd {
			return "", parseSkipToWhite(s, idStart)
		}
		i = parseSkipWhite(s, idEnd)
	}
	valStart, valEnd := 0, 0
	if i < len(s) && s[i] == '=' {
		i = parseSkipWhite(s, i+1)
		valStart, valEnd, i = parseValue(s, i)
		i = parseSkipWhite(s, i)
	}
	name := lowerTagText(s[idStart:idEnd])
	bb.updateWithStyles(t, name, lowerTagText(s[valStart:valEnd]), useBgColor)
	return name, i
}

// parseTagValues reads every name and value in a tag. It returns the first
// name, which is what a "[!pre]" tag is closed by.
func (bb *bbCode) parseTagValues(t *bbTag, s string, i int) (string, int) {
	i = parseSkipWhite(s, i)
	first := ""
	count := 0
	for i < len(s) && s[i] != ']' {
		name, next := bb.parseTagValue(t, s, i)
		if count == 0 {
			first = name
		}
		i = next
		count++
	}
	if i < len(s) && s[i] == ']' {
		i++
	}
	return first, i
}

// parseTag reads a whole tag. It returns the first name in it, whether the tag
// opens or closes, whether it holds its content unread, and where to carry on.
func (bb *bbCode) parseTag(t *bbTag, s string, i int) (string, bool, bool, int) {
	open, pre := true, false
	if i >= len(s) || s[i] != '[' {
		return "", open, pre, i
	}
	i = parseSkipWhite(s, i+1)
	switch {
	case i < len(s) && s[i] == '!':
		pre = true
		i = parseSkipWhite(s, i+1)
	case i < len(s) && s[i] == '/':
		open = false
		i = parseSkipWhite(s, i+1)
	}
	name, i := bb.parseTagValues(t, s, i)
	return name, open, pre, i
}

// parseTagContent reads the inside of a tag, without the brackets.
func (bb *bbCode) parseTagContent(s string) bbTag {
	var t bbTag
	bb.parseTagValues(&t, s, 0)
	return t
}

// styleOpen opens a style on the terminal.
func (bb *bbCode) styleOpen(spec string) {
	t := bb.parseTagContent(spec)
	bb.term.setAttr(bb.openTag(0, t, bb.term.getAttr()))
}

// styleClose closes the innermost style on the terminal.
func (bb *bbCode) styleClose(spec string) {
	base := len(bb.tags) - 1
	bb.parseTagContent(spec)
	if prev, ok := bb.closeTag(base); ok {
		bb.term.setAttr(prev.attr)
	}
}

//-------------------------------------------------------------
// Fitting text to a width
//-------------------------------------------------------------

// restrictWidth pads or cuts the output from start so that it takes exactly
// the width the tag asked for.
func restrictWidth(start int, width widthSpec, out *buffer, outAttrs *attrBuf) {
	if width.w <= 0 {
		return
	}
	s := out.bytes()[start:]
	length := len(s)
	w := strWidth(s)
	switch {
	case w == width.w:
		return
	case w > width.w:
		// Too wide. Cut it, leaving room for the dots when they are wanted.
		inner := width.w
		if width.dots && width.w > 3 {
			inner = width.w - 3
		}
		if width.align == alignRight {
			ndel := skipUntilFit(s, inner)
			out.deleteAt(start, ndel)
			outAttrs.deleteAt(start, ndel)
			if inner < width.w {
				out.insertAt("...", start)
				outAttrs.insertAt(start, 3, outAttrs.at(start))
			}
			return
		}
		count := takeWhileFit(s, inner)
		out.deleteAt(start+count, length-count)
		outAttrs.deleteAt(start+count, length-count)
		if inner < width.w {
			outAttrs.appendTo(out, "...", outAttrs.at(start))
		}
	default:
		// Too narrow. Pad it.
		diff := width.w - w
		padLeft, padRight := 0, 0
		switch width.align {
		case alignRight:
			padLeft = diff
		case alignLeft:
			padRight = diff
		default:
			padLeft = diff / 2
			padRight = diff - padLeft
		}
		if width.fill == 0 {
			return
		}
		if padLeft > 0 {
			a := outAttrs.at(start)
			for range padLeft {
				out.insertByteAt(width.fill, start)
			}
			outAttrs.insertAt(start, padLeft, a)
		}
		if padRight > 0 {
			a := outAttrs.at(out.length() - 1)
			for range padRight {
				outAttrs.appendTo(out, string(width.fill), a)
			}
		}
	}
}

//-------------------------------------------------------------
// Turning markup into text and attributes
//-------------------------------------------------------------

// processTag handles one tag at i and returns how many bytes it used.
func (bb *bbCode) processTag(s string, i, nestingBase int, out *buffer, outAttrs *attrBuf, cur *attr) int {
	var t bbTag
	name, open, isPre, end := bb.parseTag(&t, s, i)
	switch {
	case open && !isPre:
		*cur = bb.openTag(out.length(), t, *cur)
	case open:
		// A "[!name]" tag holds everything up to its closing tag unread, so
		// markup inside it is text.
		a := cur.updateWith(t.attr)
		closing := "[/" + name + "]"
		if at := strings.Index(s[end:], closing); at < 0 {
			outAttrs.appendTo(out, s[end:], a)
			end = len(s)
		} else {
			outAttrs.appendTo(out, s[end:end+at], a)
			end += at + len(closing)
		}
	default:
		if prev, ok := bb.closeTag(nestingBase); ok {
			*cur = prev.attr
			if prev.width.w > 0 {
				restrictWidth(prev.pos, prev.width, out, outAttrs)
			}
		}
	}
	return end - i
}

// appendTo turns markup into text in out and one attribute per byte in
// outAttrs, which may be nil when only the text is wanted.
func (bb *bbCode) appendTo(s string, out *buffer, outAttrs *attrBuf) {
	var a attr
	base := len(bb.tags)
	i := 0
	for i < len(s) {
		// Text with no markup in it goes across in one piece.
		n := 0
		for i+n < len(s) {
			c := s[i+n]
			if c == '[' || c == '\\' {
				break
			}
			// An escape sequence may hold a bracket, which is not a tag.
			if c == 0x1B && i+n+1 < len(s) && s[i+n+1] == '[' {
				n++
			}
			n++
		}
		if n > 0 {
			outAttrs.appendTo(out, s[i:i+n], a)
			i += n
		}
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '[':
			i += bb.processTag(s, i, base, out, outAttrs, &a)
		case '\\':
			if i+1 < len(s) && (s[i+1] == '\\' || s[i+1] == '[') {
				outAttrs.appendTo(out, s[i+1:i+2], a)
				i += 2
				continue
			}
			outAttrs.appendTo(out, s[i:i+1], a)
			i++
		}
	}
	// Anything still open is dropped.
	for len(bb.tags) > base {
		bb.popTag()
	}
}

// print writes markup to the terminal.
func (bb *bbCode) print(s string) {
	bb.appendTo(s, &bb.out, &bb.outAttrs)
	bb.term.writeFormatted(bb.out.string(), bb.outAttrs.slice(bb.out.length()))
	bb.outAttrs.clear()
	bb.out.clear()
}

// println writes markup to the terminal and ends the line.
func (bb *bbCode) println(s string) {
	bb.print(s)
	bb.term.writeln("")
}

// columnWidth returns how many columns the markup takes once the tags are
// taken out.
func (bb *bbCode) columnWidth(s string) int {
	if s == "" {
		return 0
	}
	bb.appendTo(s, &bb.vout, nil)
	w := strWidth(bb.vout.bytes())
	bb.vout.clear()
	return w
}

// --------------------------------------------------------------------------
// bbcodecolors.go

// The HTML color names that bbcode markup accepts, such as [red]text[/red].
//
// The C keeps these in a sorted array and finds one with a binary search over
// strcmp. A map answers the same question, because the search is only ever for
// an exact match.
//
// Ported from isocline/src/bbcode_colors.c.

// htmlColors maps a color name to the color it stands for. The names that
// start with "ansi-" give a palette code, and the rest give an RGB value.
var htmlColors = map[string]Color{
	"aliceblue":            RGBHex(0xf0f8ff),
	"ansi-aqua":            ANSIAqua,
	"ansi-black":           ANSIBlack,
	"ansi-blue":            ANSIBlue,
	"ansi-cyan":            ANSICyan,
	"ansi-darkgray":        ANSIDarkGray,
	"ansi-darkgrey":        ANSIDarkGray,
	"ansi-default":         ANSIDefault,
	"ansi-fuchsia":         ANSIFuchsia,
	"ansi-gray":            ANSIGray,
	"ansi-green":           ANSIGreen,
	"ansi-grey":            ANSIGray,
	"ansi-lightgray":       ANSILightGray,
	"ansi-lightgrey":       ANSILightGray,
	"ansi-lime":            ANSILime,
	"ansi-magenta":         ANSIMagenta,
	"ansi-maroon":          ANSIMaroon,
	"ansi-navy":            ANSINavy,
	"ansi-olive":           ANSIOlive,
	"ansi-purple":          ANSIPurple,
	"ansi-red":             ANSIRed,
	"ansi-silver":          ANSISilver,
	"ansi-teal":            ANSITeal,
	"ansi-white":           ANSIWhite,
	"ansi-yellow":          ANSIYellow,
	"antiquewhite":         RGBHex(0xfaebd7),
	"aqua":                 RGBHex(0x00ffff),
	"aquamarine":           RGBHex(0x7fffd4),
	"azure":                RGBHex(0xf0ffff),
	"beige":                RGBHex(0xf5f5dc),
	"bisque":               RGBHex(0xffe4c4),
	"black":                RGBHex(0x000000),
	"blanchedalmond":       RGBHex(0xffebcd),
	"blue":                 RGBHex(0x0000ff),
	"blueviolet":           RGBHex(0x8a2be2),
	"brown":                RGBHex(0xa52a2a),
	"burlywood":            RGBHex(0xdeb887),
	"cadetblue":            RGBHex(0x5f9ea0),
	"chartreuse":           RGBHex(0x7fff00),
	"chocolate":            RGBHex(0xd2691e),
	"coral":                RGBHex(0xff7f50),
	"cornflowerblue":       RGBHex(0x6495ed),
	"cornsilk":             RGBHex(0xfff8dc),
	"crimson":              RGBHex(0xdc143c),
	"cyan":                 RGBHex(0x00ffff),
	"darkblue":             RGBHex(0x00008b),
	"darkcyan":             RGBHex(0x008b8b),
	"darkgoldenrod":        RGBHex(0xb8860b),
	"darkgray":             RGBHex(0xa9a9a9),
	"darkgreen":            RGBHex(0x006400),
	"darkgrey":             RGBHex(0xa9a9a9),
	"darkkhaki":            RGBHex(0xbdb76b),
	"darkmagenta":          RGBHex(0x8b008b),
	"darkolivegreen":       RGBHex(0x556b2f),
	"darkorange":           RGBHex(0xff8c00),
	"darkorchid":           RGBHex(0x9932cc),
	"darkred":              RGBHex(0x8b0000),
	"darksalmon":           RGBHex(0xe9967a),
	"darkseagreen":         RGBHex(0x8fbc8f),
	"darkslateblue":        RGBHex(0x483d8b),
	"darkslategray":        RGBHex(0x2f4f4f),
	"darkslategrey":        RGBHex(0x2f4f4f),
	"darkturquoise":        RGBHex(0x00ced1),
	"darkviolet":           RGBHex(0x9400d3),
	"deeppink":             RGBHex(0xff1493),
	"deepskyblue":          RGBHex(0x00bfff),
	"dimgray":              RGBHex(0x696969),
	"dimgrey":              RGBHex(0x696969),
	"dodgerblue":           RGBHex(0x1e90ff),
	"firebrick":            RGBHex(0xb22222),
	"floralwhite":          RGBHex(0xfffaf0),
	"forestgreen":          RGBHex(0x228b22),
	"fuchsia":              RGBHex(0xff00ff),
	"gainsboro":            RGBHex(0xdcdcdc),
	"ghostwhite":           RGBHex(0xf8f8ff),
	"gold":                 RGBHex(0xffd700),
	"goldenrod":            RGBHex(0xdaa520),
	"gray":                 RGBHex(0x808080),
	"green":                RGBHex(0x008000),
	"greenyellow":          RGBHex(0xadff2f),
	"grey":                 RGBHex(0x808080),
	"honeydew":             RGBHex(0xf0fff0),
	"hotpink":              RGBHex(0xff69b4),
	"indianred":            RGBHex(0xcd5c5c),
	"indigo":               RGBHex(0x4b0082),
	"ivory":                RGBHex(0xfffff0),
	"khaki":                RGBHex(0xf0e68c),
	"lavender":             RGBHex(0xe6e6fa),
	"lavenderblush":        RGBHex(0xfff0f5),
	"lawngreen":            RGBHex(0x7cfc00),
	"lemonchiffon":         RGBHex(0xfffacd),
	"lightblue":            RGBHex(0xadd8e6),
	"lightcoral":           RGBHex(0xf08080),
	"lightcyan":            RGBHex(0xe0ffff),
	"lightgoldenrodyellow": RGBHex(0xfafad2),
	"lightgray":            RGBHex(0xd3d3d3),
	"lightgreen":           RGBHex(0x90ee90),
	"lightgrey":            RGBHex(0xd3d3d3),
	"lightpink":            RGBHex(0xffb6c1),
	"lightsalmon":          RGBHex(0xffa07a),
	"lightseagreen":        RGBHex(0x20b2aa),
	"lightskyblue":         RGBHex(0x87cefa),
	"lightslategray":       RGBHex(0x778899),
	"lightslategrey":       RGBHex(0x778899),
	"lightsteelblue":       RGBHex(0xb0c4de),
	"lightyellow":          RGBHex(0xffffe0),
	"lime":                 RGBHex(0x00ff00),
	"limegreen":            RGBHex(0x32cd32),
	"linen":                RGBHex(0xfaf0e6),
	"magenta":              RGBHex(0xff00ff),
	"maroon":               RGBHex(0x800000),
	"mediumaquamarine":     RGBHex(0x66cdaa),
	"mediumblue":           RGBHex(0x0000cd),
	"mediumorchid":         RGBHex(0xba55d3),
	"mediumpurple":         RGBHex(0x9370db),
	"mediumseagreen":       RGBHex(0x3cb371),
	"mediumslateblue":      RGBHex(0x7b68ee),
	"mediumspringgreen":    RGBHex(0x00fa9a),
	"mediumturquoise":      RGBHex(0x48d1cc),
	"mediumvioletred":      RGBHex(0xc71585),
	"midnightblue":         RGBHex(0x191970),
	"mintcream":            RGBHex(0xf5fffa),
	"mistyrose":            RGBHex(0xffe4e1),
	"moccasin":             RGBHex(0xffe4b5),
	"navajowhite":          RGBHex(0xffdead),
	"navy":                 RGBHex(0x000080),
	"oldlace":              RGBHex(0xfdf5e6),
	"olive":                RGBHex(0x808000),
	"olivedrab":            RGBHex(0x6b8e23),
	"orange":               RGBHex(0xffa500),
	"orangered":            RGBHex(0xff4500),
	"orchid":               RGBHex(0xda70d6),
	"palegoldenrod":        RGBHex(0xeee8aa),
	"palegreen":            RGBHex(0x98fb98),
	"paleturquoise":        RGBHex(0xafeeee),
	"palevioletred":        RGBHex(0xdb7093),
	"papayawhip":           RGBHex(0xffefd5),
	"peachpuff":            RGBHex(0xffdab9),
	"peru":                 RGBHex(0xcd853f),
	"pink":                 RGBHex(0xffc0cb),
	"plum":                 RGBHex(0xdda0dd),
	"powderblue":           RGBHex(0xb0e0e6),
	"purple":               RGBHex(0x800080),
	"rebeccapurple":        RGBHex(0x663399),
	"red":                  RGBHex(0xff0000),
	"rosybrown":            RGBHex(0xbc8f8f),
	"royalblue":            RGBHex(0x4169e1),
	"saddlebrown":          RGBHex(0x8b4513),
	"salmon":               RGBHex(0xfa8072),
	"sandybrown":           RGBHex(0xf4a460),
	"seagreen":             RGBHex(0x2e8b57),
	"seashell":             RGBHex(0xfff5ee),
	"sienna":               RGBHex(0xa0522d),
	"silver":               RGBHex(0xc0c0c0),
	"skyblue":              RGBHex(0x87ceeb),
	"slateblue":            RGBHex(0x6a5acd),
	"slategray":            RGBHex(0x708090),
	"slategrey":            RGBHex(0x708090),
	"snow":                 RGBHex(0xfffafa),
	"springgreen":          RGBHex(0x00ff7f),
	"steelblue":            RGBHex(0x4682b4),
	"tan":                  RGBHex(0xd2b48c),
	"teal":                 RGBHex(0x008080),
	"thistle":              RGBHex(0xd8bfd8),
	"tomato":               RGBHex(0xff6347),
	"turquoise":            RGBHex(0x40e0d0),
	"violet":               RGBHex(0xee82ee),
	"wheat":                RGBHex(0xf5deb3),
	"white":                RGBHex(0xffffff),
	"whitesmoke":           RGBHex(0xf5f5f5),
	"yellow":               RGBHex(0xffff00),
	"yellowgreen":          RGBHex(0x9acd32),
}

// --------------------------------------------------------------------------
// highlight.go

// Syntax highlighting.
//
// A highlighter is handed the line and marks stretches of it with a style. The
// marks land in an attribute buffer, one attribute per byte, which the edit
// loop then draws.
//
// Ported from isocline/src/highlight.c.

// maxBraceNesting is how deep brace matching will go before it gives up.
const maxBraceNesting = 64

// Highlighter marks up a line. It is given the line and an environment to
// mark it through.
type Highlighter interface {
	Highlight(l *LineStyle)
}

// HighlighterFunc makes a Highlighter out of an ordinary function.
type HighlighterFunc func(l *LineStyle)

// Highlight satisfies Highlighter.
func (f HighlighterFunc) Highlight(l *LineStyle) { f(l) }

// LineStyle is a line and the attributes drawn over it.
//
// A highlighter is handed one of these, reads the line with Text, and calls
// Style or StyleRunes on the stretches it recognises. Anything it does
// not touch keeps the attributes of the terminal.
type LineStyle struct {
	// What is being marked, and where the marks go.
	input string
	attrs *attrBuf

	// bb resolves a style name to attributes.
	bb *bbCode

	// The last character position that was turned into a byte position.
	// Highlighters walk a line from the front, so remembering one place stops
	// the walk being repeated from the start every time.
	cachedUPos int
	cachedCPos int
}

// runHighlight fills attrs with one attribute per byte of s and then lets the
// highlighter mark it up. A nil highlighter leaves the line unmarked.
func runHighlight(bb *bbCode, s string, attrs *attrBuf, fn Highlighter) {
	if len(s) == 0 {
		return
	}
	attrs.setAt(0, len(s), attr{})
	if fn == nil {
		return
	}
	fn.Highlight(&LineStyle{input: s, attrs: attrs, bb: bb})
}

// Text returns the line being marked up.
//
// A highlighter reads the line from here rather than being handed it
// separately, so that there is one copy of it and no way for the two to
// disagree.
func (l *LineStyle) Text() string {
	if l == nil {
		return ""
	}
	return l.input
}

// posAdjust turns a position and a count given in characters into one given in
// bytes. A negative value means characters, which is how a caller that counts
// in characters rather than bytes says so.
//
// Nothing reaches the negative position case, because the one public entry
// point refuses a negative position before it gets here. Only a negative count
// can arrive.
func (l *LineStyle) posAdjust(pos, count int) (int, int) {
	if pos >= len(l.input) {
		return pos, count
	}
	if pos >= 0 && count >= 0 {
		return pos, count
	}
	if pos < 0 {
		upos := -pos
		cpos, ucount := 0, 0
		if l.cachedUPos <= upos {
			ucount, cpos = l.cachedUPos, l.cachedCPos
		}
		for ucount < upos {
			next, _ := nextOfs([]byte(l.input), cpos)
			if next <= 0 {
				return pos, count
			}
			ucount++
			cpos += next
		}
		pos = cpos
		l.cachedUPos, l.cachedCPos = upos, cpos
	}
	if count < 0 {
		want := -count
		ucount, clen := 0, 0
		for ucount < want {
			next, _ := nextOfs([]byte(l.input), pos+clen)
			if next <= 0 {
				return pos, count
			}
			ucount++
			clen += next
		}
		count = clen
		if l.cachedCPos == pos {
			l.cachedUPos += ucount
			l.cachedCPos += clen
		}
	}
	return pos, count
}

// mark lays a over count bytes from pos.
func (l *LineStyle) mark(pos, count int, a attr) {
	pos, count = l.posAdjust(pos, count)
	if pos < 0 || count <= 0 {
		return
	}
	l.attrs.updateAt(pos, count, a)
}

// Style marks count bytes from pos with a named style, such as "keyword" or
// a color name such as "red".
//
// A negative count means a number of characters rather than bytes, which is
// what a caller counting characters wants. A negative pos is refused, which is
// what the C does.
func (l *LineStyle) Style(pos, count int, style string) {
	if style == "" || pos < 0 {
		return
	}
	l.mark(pos, count, l.bb.style(style))
}

// StyleRunes marks count characters from pos with a named style.
//
// pos is still counted in bytes, because that is where the caller found the
// word; only the length is counted in characters. The name carries the unit
// because characters are the exception here: everywhere else in this package
// a count or an offset is in bytes, which is what indexes a Go string, and
// goes unnamed for the same reason strings.Index does not name it.
func (l *LineStyle) StyleRunes(pos, count int, style string) {
	if style == "" || pos < 0 {
		return
	}
	l.mark(pos, -count, l.bb.style(style))
}

// StyleMarkup styles the stretch of line that s covers, taking the styles
// from markup that spells out the same text.
//
// It is for a caller that would rather describe a whole line at once than a
// stretch at a time: pass the text and the same text with tags around it, and
// the tags decide the attributes.
//
// Only the attributes are taken. The text the markup produces is thrown away,
// and when the two disagree in length the marks simply run out and the rest of
// the line keeps what it had. The C writes a debug line about that, which the
// port drops because nothing reads it.
func (l *LineStyle) StyleMarkup(s, markup string) {
	if s == "" {
		return
	}
	var out buffer
	var attrs attrBuf
	l.bb.appendTo(markup, &out, &attrs)
	for i := range len(s) {
		l.attrs.updateAt(i, 1, attrs.at(i))
	}
}

//-------------------------------------------------------------
// Brace matching
//-------------------------------------------------------------

// openBrace is a brace that has been opened and not yet closed.
type openBrace struct {
	// closer is the brace that would close this one.
	closer byte

	// pos is where the opening brace is.
	pos int

	// atCursor says the cursor sits just after the opening brace.
	atCursor bool
}

// braceOpener returns the closing brace that c opens, and whether c opens one
// at all. The braces are given in pairs, as in "()[]{}".
func braceOpener(braces string, c byte) (byte, bool) {
	for b := 0; b+1 < len(braces); b += 2 {
		if c == braces[b] {
			return braces[b+1], true
		}
	}
	return 0, false
}

// isBraceCloser reports whether c closes a brace.
func isBraceCloser(braces string, c byte) bool {
	for b := 1; b < len(braces); b += 2 {
		if c == braces[b] {
			return true
		}
	}
	return false
}

// highlightMatchBraces marks the brace under the cursor and the one that goes
// with it, and marks a brace that has no partner as an error.
//
// An opening brace left unclosed at the end of the line is not marked, because
// the line is probably still being typed.
func highlightMatchBraces(s string, attrs *attrBuf, cursorPos int, braces string, matchAttr, errorAttr attr) {
	var open [maxBraceNesting + 1]openBrace
	nesting := 0
	for i := range len(s) {
		c := s[i]
		if closer, ok := braceOpener(braces, c); ok {
			if nesting >= maxBraceNesting {
				return // too deep to be worth following
			}
			open[nesting] = openBrace{closer: closer, pos: i, atCursor: i == cursorPos-1}
			nesting++
			continue
		}
		if !isBraceCloser(braces, c) {
			continue
		}
		if nesting <= 0 {
			attrs.updateAt(i, 1, errorAttr)
			continue
		}
		// One wrong opening brace can be stepped over, when the one before it
		// is the partner. That turns "([)" into a single error rather than
		// making everything after it wrong.
		if open[nesting-1].closer != c && nesting > 1 && open[nesting-2].closer == c {
			attrs.updateAt(open[nesting-1].pos, 1, errorAttr)
			nesting--
		}
		if open[nesting-1].closer != c {
			attrs.updateAt(i, 1, errorAttr)
			continue
		}
		nesting--
		if i == cursorPos-1 || (open[nesting].atCursor && open[nesting].pos != i-1) {
			attrs.updateAt(open[nesting].pos, 1, matchAttr)
			attrs.updateAt(i, 1, matchAttr)
		}
	}
}

// findMatchingBrace returns the position just after the brace that goes with
// the one at the cursor, or -1 when there is none, and whether the whole line
// is balanced.
func findMatchingBrace(s string, cursorPos int, braces string) (int, bool) {
	var open [maxBraceNesting + 1]openBrace
	nesting := 0
	match := -1
	balanced := true
	for i := range len(s) {
		c := s[i]
		if closer, ok := braceOpener(braces, c); ok {
			if nesting >= maxBraceNesting {
				return -1, false
			}
			open[nesting] = openBrace{closer: closer, pos: i, atCursor: i == cursorPos-1}
			nesting++
			continue
		}
		if !isBraceCloser(braces, c) {
			continue
		}
		switch {
		case nesting <= 0:
			balanced = false
		case open[nesting-1].closer != c:
			balanced = false
		default:
			nesting--
			if i == cursorPos-1 {
				match = open[nesting].pos + 1
			} else if open[nesting].atCursor {
				match = i + 1
			}
		}
	}
	if nesting != 0 {
		balanced = false
	}
	return match, balanced
}
