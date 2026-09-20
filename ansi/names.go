// Color names, and reading a color out of written text.
//
// A color can be written as one of the names below, as a hex value such as
// "#ff0000", or as an index into the 256 color palette. The C reads each of
// these with sscanf, and the quirks of that are kept here rather than fixed.
//
// Ported from isocline/src/bbcode_colors.c and the color reading in bbcode.c.

package ansi

import "strconv"

// ParseColor reads a color written as text: the empty string or "none" for no
// color, a hex value such as "#ff0000", or one of the names in [ColorByName].
// Anything else answers [None].
//
// None is what a caller laying one set of attributes over another reads as
// "say nothing about the color", so a value that names no color leaves
// whatever is underneath alone. This is not the same as [FromColor], which
// takes a color from image/color rather than text.
func ParseColor(s string) Code {
	if s == "" || s == "none" {
		return None
	}
	if s[0] == '#' {
		// The C reads this with sscanf, which takes as many hex digits as it
		// finds and does not widen a short value, so "#f00" is 0xf00 rather
		// than 0xff0000.
		if v, ok := scanHex(s[1:]); ok {
			return RGBHex(v)
		}
		return None
	}
	if c, ok := ColorByName(s); ok {
		return c
	}
	return None
}

// ParseANSI256 reads an index into the 256 color palette, written in decimal.
// An index outside 0 to 256, or text that is not a number, answers [None].
//
// This is [FromANSI256] with the index read out of text, and it takes the same
// index of 256 that the C allows although the palette holds 256 entries.
func ParseANSI256(s string) Code {
	if n, ok := atoz(s); ok && n >= 0 && n <= 256 {
		return FromANSI256(n)
	}
	return None
}

// ColorByName returns the color a name stands for, and whether the name is
// one. The names that start with "ansi-" give a palette code, and the rest
// give an RGB value.
func ColorByName(name string) (Code, bool) {
	c, ok := htmlColors[name]
	return c, ok
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

// atoz reads one decimal number from the front of s, the way sscanf reads %d:
// any leading space, an optional sign, then digits, and whatever follows is
// ignored. It reports false when there is no number, and when the number is
// too large for an int, which C does not define and this does not either.
//
// This is the same reading as rline's own atoz, kept here so that the package
// depends on nothing outside the standard library.
func atoz(s string) (int, bool) {
	i := 0
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == digits {
		return 0, false
	}
	v, err := strconv.Atoi(s[start:i])
	if err != nil {
		return 0, false
	}
	return v, true
}

// isSpace reports whether c is one of the six bytes C calls a space.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

// htmlColors maps a color name to the color it stands for. The names that
// start with "ansi-" give a palette code, and the rest give an RGB value.
var htmlColors = map[string]Code{
	"aliceblue":            RGBHex(0xf0f8ff),
	"ansi-aqua":            Aqua,
	"ansi-black":           Black,
	"ansi-blue":            Blue,
	"ansi-cyan":            Cyan,
	"ansi-darkgray":        DarkGray,
	"ansi-darkgrey":        DarkGray,
	"ansi-default":         Default,
	"ansi-fuchsia":         Fuchsia,
	"ansi-gray":            Gray,
	"ansi-green":           Green,
	"ansi-grey":            Gray,
	"ansi-lightgray":       LightGray,
	"ansi-lightgrey":       LightGray,
	"ansi-lime":            Lime,
	"ansi-magenta":         Magenta,
	"ansi-maroon":          Maroon,
	"ansi-navy":            Navy,
	"ansi-olive":           Olive,
	"ansi-purple":          Purple,
	"ansi-red":             Red,
	"ansi-silver":          Silver,
	"ansi-teal":            Teal,
	"ansi-white":           White,
	"ansi-yellow":          Yellow,
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
