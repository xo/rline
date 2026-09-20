// Text attributes, and reading them out of an SGR escape sequence.
//
// An Attr carries a foreground color, a background color, and four switches.
// Each switch is three valued: turn it on, turn it off, or say nothing about
// it. That third value is what lets one attribute be laid over another, which
// is how nested markup builds up a final style.
//
// The C code packs all of this into a 64 bit union of bit fields, so that it
// can compare two attributes with one integer compare. A Go struct of
// comparable fields compares with == and needs no packing, so the port drops
// it. Nothing outside attr.c depended on the packed value.
//
// Ported from isocline/src/attr.c.

package ansi

import "strconv"

// Flag is a three valued switch: on, off, or nothing said.
type Flag int8

// Flag values. FlagUnset means the attribute says nothing about the switch, so
// whatever is underneath shows through.
const (
	FlagOff   Flag = -1
	FlagUnset Flag = 0
	FlagOn    Flag = 1
)

// Attr is a set of text attributes.
//
// The zero Attr says nothing at all, which is not the same as saying "use the
// terminal's default": that is [DefaultAttr]. An Fg or Bg of [None] means the
// same as the zero value, so an Attr cannot ask for a color to be unset, only
// for it to be left alone or set to [Default].
//
// The C color fields are 28 bits wide, so a color above that would be cut short
// there. No path produces one, because every color is either a palette code or
// an RGB value with bit 24 set, which needs 25 bits.
//
// Every field is comparable, and callers do compare an Attr with ==, so
// nothing that breaks that may be added.
type Attr struct {
	// Fg is the text color and Bg the background.
	Fg Code
	Bg Code

	Bold      Flag
	Italic    Flag
	Reverse   Flag
	Underline Flag
}

// DefaultAttr returns the attributes that turn everything back to the default
// of the terminal. It is a function rather than a variable so that a caller
// cannot change what every other caller gets.
func DefaultAttr() Attr {
	return Attr{
		Fg:        Default,
		Bg:        Default,
		Bold:      FlagOff,
		Italic:    FlagOff,
		Reverse:   FlagOff,
		Underline: FlagOff,
	}
}

// IsZero reports whether the attributes say nothing at all.
//
// This is a == against the zero Attr, given a name so that adding a field
// cannot quietly change what every caller meant.
func (a Attr) IsZero() bool {
	return a == Attr{}
}

// Merge lays b over a and returns the result. Every part of b that says
// nothing leaves the matching part of a alone, so b wins wherever b speaks.
func (a Attr) Merge(b Attr) Attr {
	if b.Fg != None {
		a.Fg = b.Fg
	}
	if b.Bg != None {
		a.Bg = b.Bg
	}
	if b.Bold != FlagUnset {
		a.Bold = b.Bold
	}
	if b.Italic != FlagUnset {
		a.Italic = b.Italic
	}
	if b.Reverse != FlagUnset {
		a.Reverse = b.Reverse
	}
	if b.Underline != FlagUnset {
		a.Underline = b.Underline
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
//
// A number too large for an int is not defined by C and not defined here
// either. This returns false for it, where sscanf would store something.
func sgrNextPar(s string, i int) (int, int, bool) {
	n := 0
	for sgrIsDigit(s, i+n) {
		n++
	}
	if n == 0 {
		return 0, i, true
	}
	par, err := strconv.Atoi(s[i : i+n])
	return par, i + n, err == nil
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

// ParseSGR reads a Select Graphic Rendition parameter string, which is the
// part of an escape sequence between "\x1b[" and the final "m". A trailing "m"
// is harmless, because it is not a digit.
//
// An unknown parameter is skipped. The C code writes a debug line for it, and
// the port drops that, because nothing reads it.
func ParseSGR(s string) Attr {
	var a Attr
	for i := 0; i < len(s) && s[i] != 0; i++ {
		cmd, next, ok := sgrNextPar(s, i)
		i = next
		if !ok {
			continue
		}
		switch {
		case cmd == 0:
			a = DefaultAttr()
		case cmd == 1:
			a.Bold = FlagOn
		case cmd == 3:
			a.Italic = FlagOn
		case cmd == 4:
			a.Underline = FlagOn
		case cmd == 7:
			a.Reverse = FlagOn
		case cmd == 22:
			a.Bold = FlagOff
		case cmd == 23:
			a.Italic = FlagOff
		case cmd == 24:
			a.Underline = FlagOff
		case cmd == 27:
			a.Reverse = FlagOff
		case cmd == 39:
			a.Fg = Default
		case cmd == 49:
			a.Bg = Default
		case cmd >= 30 && cmd <= 37:
			a.Fg = Black + Code(cmd-30)
		case cmd >= 40 && cmd <= 47:
			a.Bg = Black + Code(cmd-40)
		case cmd >= 90 && cmd <= 97:
			a.Fg = DarkGray + Code(cmd-90)
		case cmd >= 100 && cmd <= 107:
			a.Bg = DarkGray + Code(cmd-100)
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
func sgrExtended(s string, i, cmd int, a *Attr) int {
	par, i, ok := sgrNextPar(s, i)
	if !ok {
		return i
	}
	set := func(c Code) {
		if cmd == 38 {
			a.Fg = c
		} else {
			a.Bg = c
		}
	}
	switch {
	case par == 5 && sgrIsSep(s, i):
		i++
		if par, i, ok = sgrNextPar(s, i); ok && par >= 0 && par <= 0xFF {
			set(FromANSI256(par))
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

// ParseEscapeSGR reads a whole escape sequence, "\x1b[" then parameters then
// "m". Anything else gives no attributes at all.
//
// This is not the same as calling [ParseSGR] on the middle of the sequence:
// the check that the sequence is an SGR at all lives here, where the recorded
// C calls reach it.
func ParseEscapeSGR(s string) Attr {
	if len(s) <= 2 || s[0] != 0x1B || s[1] != '[' || s[len(s)-1] != 'm' {
		return Attr{}
	}
	return ParseSGR(s[2:])
}
