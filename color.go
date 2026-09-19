// Colors and the attributes that carry them.
//
// A Color is either an ANSI palette code or a 24 bit RGB value, and the
// reduction at the end of this file turns one into whatever the terminal at
// hand can actually show.
//
// Ported from isocline/src/attr.c and isocline/src/term_color.c.

package rline

import (
	"fmt"
	"image/color"
)

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

// isRGB and rgb are used when term_color.c reduces a color to what a terminal
// accepts, which is step 6.
//
// isRGB reports whether the color carries an RGB value rather than a palette
// code.
//
//nolint:unused // wired up when term_color.c lands
func (c Color) isRGB() bool {
	return c >= rgbFlag
}

// rgb returns the red, green and blue components of an RGB color.
//
//nolint:unused // wired up when term_color.c lands
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

// at is used by the bbcode parser, which is step 7.
//
// at returns the attribute at pos, or nothing when pos is outside the buffer.
//
// The C code tests pos against the count with the wrong comparison, so at the
// one position just past the end it reads a slot it never wrote. That is
// undefined behavior rather than a wrong answer, so there is nothing to
// reproduce. This returns the empty attribute there.
//
//nolint:unused // wired up when bbcode.c lands
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
