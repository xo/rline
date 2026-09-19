package rline

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
			set(RGBX(r, g, b))
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
