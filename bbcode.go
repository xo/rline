// Markup and highlighting: how a line is marked with attributes.
//
// The file reads bottom up. An attribute is an ansi.Attr and a run of them is
// an ansi.AttrBuf, both from the ansi package, which also holds the color an
// attribute carries and the reduction to what the terminal can show. Markup,
// written like [red]text[/red], is the way a program names a set of attributes
// in text. A highlighter marks stretches of a line and writes no tags at all.
//
// What is here is therefore the naming rather than the thing named: which
// words mean which attributes, and where a marked line meets rline's own text
// buffer. That is one subject, which is why it is one file.
//
// Ported from isocline/src/bbcode.c and highlight.c.

package rline

import (
	"strings"

	"github.com/xo/rline/ansi"
	"github.com/xo/rline/internal/text"
)

// --------------------------------------------------------------------------
// attrbuf.go

// appendMarked adds s to the text buffer and gives every byte of it the attribute
// a. It returns the length of the text buffer afterwards.
//
// This is the one place an attribute buffer meets rline's own text buffer, so
// it stays here rather than in ansi, which knows nothing about a line.
//
// The attribute buffer may be nil, which appends the text and records no
// attributes.
func appendMarked(ab *ansi.AttrBuf, b *text.Buffer, s string, a ansi.Attr) int {
	if s == "" {
		return b.Length()
	}
	ab.SetAt(ab.Length(), len(s), a)
	return b.AppendString(s)
}

// --------------------------------------------------------------------------
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
	attr ansi.Attr

	// width is the width the tag asks for.
	width widthSpec

	// pos is where the text this tag wraps starts in the output.
	pos int
}

// bbStyle is a named set of attributes.
type bbStyle struct {
	name string
	attr ansi.Attr
}

// bbCode turns markup into text and attributes.
type bbCode struct {
	// term is where print writes.
	term *term

	// tags is the stack of open tags, and styles are the ones defined by name.
	tags   []bbTag
	styles []bbStyle

	// Working buffers, kept so that printing does not allocate each time.
	out      text.Buffer
	outAttrs ansi.AttrBuf
	vout     text.Buffer
}

// newBBCode returns a bbCode that prints to term.
func newBBCode(t *term) *bbCode {
	return &bbCode{term: t}
}

// builtinStyles are the styles that need no definition.
var builtinStyles = []bbStyle{
	{"b", ansi.Attr{Bold: ansi.FlagOn}},
	{"r", ansi.Attr{Reverse: ansi.FlagOn}},
	{"u", ansi.Attr{Underline: ansi.FlagOn}},
	{"i", ansi.Attr{Italic: ansi.FlagOn}},
	{"em", ansi.Attr{Bold: ansi.FlagOn}},
	{"url", ansi.Attr{Underline: ansi.FlagOn}},
}

// styleAdd gives a name to a set of attributes.
func (bb *bbCode) styleAdd(name string, a ansi.Attr) {
	bb.styles = append(bb.styles, bbStyle{name: name, attr: a})
}

// styleDef gives a name to the attributes that the markup spec describes.
func (bb *bbCode) styleDef(name, spec string) {
	bb.styleAdd(name, bb.parseTagContent(spec).attr)
}

// style returns the attributes that a style name stands for.
func (bb *bbCode) style(name string) ansi.Attr {
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
func (bb *bbCode) openTag(outPos int, t bbTag, current ansi.Attr) ansi.Attr {
	bb.pushTag(bbTag{name: t.name, attr: current, width: t.width, pos: outPos})
	return current.Merge(t.attr)
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

// updateWidth reads a width, written as
// <width>;<left|center|right>;<fill>;<cut>.
//
// A width that is not a number leaves everything at nothing, except the fill,
// which keeps the default it was given.
func updateWidth(out *widthSpec, defaultFill byte, value string) {
	w := widthSpec{fill: defaultFill}
	defer func() { *out = w }()
	n, ok := text.Atoz(value)
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
	case len(f) == 4 && text.HasPrefixFold(f, "left"):
		w.align = alignLeft
	case len(f) == 5 && text.HasPrefixFold(f, "right"):
		w.align = alignRight
	case len(f) == 6 && text.HasPrefixFold(f, "center"):
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
	if (len(f) == 2 && text.HasPrefixFold(f, "on")) || f == "1" {
		w.dots = true
	}
}

// updateProperty sets one named property on a tag. It returns the name the
// property is known by, or an empty string when the name is not a property at
// all.
func updateProperty(t *bbTag, name, value string) string {
	// A switch is turned on whatever the value says. The C means to compare
	// the value against "on", "true" and "1", but it uses each strcmp as a
	// truth value rather than testing it against zero, and strcmp answers zero
	// when the two are equal. So the first branch is taken for every value
	// that is not equal to all three at once, which no value is. [bold=off]
	// therefore turns bold on.
	//
	// A color of ansi.None names no color, and leaves what is there alone.
	switch name {
	case "bold":
		t.attr.Bold = ansi.FlagOn
	case "italic":
		t.attr.Italic = ansi.FlagOn
	case "underline":
		t.attr.Underline = ansi.FlagOn
	case "reverse":
		t.attr.Reverse = ansi.FlagOn
	case "color":
		if c := ansi.ParseColor(value); c != ansi.None {
			t.attr.Fg = c
		}
	case "bgcolor":
		if c := ansi.ParseColor(value); c != ansi.None {
			t.attr.Bg = c
		}
	case "ansi-sgr":
		t.attr = t.attr.Merge(ansi.ParseSGR(value))
	case "ansi-color":
		if c := ansi.ParseANSI256(value); c != ansi.None {
			t.attr.Fg = c
		}
	case "ansi-bgcolor":
		if c := ansi.ParseANSI256(value); c != ansi.None {
			t.attr.Bg = c
		}
	case "width":
		updateWidth(&t.width, ' ', value)
	case "max-width":
		updateWidth(&t.width, 0, value)
		return "width"
	default:
		// Not a property. The caller goes on to the styles and color names.
		return ""
	}
	return name
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
			t.attr = t.attr.Merge(bb.styles[i].attr)
			return
		}
	}
	for _, s := range builtinStyles {
		if s.name == name {
			t.attr = t.attr.Merge(s.attr)
			return
		}
	}
	if c, ok := ansi.ColorByName(name); ok {
		var ca ansi.Attr
		if useBgColor {
			ca.Bg = c
		} else {
			ca.Fg = c
		}
		t.attr = t.attr.Merge(ca)
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
		b[i] = text.ASCIILower(b[i])
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
	if idEnd-idStart == 2 && text.CompareFoldN(s[idStart:], "on", 2) == 0 &&
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
	bb.term.setAttr(bb.openTag(0, t, bb.term.attr))
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
func restrictWidth(start int, width widthSpec, out *text.Buffer, outAttrs *ansi.AttrBuf) {
	if width.w <= 0 {
		return
	}
	s := out.Bytes()[start:]
	length := len(s)
	w := text.StrWidth(s)
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
			ndel := text.SkipUntilFit(s, inner)
			out.DeleteAt(start, ndel)
			outAttrs.DeleteAt(start, ndel)
			if inner < width.w {
				out.InsertAt("...", start)
				outAttrs.InsertAt(start, 3, outAttrs.At(start))
			}
			return
		}
		count := text.TakeWhileFit(s, inner)
		out.DeleteAt(start+count, length-count)
		outAttrs.DeleteAt(start+count, length-count)
		if inner < width.w {
			appendMarked(outAttrs, out, "...", outAttrs.At(start))
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
			a := outAttrs.At(start)
			for range padLeft {
				out.InsertByteAt(width.fill, start)
			}
			outAttrs.InsertAt(start, padLeft, a)
		}
		if padRight > 0 {
			a := outAttrs.At(out.Length() - 1)
			for range padRight {
				appendMarked(outAttrs, out, string(width.fill), a)
			}
		}
	}
}

//-------------------------------------------------------------
// Turning markup into text and attributes
//-------------------------------------------------------------

// processTag handles one tag at i and returns how many bytes it used.
func (bb *bbCode) processTag(s string, i, nestingBase int, out *text.Buffer, outAttrs *ansi.AttrBuf, cur *ansi.Attr) int {
	var t bbTag
	name, open, isPre, end := bb.parseTag(&t, s, i)
	switch {
	case open && !isPre:
		*cur = bb.openTag(out.Length(), t, *cur)
	case open:
		// A "[!name]" tag holds everything up to its closing tag unread, so
		// markup inside it is text.
		a := cur.Merge(t.attr)
		closing := "[/" + name + "]"
		if at := strings.Index(s[end:], closing); at < 0 {
			appendMarked(outAttrs, out, s[end:], a)
			end = len(s)
		} else {
			appendMarked(outAttrs, out, s[end:end+at], a)
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
func (bb *bbCode) appendTo(s string, out *text.Buffer, outAttrs *ansi.AttrBuf) {
	var a ansi.Attr
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
			appendMarked(outAttrs, out, s[i:i+n], a)
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
				appendMarked(outAttrs, out, s[i+1:i+2], a)
				i += 2
				continue
			}
			appendMarked(outAttrs, out, s[i:i+1], a)
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
	bb.term.writeFormatted(bb.out.String(), bb.outAttrs.Extend(bb.out.Length()))
	bb.outAttrs.Clear()
	bb.out.Clear()
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
	w := text.StrWidth(bb.vout.Bytes())
	bb.vout.Clear()
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

// --------------------------------------------------------------------------
// highlight.go

// Syntax highlighting.
//
// A highlighter is handed the line and marks stretches of it with a style. The
// marks land in an attribute buffer, one attribute per byte, which the edit
// loop then draws.
//
// Ported from isocline/src/highlight.c.

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
	attrs *ansi.AttrBuf

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
func runHighlight(bb *bbCode, s string, attrs *ansi.AttrBuf, fn Highlighter) {
	if len(s) == 0 {
		return
	}
	attrs.SetAt(0, len(s), ansi.Attr{})
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
			next, _ := text.NextOfs([]byte(l.input), cpos)
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
			next, _ := text.NextOfs([]byte(l.input), pos+clen)
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
func (l *LineStyle) mark(pos, count int, a ansi.Attr) {
	pos, count = l.posAdjust(pos, count)
	if pos < 0 || count <= 0 {
		return
	}
	l.attrs.UpdateAt(pos, count, a)
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
	var out text.Buffer
	var attrs ansi.AttrBuf
	l.bb.appendTo(markup, &out, &attrs)
	for i := range len(s) {
		l.attrs.UpdateAt(i, 1, attrs.At(i))
	}
}

// highlightMatchBraces marks the brace under the cursor and the one that goes
// with it, and marks a brace that has no partner as an error.
//
// An opening brace left unclosed at the end of the line is not marked, because
// the line is probably still being typed.
func highlightMatchBraces(s string, attrs *ansi.AttrBuf, cursorPos int, braces string, matchAttr, errorAttr ansi.Attr) {
	var open [text.MaxBraceNesting + 1]text.OpenBrace
	nesting := 0
	for i := range len(s) {
		c := s[i]
		if closer, ok := text.BraceOpener(braces, c); ok {
			if nesting >= text.MaxBraceNesting {
				return // too deep to be worth following
			}
			open[nesting] = text.OpenBrace{Closer: closer, Pos: i, AtCursor: i == cursorPos-1}
			nesting++
			continue
		}
		if !text.IsBraceCloser(braces, c) {
			continue
		}
		if nesting <= 0 {
			attrs.UpdateAt(i, 1, errorAttr)
			continue
		}
		// One wrong opening brace can be stepped over, when the one before it
		// is the partner. That turns "([)" into a single error rather than
		// making everything after it wrong.
		if open[nesting-1].Closer != c && nesting > 1 && open[nesting-2].Closer == c {
			attrs.UpdateAt(open[nesting-1].Pos, 1, errorAttr)
			nesting--
		}
		if open[nesting-1].Closer != c {
			attrs.UpdateAt(i, 1, errorAttr)
			continue
		}
		nesting--
		if i == cursorPos-1 || (open[nesting].AtCursor && open[nesting].Pos != i-1) {
			attrs.UpdateAt(open[nesting].Pos, 1, matchAttr)
			attrs.UpdateAt(i, 1, matchAttr)
		}
	}
}
