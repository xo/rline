package rline

import (
	"bytes"
	"testing"
	"time"
)

// Driving the whole editor over a fixed set of keystrokes.
//
// The recorded corpora check that each function answers correctly, and the
// recorded sessions check that a program works, but neither checks the key
// dispatch: which key reaches which operation, and what the line looks like
// afterwards. That is the layer this file covers.
//
// The shape is taken from python-prompt-toolkit, whose tests/test_cli.py feeds
// a string of keys to the editor and asserts on the text and the cursor that
// come out. It is the only comparable project with a test suite worth copying:
// GNU readline has example programs rather than tests.

// The keys these tests send. A terminal sends these bytes, and the decoder
// turns them back into the keys they name.
const (
	kEnter     = "\r"
	kLinefeed  = "\n" // ctrl+J, which starts another row
	kTab       = "\t"
	kEsc       = "\x1b"
	kBackspace = "\x7f"
	kDel       = "\x1b[3~"
	kLeft      = "\x1b[D"
	kRight     = "\x1b[C"
	kUp        = "\x1b[A"
	kDown      = "\x1b[B"
	kHome      = "\x1b[H"
	kEnd       = "\x1b[F"
	kCtrlA     = "\x01"
	kCtrlB     = "\x02"
	kCtrlD     = "\x04"
	kCtrlE     = "\x05"
	kCtrlF     = "\x06"
	kCtrlK     = "\x0b"
	kCtrlT     = "\x14"
	kCtrlU     = "\x15"
	kCtrlW     = "\x17"
	kCtrlY     = "\x19"
	kCtrlZ     = "\x1a"
	kAltB      = "\x1bb"
	kAltD      = "\x1bd"
	kAltF      = "\x1bf"
	kAltM      = "\x1bm"

	// kPause makes the reader answer nothing once, which is how a terminal
	// looks when the user stops typing. An escape needs it: the decoder waits
	// to see whether more follows, so Escape then "z" sent together is alt+z
	// rather than two keys.
	kPause = "\x00"
)

// feedKeys hands out a fixed set of keystrokes one byte at a time.
type feedKeys struct {
	keys  string
	pos   int
	spent bool
}

// readByte satisfies byteReader.
//
// When the keys run out it gives one Ctrl-C, which ends the line. Without that
// a sequence that never finishes the line would hang the whole suite rather
// than failing, and the empty answer it produces instead is easy to read.
func (f *feedKeys) readByte(_ time.Duration) (byte, bool) {
	if f.pos < len(f.keys) {
		c := f.keys[f.pos]
		f.pos++
		if c == 0 {
			// A pause. Nothing arrives, which is what the decoder waits for.
			return 0, false
		}
		return c, true
	}
	if !f.spent {
		f.spent = true
		return 0x03, true
	}
	return 0, false
}

// feed runs the keys through a fresh editor and returns the line and where the
// cursor ended up.
//
// Highlighting, brace matching and hints are off, so that what comes back is
// the text and nothing about how it was drawn.
func feed(t *testing.T, keys string) (string, int) {
	t.Helper()
	var sink bytes.Buffer
	tm := newTerm(&sink, termOptions{NoColor: true, Sizer: fixedSize{cols: 80, rows: 24}})
	h := &history{}
	h.loadFrom("", DefaultHistoryEntries)
	ev := &env{
		term:          tm,
		tty:           newTTY(&feedKeys{keys: keys}),
		bb:            newBBCode(tm),
		history:       h,
		completions:   &completions{},
		promptMarker:  "> ",
		cpromptMarker: "> ",
		opts: editOptions{
			MatchBraces:  DefaultMatchBraces,
			AutoBraces:   DefaultAutoBraces,
			MultilineEOL: DefaultMultilineEOL,
		},
		noHighlight:  true,
		noBraceMatch: true,
		noHint:       true,
	}
	e := &editor{opts: ev.opts, termW: 80, curRows: 1}
	ev.runEditLoop(e)
	return e.input.string(), e.pos
}

// TestFeedEditing drives the editor over a set of keystrokes and checks the
// line and the cursor that come out.
func TestFeedEditing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		keys   string
		want   string
		cursor int
	}{
		// Typing and taking back.
		{"plain text", "hello" + kEnter, "hello", 5},
		{"nothing typed", kEnter, "", 0},
		{"backspace takes off the last", "hello" + kBackspace + kEnter, "hell", 4},
		{"backspace at the start does nothing", kBackspace + "a" + kEnter, "a", 1},
		{"delete takes off the next", "hello" + kCtrlA + kDel + kEnter, "ello", 0},
		{"delete at the end does nothing", "hi" + kDel + kEnter, "hi", 2},

		// Moving about.
		{"left and right", "abc" + kLeft + kLeft + kRight + kEnter, "abc", 2},
		{"left at the start stops", "ab" + kCtrlA + kLeft + kEnter, "ab", 0},
		{"right at the end stops", "ab" + kRight + kEnter, "ab", 2},
		{"home and end", "abc" + kHome + kEnd + kEnter, "abc", 3},
		{"ctrl-a and ctrl-e", "abc" + kCtrlA + kCtrlE + kEnter, "abc", 3},
		{"ctrl-b and ctrl-f", "abc" + kCtrlB + kCtrlB + kCtrlF + kEnter, "abc", 2},
		{"typing in the middle", "abc" + kCtrlA + "X" + kEnter, "Xabc", 1},

		// Words. A word motion takes the space beside the word with it, which
		// is isocline's rule: from the end of "one two", going back lands at
		// 3 rather than at 4.
		{"back a word", "one two" + kAltB + kEnter, "one two", 3},
		{"back two words", "one two" + kAltB + kAltB + kEnter, "one two", 0},
		{"forward a word", "one two" + kCtrlA + kAltF + kEnter, "one two", 4},
		{"delete the word before", "one two" + kCtrlW + kEnter, "one ", 4},
		{"delete the word after", "one two" + kCtrlA + kAltD + kEnter, "two", 0},

		// Killing.
		{"kill to the end", "hello" + kCtrlA + kCtrlK + kEnter, "", 0},
		{"kill to the start", "hello" + kCtrlU + kEnter, "", 0},
		{"kill to the start from the middle", "hello" + kLeft + kLeft + kCtrlU + kEnter, "lo", 0},
		{"kill to the end from the middle", "hello" + kLeft + kLeft + kCtrlK + kEnter, "hel", 3},

		// Swapping. The cursor lands in front of the pair rather than after
		// it, which is what isocline does and not what readline does.
		{"swap two characters", "ab" + kLeft + kCtrlT + kEnter, "ba", 0},
		{"swap at the start does nothing", "ab" + kCtrlA + kCtrlT + kEnter, "ab", 0},

		// Undo and redo.
		{"undo the last typing", "abc" + kCtrlZ + kEnter, "ab", 2},
		{"undo twice", "abc" + kCtrlZ + kCtrlZ + kEnter, "a", 1},
		{"redo what was undone", "abc" + kCtrlZ + kCtrlY + kEnter, "abc", 3},

		// More than one row.
		{"ctrl-j starts another row", "a" + kLinefeed + "b" + kEnter, "a\nb", 3},
		{"up moves between rows", "a" + kLinefeed + "b" + kUp + "X" + kEnter, "aX\nb", 2},
		{"down moves back", "a" + kLinefeed + "b" + kUp + kDown + "X" + kEnter, "a\nbX", 4},
		{"home goes to the start of the row", "a" + kLinefeed + "bc" + kHome + "X" + kEnter, "a\nXbc", 3},

		// Escape clears the line, which is what isocline does.
		{"escape clears what was typed", "abc" + kEsc + kPause + "z" + kEnter, "z", 1},

		// Braces close themselves.
		{"an opening brace closes itself", "(" + kEnter, "()", 1},
		{"typing the closing brace steps over it", "()" + kEnter, "()", 2},
		{"a second opening brace closes itself too", "(" + kCtrlA + "(" + kEnter, "()()", 1},
		{"jump to the matching brace", "(ab)" + kRight + kAltM + kEnter, "(ab)", 1},

		// Text outside ASCII.
		{"accented letters", "éàü" + kEnter, "éàü", 6},
		{"backspace takes off a whole character", "éà" + kBackspace + kEnter, "é", 2},
		{"left moves over a whole character", "éà" + kLeft + "X" + kEnter, "éXà", 3},
		{"wide characters", "日本" + kEnter, "日本", 6},
		{"backspace over a wide character", "日本" + kBackspace + kEnter, "日", 3},

		// The line continuation character.
		{"a trailing backslash starts another row", `a\` + kEnter + "b" + kEnter, "a\nb", 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, cursor := feed(t, test.keys)
			if got != test.want {
				t.Errorf("the line is %q, want %q", got, test.want)
			}
			if cursor != test.cursor {
				t.Errorf("the cursor is at %d, want %d", cursor, test.cursor)
			}
		})
	}
}
