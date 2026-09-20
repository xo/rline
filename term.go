package rline

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/xo/rline/ansi"
	"github.com/xo/rline/internal/text"
)

// Writing to a terminal.
//
// Everything the editor draws goes through here. Output is collected in a
// buffer and written in one go, which stops the cursor from flickering while a
// line is redrawn.
//
// Ported from isocline/src/term.c.

// bufferMode says when collected output is written out.
type bufferMode int

// bufferMode values.
const (
	unbuffered   bufferMode = iota // write at once
	lineBuffered                   // write when a line ends
	buffered                       // write when asked, or when the buffer fills
)

// flushAt is how much output may collect before it is written out anyway.
const flushAt = 4000

// sizer reports the size of a terminal. A term with no sizer keeps the size it
// was given.
type sizer interface {
	// size returns the width and height in characters, and whether it could
	// find out at all.
	size() (int, int, bool)
}

// term writes to a terminal.
type term struct {
	// Where the bytes go, and what is waiting to go there.
	out  io.Writer
	sz   sizer
	buf  text.Buffer
	mode bufferMode

	// What the terminal can do.
	width   int
	height  int
	palette ansi.Palette
	nocolor bool
	silent  bool
	isUTF8  bool

	// The attributes the terminal is showing now. This is kept up to date by
	// the write path rather than by setAttr, because setAttr sets attributes
	// by writing escape sequences, and appendEsc reads every sequence that
	// goes past. See setAttr.
	attr ansi.Attr

	// rawEnabled counts how many times raw mode was asked for. On a Unix
	// system the tty does the work and this only counts.
	rawEnabled int
}

// termOptions are the choices newTerm cannot work out for itself.
type termOptions struct {
	// NoColor turns off every color, whatever the terminal supports.
	NoColor bool

	// Silent turns off the beep.
	Silent bool

	// IsUTF8 says whether the terminal reads UTF-8.
	IsUTF8 bool

	// Sizer reports the size of the terminal, and may be nil.
	Sizer sizer
}

// newTerm returns a term that writes to out.
func newTerm(out io.Writer, opts termOptions) *term {
	t := &term{
		out:     out,
		sz:      opts.Sizer,
		mode:    lineBuffered,
		width:   80,
		height:  25,
		palette: ansi.PaletteANSI16, // near enough to universal
		nocolor: opts.NoColor,
		silent:  opts.Silent,
		isUTF8:  opts.IsUTF8,
		attr:    ansi.DefaultAttr(),
	}
	if os.Getenv("NO_COLOR") != "" {
		t.nocolor = true
	}
	if !t.nocolor {
		t.palette = detectPalette()
	}
	// COLUMNS and LINES give a better first guess than the defaults.
	if v, ok := text.Atoz(os.Getenv("COLUMNS")); ok {
		t.width = v
	}
	if v, ok := text.Atoz(os.Getenv("LINES")); ok {
		t.height = v
	}
	t.updateDim()
	t.attrReset()
	return t
}

// detectPalette works out how much color the terminal supports, from the
// environment. COLORTERM decides when it is set, then a few terminals that
// are known by their own variables, then TERM.
func detectPalette() ansi.Palette {
	colorterm := os.Getenv("COLORTERM")
	switch {
	case containsAny(colorterm, "24bit", "truecolor", "direct"):
		return ansi.PaletteTrueColor
	case containsAny(colorterm, "8bit", "256color"):
		return ansi.PaletteANSI256
	case containsAny(colorterm, "4bit", "16color"):
		return ansi.PaletteANSI16
	case containsAny(colorterm, "3bit", "8color"):
		return ansi.PaletteANSI8
	case containsAny(colorterm, "1bit", "nocolor", "monochrome"):
		return ansi.PaletteMono
	case os.Getenv("WT_SESSION") != "":
		return ansi.PaletteTrueColor // Windows Terminal
	case os.Getenv("ITERM_SESSION_ID") != "":
		return ansi.PaletteTrueColor // iTerm2
	case os.Getenv("VSCODE_PID") != "":
		return ansi.PaletteTrueColor // the terminal inside VS Code
	}
	eterm := os.Getenv("TERM")
	switch {
	// The C tests COLORTERM for "24bit" a second time here. It cannot be
	// reached, because the first test above already caught it.
	case containsAny(eterm, "truecolor", "direct"):
		return ansi.PaletteTrueColor
	case containsAny(eterm, "alacritty", "kitty"):
		return ansi.PaletteTrueColor
	case containsAny(eterm, "256color", "gnome"):
		return ansi.PaletteANSI256
	case containsAny(eterm, "16color"):
		return ansi.PaletteANSI16
	case containsAny(eterm, "8color"):
		return ansi.PaletteANSI8
	case containsAny(eterm, "monochrome", "nocolor", "dumb"):
		return ansi.PaletteMono
	}
	return ansi.PaletteANSI16
}

// containsAny reports whether s holds any of the given parts.
func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// isInteractive reports whether the terminal can be edited on.
//
// The C passes its two arguments to strstr the wrong way round, so it asks
// whether TERM is part of the list rather than whether the list holds TERM.
// The names it means to catch do answer yes, but so does any piece of one,
// such as "umb" or "25|CONS", and so does an empty TERM, because an empty
// string is part of every string. The port keeps this, because changing it
// would change which terminals the editor refuses to run on.
func isInteractive() bool {
	eterm, ok := os.LookupEnv("TERM")
	if !ok {
		return true
	}
	//nolint:gocritic // the reversed order is the fault being reproduced
	return !strings.Contains("dumb|DUMB|cons25|CONS25|emacs|EMACS", eterm)
}

// updateDim works out the size of the terminal and reports whether it changed.
func (t *term) updateDim() bool {
	var cols, rows int
	if t.sz != nil {
		if c, r, ok := t.sz.size(); ok {
			cols, rows = c, r
		}
	}
	changed := t.width != cols || t.height != rows
	// A width of zero means the answer was no use, which is what a debugger
	// reports, so the old size stands.
	if cols > 0 {
		t.width, t.height = cols, rows
	}
	return changed
}

// colorBits returns how many bits of color the terminal takes.
func (t *term) colorBits() int { return t.palette.Bits() }

// startRaw notes that raw mode is wanted. On a Unix system the tty does the
// work, so this only counts.
func (t *term) startRaw() { t.rawEnabled++ }

// endRaw gives up one claim on raw mode, or all of them when force is set.
func (t *term) endRaw(force bool) {
	if t.rawEnabled <= 0 {
		return
	}
	if force {
		t.rawEnabled = 0
		return
	}
	t.rawEnabled--
}

//-------------------------------------------------------------
// Writing
//-------------------------------------------------------------

// write adds s to the terminal.
func (t *term) write(s string) {
	if s == "" {
		return
	}
	t.writeBytes([]byte(s))
}

// writeBytes adds b to the terminal. Every write ends up here, except writef,
// which goes straight to the buffer.
func (t *term) writeBytes(b []byte) {
	if len(b) == 0 {
		return
	}
	t.appendBuf(b)
}

// writeln adds s and then a line break.
func (t *term) writeln(s string) {
	t.write(s)
	t.write("\n")
}

// writeChar adds one byte.
func (t *term) writeChar(c byte) {
	t.writeBytes([]byte{c})
}

// writeRepeat adds s count times.
func (t *term) writeRepeat(s string, count int) {
	for ; count > 0; count-- {
		t.write(s)
	}
}

// writef adds formatted text.
//
// This goes straight into the buffer, so unlike write it does not look at
// escape sequences, does not drop control characters, and does not flush. The
// cursor movements below all use it, and they only ever produce escape
// sequences that carry no attributes.
func (t *term) writef(format string, args ...any) {
	t.buf.Appendf(format, args...)
}

// beep sounds the terminal bell, unless the terminal is silent.
//
// The C writes to standard error rather than to the terminal, so the bell
// still sounds while output is being collected. The port does the same.
func (t *term) beep() {
	if t.silent {
		return
	}
	fmt.Fprint(os.Stderr, "\a")
}

// appendEsc adds a whole escape sequence.
//
// A sequence that sets attributes is read as it goes past, which is how the
// terminal keeps track of what it is showing. When color is off the sequence
// is dropped rather than written.
func (t *term) appendEsc(s []byte) {
	if len(s) >= 2 && s[1] == '[' && s[len(s)-1] == 'm' {
		if t.nocolor {
			return
		}
		t.attr = t.attr.Merge(ansi.ParseEscapeSGR(string(s)))
	}
	t.buf.AppendString(string(s))
}

// appendUTF8 adds one character.
//
// A byte that is not part of valid UTF-8 was decoded into the raw plane, and
// goes out as the single byte it came from. That is what carries text in some
// other encoding through unchanged.
func (t *term) appendUTF8(s []byte) {
	r, _ := text.DecodeRune(s)
	if c, ok := text.RawByte(r); ok {
		t.buf.AppendByte(c)
		return
	}
	// The C has two branches here, one for a terminal that reads UTF-8 and one
	// for a terminal that does not, and they do the same thing. It sends UTF-8
	// either way and hopes.
	t.buf.AppendString(string(s))
}

// appendBuf adds text to the buffer, reading escape sequences as they go past
// and dropping the control characters a terminal cannot use.
func (t *term) appendBuf(s []byte) {
	pos := 0
	newline := false
	for pos < len(s) {
		// Plain ASCII goes in as one lump. The escape byte ends the run,
		// because an escape sequence has to be looked at.
		ascii, next := 0, 0
		for {
			n, _ := text.NextOfs(s, pos+ascii)
			next = n
			if next <= 0 {
				break
			}
			if c := s[pos+ascii]; c <= 0x1B || c > 0x7F {
				break
			}
			ascii += next
		}
		if ascii > 0 {
			t.buf.AppendString(string(s[pos : pos+ascii]))
			pos += ascii
		}
		if next <= 0 {
			break
		}
		c := s[pos]
		switch {
		case c >= 0x80:
			t.appendUTF8(s[pos : pos+next])
		case next > 1 && c == 0x1B:
			t.appendEsc(s[pos : pos+next])
		case c < ' ' && c != 0 && (c < 0x07 || c > 0x0D):
			// Dropped. A terminal has no use for these, and the ones that are
			// kept are the bell, backspace, tab, the line breaks, the form
			// feed and the vertical tab.
		default:
			if c == '\n' {
				newline = true
			}
			t.buf.AppendString(string(s[pos : pos+next]))
		}
		pos += next
	}
	t.checkFlush(newline)
}

//-------------------------------------------------------------
// Buffering
//-------------------------------------------------------------

// flush writes out everything that has collected.
func (t *term) flush() {
	if t.buf.Length() == 0 {
		return
	}
	_, _ = t.out.Write(t.buf.Bytes())
	t.buf.Clear()
}

// setBufferMode changes when output is written out, and returns the mode it
// replaced.
func (t *term) setBufferMode(mode bufferMode) bufferMode {
	was := t.mode
	if was == mode {
		return was
	}
	if mode == unbuffered {
		t.flush()
	}
	t.mode = mode
	return was
}

// checkFlush writes out the buffer when the mode calls for it, or when enough
// has collected that waiting is no longer worth it.
func (t *term) checkFlush(containsNewline bool) {
	if t.mode == unbuffered ||
		t.buf.Length() > flushAt ||
		(t.mode == lineBuffered && containsNewline) {
		t.flush()
	}
}

//-------------------------------------------------------------
// Moving the cursor
//-------------------------------------------------------------

// left moves the cursor n columns to the left.
func (t *term) left(n int) { t.move(n, 'D') }

// right moves the cursor n columns to the right.
func (t *term) right(n int) { t.move(n, 'C') }

// up moves the cursor n rows up.
func (t *term) up(n int) { t.move(n, 'A') }

// down moves the cursor n rows down.
func (t *term) down(n int) { t.move(n, 'B') }

// move moves the cursor n steps in the direction that cmd names. Moving
// nowhere writes nothing.
func (t *term) move(n int, cmd byte) {
	if n <= 0 {
		return
	}
	t.writef("%s%d%c", ansi.CSI, n, cmd)
}

// clearLine goes to the start of the line and clears it.
func (t *term) clearLine() { t.write("\r" + ansi.CSI + "K") }

// clearToEndOfLine clears from the cursor to the end of the line.
func (t *term) clearToEndOfLine() { t.write(ansi.CSI + "K") }

// startOfLine puts the cursor at the start of the line.
func (t *term) startOfLine() { t.write("\r") }

//-------------------------------------------------------------
// Attributes
//-------------------------------------------------------------

// attrReset puts every attribute back to the default of the terminal.
func (t *term) attrReset() { t.write(ansi.CSI + "m") }

// underline turns underlining on or off.
func (t *term) underline(on bool) { t.write(escapeFor(on, "4m", "24m")) }

// reverse swaps the text and background colors, or stops doing so.
func (t *term) reverse(on bool) { t.write(escapeFor(on, "7m", "27m")) }

// bold turns bold on or off.
func (t *term) bold(on bool) { t.write(escapeFor(on, "1m", "22m")) }

// italic turns italics on or off.
func (t *term) italic(on bool) { t.write(escapeFor(on, "3m", "23m")) }

// escapeFor returns the escape sequence for whichever of the two applies.
func escapeFor(on bool, yes, no string) string {
	if on {
		return ansi.CSI + yes
	}
	return ansi.CSI + no
}

// setColor sets the color of the text.
func (t *term) setColor(c ansi.Code) { t.write(ansi.Format(t.palette, c, false)) }

// setBgColor sets the color behind the text.
func (t *term) setBgColor(c ansi.Code) { t.write(ansi.Format(t.palette, c, true)) }

// setAttr makes the terminal show a, writing only what has to change.
//
// Nothing here assigns to t.attr. Each piece is set by writing an escape
// sequence, and appendEsc reads every sequence that goes past and updates
// t.attr from it. The two places below that do assign are covering for that:
// when the terminal cannot show the exact color, the sequence that goes out
// names the nearest color it can show, so reading it back would record the
// approximation. Storing the color that was asked for instead stops the next
// call sending the same sequence again.
func (t *term) setAttr(a ansi.Attr) {
	if t.nocolor {
		return
	}
	if a.Fg != t.attr.Fg && a.Fg != ansi.None {
		t.setColor(a.Fg)
		if t.palette < ansi.PaletteTrueColor && a.Fg.IsRGB() {
			t.attr.Fg = a.Fg
		}
	}
	if a.Bg != t.attr.Bg && a.Bg != ansi.None {
		t.setBgColor(a.Bg)
		if t.palette < ansi.PaletteTrueColor && a.Bg.IsRGB() {
			t.attr.Bg = a.Bg
		}
	}
	if a.Bold != t.attr.Bold && a.Bold != ansi.FlagUnset {
		t.bold(a.Bold == ansi.FlagOn)
	}
	if a.Underline != t.attr.Underline && a.Underline != ansi.FlagUnset {
		t.underline(a.Underline == ansi.FlagOn)
	}
	if a.Reverse != t.attr.Reverse && a.Reverse != ansi.FlagUnset {
		t.reverse(a.Reverse == ansi.FlagOn)
	}
	if a.Italic != t.attr.Italic && a.Italic != ansi.FlagUnset {
		t.italic(a.Italic == ansi.FlagOn)
	}
}

// writeFormatted adds s, giving each byte the attribute beside it in attrs.
//
// A nil attrs writes the text plain. Otherwise the text is written in runs,
// one per stretch that shares an attribute, and the attributes are put back
// to what they were at the end.
func (t *term) writeFormatted(s string, attrs []ansi.Attr) {
	if attrs == nil {
		t.write(s)
		return
	}
	// From here on the terminal has to be in raw mode, or the runs will not
	// land where they are meant to.
	if t.rawEnabled <= 0 {
		t.startRaw()
	}
	base := t.attr
	var current ansi.Attr
	i, n := 0, 0
	for i+n < len(s) && s[i+n] != 0 {
		if current != attrs[i+n] {
			if n > 0 {
				t.write(s[i : i+n])
				i += n
				n = 0
			}
			current = attrs[i]
			t.setAttr(base.Merge(current))
		}
		n++
	}
	if n > 0 {
		t.write(s[i : i+n])
	}
	t.setAttr(base)
}

// free writes out anything still waiting and gives up raw mode. The C frees
// the buffer here too, which Go does not need.
func (t *term) restore() {
	t.flush()
	t.endRaw(true)
}
