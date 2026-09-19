package rline

import (
	"strconv"
	"strings"
)

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
			*field = RGB(v)
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
//
//nolint:unused // wired up by editline and the public API
func (bb *bbCode) styleOpen(spec string) {
	t := bb.parseTagContent(spec)
	bb.term.setAttr(bb.openTag(0, t, bb.term.getAttr()))
}

// styleClose closes the innermost style on the terminal.
//
//nolint:unused // wired up by editline and the public API
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
//
//nolint:unused // wired up by editline and the public API
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
