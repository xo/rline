package rline

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/rline/internal/editor"
	"github.com/xo/rline/internal/text"
	"github.com/xo/rline/key"
)

// --------------------------------------------------------------------------
// feed_test.go

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
	kPageDown  = "\x1b[6~"
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

// feedOpts are the settings a test changes from the plain ones.
type feedOpts struct {
	// NotUTF8 says the terminal sends something other than UTF-8, so a byte
	// above 0x7f is kept as the byte it was rather than read as a character.
	NotUTF8 bool

	// Completer offers completions, which the menu and the hints need.
	Completer Completer

	// Hints turns the inline suggestion back on.
	Hints bool

	// Highlighter marks up the line as it is typed. Supplying one is what
	// turns highlighting on; there is no separate switch.
	Highlighter Highlighter

	// Continue decides whether Enter finishes the line or starts another row
	// inside it, which is how a statement spans rows.
	Continue func(string) bool
}

// feed runs the keys through a fresh editor and returns the line and where the
// cursor ended up.
//
// Highlighting, brace matching and hints are off, so that what comes back is
// the text and nothing about how it was drawn.
func feed(t *testing.T, keys string) (string, int) {
	t.Helper()
	text, cursor, _ := feedWith(t, keys, feedOpts{})
	return text, cursor
}

// feedWith runs the keys through an editor set up as the options say, and also
// returns the key that finished the line, which is what tells an ordinary
// finish from an interrupt or the end of the input.
func feedWith(t *testing.T, keys string, opt feedOpts) (string, int, key.Code) {
	t.Helper()
	text, cursor, ended, _ := feedSession(t, keys, opt)
	return text, cursor, ended
}

// feedSession is feedWith with what was written to the terminal as well,
// which is the only way to see something that is drawn but never reaches the
// line, such as the list of every completion.
func feedSession(t *testing.T, keys string, opt feedOpts) (string, int, key.Code, string) {
	t.Helper()
	ev, sink := feedEnv(t, keys, opt)
	e := &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
	c := ev.runEditLoop(e)
	ev.term.flush()
	return e.Input.String(), e.Pos, c, sink.String()
}

// feedEnv builds an editor environment that reads the keys and writes to a
// buffer, which is what every test here drives.
func feedEnv(t *testing.T, keys string, opt feedOpts) (*env, *bytes.Buffer) {
	t.Helper()
	sink := &bytes.Buffer{}
	tm := newTerm(sink, termOptions{Sizer: fixedSize{cols: 80, rows: 24}})
	h := &history{}
	_ = h.loadFrom("", DefaultHistoryEntries)
	ev := &env{
		term:          tm,
		tty:           newTTY(&feedKeys{keys: keys}),
		bb:            newBBCode(tm),
		history:       h,
		completions:   &completions{},
		promptMarker:  "> ",
		cpromptMarker: "> ",
		opts: editor.EditOptions{
			MatchPairs:   DefaultMatchPairs,
			AutoPairs:    DefaultAutoPairs,
			MultilineEOL: DefaultMultilineEOL,
			AutoPair:     true,
		},
		braceMatching:   false,
		hints:           false,
		inlineHelp:      true,
		multilineIndent: true,
		multiline:       true,
		completePreview: true,
	}
	if opt.NotUTF8 {
		ev.tty.isUTF8 = false
	}
	if opt.Completer != nil {
		ev.completions.setCompleter(opt.Completer, nil)
	}
	if opt.Hints {
		ev.hints = true
		ev.hintDelay = 0
	}
	if opt.Highlighter != nil {
		ev.highlighter = opt.Highlighter
	}
	if opt.Continue != nil {
		ev.isIncomplete = opt.Continue
	}
	return ev, sink
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

// --------------------------------------------------------------------------
// behaviour_test.go

// The behaviours jline3 tests that nothing here covered: how a line ends, what
// happens to input that is not UTF-8, and a completion list too long to show.
//
// Named after LineReaderInterruptTest, LineReaderEofTest, MultiByteCharTest,
// EncodingTest and TooManyCandidatesTest in
// reader/src/test/java/org/jline/reader/impl.

// TestHowALineEnds checks which key finishes a line and what is left in it.
//
// The key that finished it is what tells the caller apart: Enter hands the
// line back, Ctrl-D on an empty line says the input has ended, and Ctrl-C says
// the line was abandoned.
func TestHowALineEnds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		keys    string
		want    string
		endedBy key.Code
	}{
		{"enter hands back what was typed", "abc" + kEnter, "abc", key.Enter},
		{"enter on an empty line hands back nothing", kEnter, "", key.Enter},

		// Ctrl-D ends the input, but only with nothing typed. Anywhere else it
		// is a delete, which is what every shell does.
		{"ctrl-d on an empty line ends the input", kCtrlD, "", key.CtrlD},
		{"ctrl-d with text deletes instead", "abc" + kCtrlA + kCtrlD + kEnter, "bc", key.Enter},
		{"ctrl-d at the end of text does nothing", "abc" + kCtrlD + kEnter, "abc", key.Enter},

		// Ctrl-C throws the line away and stops, whatever is in it.
		{"ctrl-c abandons an empty line", "\x03", "", key.CtrlC},
		{"ctrl-c abandons a line with text", "abc\x03", "", key.CtrlC},
		{"ctrl-g abandons a line as well", "abc\x07", "", key.Bell},

		// Escape clears the line, and ends it only when there is nothing left.
		{"escape on an empty line ends it", kEsc + kPause, "", key.Esc},
		{"escape with text clears and carries on", "abc" + kEsc + kPause + "z" + kEnter, "z", key.Enter},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, _, endedBy := feedWith(t, test.keys, feedOpts{})
			if got != test.want {
				t.Errorf("the line is %q, want %q", got, test.want)
			}
			if endedBy != test.endedBy {
				t.Errorf("the line was ended by %#x, want %#x", endedBy, test.endedBy)
			}
		})
	}
}

// TestInputThatIsNotUTF8 checks that a byte the terminal sends which is not
// UTF-8 survives being edited and comes back as the byte it was.
//
// isocline reads such a byte as a code point in a private plane, and its
// encoder turns that code point back into the one byte, so the line holds the
// byte itself. Until now nothing exercised that through the editor, only the
// codec on its own.
//
// The line holds the same bytes whether or not the terminal was said to send
// UTF-8, because both paths reach the same encoder. What the setting decides
// is how the bytes arrive: as one code point each, or as characters.
func TestInputThatIsNotUTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		keys   string
		want   string
		cursor int
	}{
		// Latin-1 for "é" and "ü", neither of which is UTF-8.
		{"one byte above ASCII", "\xe9" + kEnter, "\xe9", 1},
		{"several", "\xe9\xfc" + kEnter, "\xe9\xfc", 2},
		{"mixed with ASCII", "a\xe9b" + kEnter, "a\xe9b", 3},
		{"backspace takes off the whole byte", "a\xe9" + kBackspace + kEnter, "a", 1},
		{"the cursor steps over it", "a\xe9b" + kLeft + kLeft + "X" + kEnter, "aX\xe9b", 2},
		{"a word of them", "\xe9\xfc x" + kAltB + kEnter, "\xe9\xfc x", 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, cursor, _ := feedWith(t, test.keys, feedOpts{NotUTF8: true})
			if got != test.want {
				t.Errorf("the line holds %q, want %q", got, test.want)
			}
			if cursor != test.cursor {
				t.Errorf("the cursor is at %d, want %d", cursor, test.cursor)
			}
			// Handing the line to a terminal that does not read UTF-8 gives
			// back the bytes it sent.
			var b text.Buffer
			b.Replace(got)
			if decoded := string(b.DecodeFromLocale()); decoded != test.want {
				t.Errorf("the line came back as %q, want %q", decoded, test.want)
			}
		})
	}
}

// TestBytesThatLookLikeUTF8AreMergedIntoOne records a limitation of the
// format rather than a rule to keep.
//
// The line holds raw bytes as themselves, so two bytes from a terminal that
// was not sending UTF-8 which happen to form a valid UTF-8 sequence become
// one character. Nothing afterwards can tell them apart from a character that
// was typed, and handing the line back to that terminal drops it. The C code
// has the same hole, and the port keeps it.
func TestBytesThatLookLikeUTF8AreMergedIntoOne(t *testing.T) {
	t.Parallel()
	// The bytes are Latin-1 "Ã©", and they are also UTF-8 for "é".
	got, cursor, _ := feedWith(t, "\xc3\xa9"+kEnter, feedOpts{NotUTF8: true})
	if got != "\xc3\xa9" {
		t.Fatalf("the line holds %q, want %q", got, "\xc3\xa9")
	}
	// Two bytes went in and the cursor is past one character, not two.
	if cursor != 2 {
		t.Errorf("the cursor is at %d, want 2", cursor)
	}
	var b text.Buffer
	b.Replace(got)
	if decoded := string(b.DecodeFromLocale()); decoded != "" {
		t.Errorf("the line came back as %q; the merge is supposed to lose it, "+
			"so if this now round trips the format has been fixed and this test "+
			"should say so", decoded)
	}
	// One backspace takes off both bytes, because they are one character now.
	got, _, _ = feedWith(t, "\xc3\xa9"+kBackspace+kEnter, feedOpts{NotUTF8: true})
	if got != "" {
		t.Errorf("after backspace the line holds %q, want it empty", got)
	}
}

// TestUTF8Editing checks editing over characters that take more than one byte
// and more than one column.
func TestUTF8Editing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		keys   string
		want   string
		cursor int
	}{
		{"two byte characters", "éà" + kEnter, "éà", 4},
		{"three byte characters", "日本" + kEnter, "日本", 6},
		{"four byte characters", "😀🎉" + kEnter, "😀🎉", 8},
		{"delete a four byte character", "😀🎉" + kBackspace + kEnter, "😀", 4},
		{"move over a four byte character", "😀x" + kLeft + kLeft + "Y" + kEnter, "Y😀x", 1},
		{"a combining mark stays with its letter", "é" + kBackspace + kEnter, "e", 1},
		{"mixed widths", "a日b😀" + kEnter, "a日b😀", 9},
		{"home and end over wide characters", "日本語" + kHome + kEnd + kEnter, "日本語", 9},
		// A word motion takes the space beside the word with it, which
		// the recorded corpus for editline settles.
		{"word motion over wide characters", "日本 語" + kAltB + kEnter, "日本 語", 6},
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

// manyWords returns a completer offering the words that carry on from what
// has been typed.
//
// A completer is handed the line up to the cursor, and says with deleteBefore
// how much of it its answer replaces. Passing zero would leave the typed word
// in place and put the whole answer after it.
func manyWords(words ...string) Completer {
	words = slices.Sorted(slices.Values(words))
	return CompleterFunc(func(c *Completion, prefix string) {
		for _, w := range words {
			if strings.HasPrefix(w, prefix) && w != prefix {
				if !c.AddCandidate(Candidate{Replacement: w, DeleteBefore: len(prefix)}) {
					return
				}
			}
		}
	})
}

// wordsStartingWith returns n words that all start with start and agree on
// nothing after it, so that completing start cannot narrow them down and
// every one of them reaches the menu.
//
// The letter after start cycles through an alphabet in the order bytes sort
// in, which is what keeps the words from sharing anything: had they all begun
// "x0", Tab would have filled that in before drawing the menu.
func wordsStartingWith(start string, n int) []string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	words := make([]string, n)
	for i := range words {
		words[i] = fmt.Sprintf("%s%c-%03d", start, alphabet[i%len(alphabet)], i)
	}
	return words
}

// TestCompletionMenu checks the menu that opens when several answers fit.
//
// With the preview on, which is how isocline starts, nothing is picked until
// the user moves: the menu shows the answers and the line is still theirs.
func TestCompletionMenu(t *testing.T) {
	t.Parallel()
	words := wordsStartingWith("x", 30)
	tests := []struct {
		name   string
		keys   string
		want   string
		cursor int
	}{
		{
			// The shared start is filled in first, and here every answer
			// shares only the "x" that was typed, so the line does not move.
			name: "escape leaves the menu and the line alone",
			keys: "x" + kTab + kEsc + kPause + kEnter,
			want: "x", cursor: 1,
		},
		{
			// Nothing is picked yet, so Enter ends the line as it stands.
			name: "enter with nothing picked ends the line",
			keys: "x" + kTab + kEnter,
			want: "x", cursor: 1,
		},
		{
			// Down picks the first entry, and Enter takes it. The entries are
			// sorted, so the first is the lowest numbered.
			name: "down picks the first and enter takes it",
			keys: "x" + kTab + kDown + kEnter + kEnter,
			want: "x0-000", cursor: 6,
		},
		{
			name: "up picks the last one shown",
			keys: "x" + kTab + kUp + kEnter + kEnter,
			want: "x8-008", cursor: 6,
		},
		{
			// A digit picks that entry outright, counting from one.
			name: "a digit picks that entry",
			keys: "x" + kTab + "3" + kEnter,
			want: "x2-002", cursor: 6,
		},
		{
			// Tab moves on to the next entry and wraps at the end of what is
			// shown, which is nine of the thirty.
			name: "tab walks the entries and wraps",
			keys: "x" + kTab + strings.Repeat(kTab, 10) + kEnter + kEnter,
			want: "x0-000", cursor: 6,
		},
		{
			// Typing anything else takes what was being previewed. Nothing
			// was, so the letter is simply typed.
			name: "typing carries on with nothing picked",
			keys: "x" + kTab + "y" + kEnter,
			want: "xy", cursor: 2,
		},
		{
			name: "typing after picking takes the entry as well",
			keys: "x" + kTab + kDown + "!" + kEnter,
			want: "x0-000!", cursor: 7,
		},
		{
			// One answer needs no menu: it goes straight in.
			name: "a single answer is filled in",
			keys: "x1-" + kTab + kEnter,
			want: "x1-001", cursor: 6,
		},
		{
			name: "no answer leaves the line as it was",
			keys: "zz" + kTab + kEnter,
			want: "zz", cursor: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, cursor, _ := feedWith(t, test.keys, feedOpts{Completer: manyWords(words...)})
			if got != test.want {
				t.Errorf("the line is %q, want %q", got, test.want)
			}
			if cursor != test.cursor {
				t.Errorf("the cursor is at %d, want %d", cursor, test.cursor)
			}
		})
	}
}

// TestCompletionMenuShowsThereAreMore checks the menu says how many answers it
// is not showing, and that page-down shows all of them.
func TestCompletionMenuShowsThereAreMore(t *testing.T) {
	t.Parallel()
	words := wordsStartingWith("x", 30)
	_, _, _, out := feedSession(t, "x"+kTab+kEnter, feedOpts{Completer: manyWords(words...)})
	drawn := stripEscapes(out)
	if want := "to see all 30 completions"; !strings.Contains(drawn, want) {
		t.Errorf("the menu does not say %q\ndrawn: %s", want, drawn)
	}
	// Nine of the thirty are shown, so the tenth is not there.
	if strings.Contains(drawn, "x9-009") {
		t.Errorf("the menu shows a tenth entry\ndrawn: %s", drawn)
	}
}

// TestCompletionWithTooManyAnswers checks a completer with more answers than
// the editor will ask for.
//
// It stops the completer at 250 and says so, rather than saying how many
// there are, because it does not know. Page-down asks for the rest and lists
// them all.
func TestCompletionWithTooManyAnswers(t *testing.T) {
	t.Parallel()
	words := wordsStartingWith("x", 300)
	completer := manyWords(words...)

	_, _, _, out := feedSession(t, "x"+kTab+kEnter, feedOpts{Completer: completer})
	drawn := stripEscapes(out)
	if want := "to see all further completions"; !strings.Contains(drawn, want) {
		t.Errorf("the menu does not say %q\ndrawn: %s", want, drawn)
	}
	if strings.Contains(drawn, "possible completions") {
		t.Error("the menu says how many there are, which it cannot know")
	}

	// Page-down asks the completer for the rest and lists every one.
	line, cursor, _, out := feedSession(t, "x"+kTab+kPageDown+kEnter, feedOpts{Completer: completer})
	drawn = stripEscapes(out)
	if want := "(300 possible completions)"; !strings.Contains(drawn, want) {
		t.Errorf("the list does not say %q\ndrawn: %s", want, drawn)
	}
	for _, w := range []string{words[0], words[len(words)-1]} {
		if !strings.Contains(drawn, w) {
			t.Errorf("the list leaves out %q", w)
		}
	}
	// Listing them all takes nothing, so the line is as it was typed.
	if line != "x" || cursor != 1 {
		t.Errorf("the line is %q at %d, want %q at 1", line, cursor, "x")
	}
}

// TestHintIsOfferedAndTaken checks the inline suggestion end to end: it is
// offered when one answer fits, and the right arrow takes it.
func TestHintIsOfferedAndTaken(t *testing.T) {
	t.Parallel()
	completer := manyWords("select", "settle")
	tests := []struct {
		name string
		keys string
		want string
	}{
		{"the right arrow takes the suggestion", "sel" + kRight + kEnter, "select"},
		{"the end key takes it as well", "sel" + kEnd + kEnter, "select"},
		{"nothing is taken when two answers fit", "se" + kRight + kEnter, "se"},
		{"typing on carries past it", "selec" + "t" + kEnter, "select"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, _, _ := feedWith(t, test.keys, feedOpts{
				Completer: completer,
				Hints:     true,
			})
			if got != test.want {
				t.Errorf("the line is %q, want %q", got, test.want)
			}
		})
	}
}

// TestHintIsShown checks the suggestion is drawn after the cursor.
func TestHintIsShown(t *testing.T) {
	t.Parallel()
	_, _, _, out := feedSession(t, "sel"+kEnter, feedOpts{
		Completer: manyWords("select", "settle"),
		Hints:     true,
	})
	if drawn := stripEscapes(out); !strings.Contains(drawn, "select") {
		t.Errorf("the rest of the word was never drawn\ndrawn: %s", drawn)
	}
}

// TestEditLineSaysWhenTheInputEnded checks the answer the caller gets, rather
// than the key the loop finished on.
//
// A line that ends is handed back with true. Ctrl-D on an empty line and a
// stop event are the only two that say the input is over, which is what
// ReadLine turns into io.EOF. Ctrl-C is not one of them: the C cancels the
// line and hands back an empty one, so a caller cannot tell it from Enter on
// an empty line. The port keeps that.
func TestEditLineSaysWhenTheInputEnded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		keys  string
		want  string
		ended bool
	}{
		{"a line that was typed", "abc" + kEnter, "abc", false},
		{"an empty line", kEnter, "", false},
		{"ctrl-d on an empty line", kCtrlD, "", true},
		{"ctrl-d with something typed", "abc" + kCtrlD + kEnter, "abc", false},
		{"ctrl-c", "abc\x03", "", false},
		{"escape on an empty line", kEsc + kPause, "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ev, _ := feedEnv(t, test.keys, feedOpts{})
			line, ok, _ := ev.editLine("")
			if line != test.want {
				t.Errorf("the line is %q, want %q", line, test.want)
			}
			if ok == test.ended {
				t.Errorf("the input ended: %v, want %v", !ok, test.ended)
			}
		})
	}
}

// TestALineThatEndedIsRemembered checks what reaches the history, which is
// the other thing that happens when a line ends.
func TestALineThatEndedIsRemembered(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keys string
		want []string
	}{
		{"a line is kept", "select" + kEnter, []string{"select"}},
		// Neither an empty line nor a single character is worth keeping.
		{"an empty line is not", kEnter, nil},
		{"one character is not", "a" + kEnter, nil},
		{"an abandoned line is not", "select\x03", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ev, _ := feedEnv(t, test.keys, feedOpts{})
			_, _, _ = ev.editLine("")
			// The newest entry is at zero.
			var got []string
			for i := 0; ; i++ {
				entry, ok := ev.history.get(i)
				if !ok {
					break
				}
				got = append(got, entry)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("the history holds %q, want %q", got, test.want)
			}
		})
	}
}

// TestTabFillsInWhatEveryAnswerShares checks the step Tab takes before it
// opens the menu.
//
// The menu recordings drive the menu alone, so this is the only check on the
// Tab entry point: that nothing beeps into the line, that one answer goes
// straight in, and that several answers fill in as much of themselves as they
// agree on.
func TestTabFillsInWhatEveryAnswerShares(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		words  []string
		keys   string
		want   string
		cursor int
	}{
		{
			// Every answer carries on "sel", so that much goes in and the
			// menu opens on what is left.
			name:  "as much as they share",
			words: []string{"select", "selected", "selecting"},
			keys:  "se" + kTab + kEsc + kPause + kEnter,
			want:  "select", cursor: 6,
		},
		{
			name:  "nothing when they share nothing",
			words: []string{"select", "settle"},
			keys:  "se" + kTab + kEsc + kPause + kEnter,
			want:  "se", cursor: 2,
		},
		{
			name:  "the whole answer when there is one",
			words: []string{"select", "settle"},
			keys:  "sel" + kTab + kEnter,
			want:  "select", cursor: 6,
		},
		{
			name:  "nothing at all when none fits",
			words: []string{"select"},
			keys:  "zz" + kTab + kEnter,
			want:  "zz", cursor: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, cursor, _ := feedWith(t, test.keys, feedOpts{Completer: manyWords(test.words...)})
			if got != test.want {
				t.Errorf("the line is %q, want %q", got, test.want)
			}
			if cursor != test.cursor {
				t.Errorf("the cursor is at %d, want %d", cursor, test.cursor)
			}
		})
	}
}

// TestReadLineTellsAnInterruptFromAnEmptyLine checks the one behaviour the
// port does not take from the C.
//
// The C clears the line on Ctrl-C and hands back an empty string, so a caller
// cannot tell an abandoned line from Enter on an empty one. usql has to: it
// resets its statement buffer on an interrupt and carries on, and would
// otherwise run whatever Ctrl-C left behind. So ReadLine answers
// ErrInterrupted instead.
func TestReadLineTellsAnInterruptFromAnEmptyLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		keys string
		want string
		err  error
	}{
		{"a line that was typed", "abc" + kEnter, "abc", nil},
		{"enter on an empty line", kEnter, "", nil},
		// The two keys the C treats alike, which both mean the line was
		// given up rather than finished.
		{"ctrl-c with something typed", "abc\x03", "", ErrInterrupted},
		{"ctrl-c on an empty line", "\x03", "", ErrInterrupted},
		{"ctrl-g", "abc\x07", "", ErrInterrupted},
		// Ctrl-D on an empty line is the input ending, which is a different
		// answer and must not be folded into the one above.
		{"ctrl-d on an empty line", kCtrlD, "", io.EOF},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ev, _ := feedEnv(t, test.keys, feedOpts{})
			r := &Session{env: ev, canEdit: true}
			line, err := r.ReadLine("")
			if !errors.Is(err, test.err) {
				t.Errorf("ReadLine gave %v, want %v", err, test.err)
			}
			if line != test.want {
				t.Errorf("ReadLine gave the line %q, want %q", line, test.want)
			}
		})
	}
}

// TestSetCompleterReplacesTheCompleter checks that a completer can be changed
// after the reader exists, which is what a program whose completions depend on
// something that changes while it runs needs.
func TestSetCompleterReplacesTheCompleter(t *testing.T) {
	t.Parallel()
	ev, _ := feedEnv(t, "se"+kTab+kEsc+kPause+kEnter, feedOpts{})
	r := &Session{env: ev, canEdit: true}

	// Nothing is offered until a completer is set.
	if ev.completions.completer != nil {
		t.Fatal("a reader with no completer has one")
	}
	r.SetCompleter(manyWords("select", "selected"))
	line, err := r.ReadLine("")
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	// The shared start went in, so the completer that was set is the one that
	// ran.
	if line != "select" {
		t.Errorf("the line is %q, want %q", line, "select")
	}

	// And it can be replaced, including with nothing.
	r.SetCompleter(nil)
	if ev.completions.completer != nil {
		t.Error("setting a nil completer left the old one in place")
	}
}

// TestAKeyThatChangesNothingDrawsNothing checks the value every editing
// operation returns: whether it changed anything, which is the only thing
// deciding whether the line is drawn again.
//
// Nothing checked it before. The operations are recorded against the C, but
// the C has no such value — it returns early instead — so the corpus cannot
// carry it, and the tests here read the line that came back rather than what
// was drawn, and the line is right either way because the position still
// moves. Making cursorLeft always answer false, or always answer true, left
// the whole suite green.
//
// Both directions matter. Always false means a key does nothing on screen
// until something else forces a redraw, with the cursor left behind. Always
// true means drawing the line again for a key that did nothing, which is the
// flicker the value exists to avoid.
//
// It is checked by adding one key to a session and comparing what was drawn
// against the same session without it, because "changed nothing" is exactly
// the claim that the extra key put nothing on the terminal.
func TestAKeyThatChangesNothingDrawsNothing(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		typed string
		key   string
		draws bool
	}{
		// Nowhere to go, so nothing should be sent at all.
		{"left at the start", kCtrlA, kLeft, false},
		{"right at the end", "", kRight, false},
		{"backspace at the start", kCtrlA, kBackspace, false},
		{"delete at the end", "", kDel, false},
		// Somewhere to go, so the screen has to follow.
		{"left with room to move", "", kLeft, true},
		{"home from the end", "", kHome, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			const typed = "abc"
			_, _, _, without := feedSession(t, typed+test.typed+kEnter, feedOpts{})
			_, _, _, with := feedSession(t, typed+test.typed+test.key+kEnter, feedOpts{})
			switch {
			case test.draws && with == without:
				t.Errorf("the key drew nothing, so the screen never followed it.\nBoth drew:\n%q", without)
			case !test.draws && with != without:
				t.Errorf("the key drew something although it changed nothing.\nWithout it:\n%q\nWith it:\n%q",
					without, with)
			}
		})
	}
}

// TestHighlighterSeesTheWholeStatement checks the claim the plan rests usql's
// path on: that a statement spanning rows reaches the highlighter whole.
//
// usql accumulates a statement across reads and re-parses it every keystroke,
// because the reader it uses today hands it one line at a time. With
// WithContinue the rows are one buffer, so the highlighter is handed all of
// it and the accumulation has nothing left to do. That is the argument for
// usql dropping SetOutput rather than this package growing a string filter,
// so it is worth holding rather than asserting.
func TestHighlighterSeesTheWholeStatement(t *testing.T) {
	t.Parallel()

	// Every line the highlighter was handed, in order.
	var seen []string
	h := HighlighterFunc(func(l *LineStyle) {
		seen = append(seen, l.Text())
	})
	// Unfinished until a semicolon, which is what a SQL prompt does.
	unfinished := func(line string) bool {
		return !strings.HasSuffix(strings.TrimSpace(line), ";")
	}

	// Three rows: Enter carries on twice, and the semicolon ends it.
	line, _, _, _ := feedSession(t, "select a,"+kEnter+"b"+kEnter+"c from t;"+kEnter, feedOpts{
		Highlighter: h,
		Continue:    unfinished,
	})

	const want = "select a,\nb\nc from t;"
	if line != want {
		t.Fatalf("the line came back as %q, want %q", line, want)
	}
	if len(seen) == 0 {
		t.Fatal("the highlighter was never called")
	}
	// The last call is the one that matters: by then the whole statement is
	// in the buffer, and the highlighter is handed all of it rather than the
	// row being typed.
	last := seen[len(seen)-1]
	if last != want {
		t.Errorf("the highlighter was last handed %q, want the whole statement %q", last, want)
	}
	// And it saw the rows join rather than only ever one row.
	sawTwoRows := false
	for _, s := range seen {
		if strings.Count(s, "\n") >= 2 {
			sawTwoRows = true
			break
		}
	}
	if !sawTwoRows {
		t.Errorf("the highlighter never saw more than two rows at once, so it is being handed one row at a time: %q", seen)
	}
}

// TestHintsCanBeTurnedOff checks the switch in the direction a user would
// notice.
//
// Seven tests exercise hints being on and none exercised them being off, so
// the guard that honours the switch could be removed entirely and the whole
// suite stayed green. Tests that all point the same way look like coverage
// of a switch and are coverage of one of its positions.
func TestHintsCanBeTurnedOff(t *testing.T) {
	t.Parallel()
	const typed = "sel"
	words := manyWords("select", "settle")

	_, _, _, on := feedSession(t, typed+kEnter, feedOpts{Completer: words, Hints: true})
	if drawn := stripEscapes(on); !strings.Contains(drawn, "select") {
		t.Fatalf("with hints on the suggestion was never drawn, so this test "+
			"cannot tell the two apart\ndrawn: %s", drawn)
	}

	_, _, _, off := feedSession(t, typed+kEnter, feedOpts{Completer: words})
	if drawn := stripEscapes(off); strings.Contains(drawn, "select") {
		t.Errorf("with hints off the suggestion was drawn anyway\ndrawn: %s", drawn)
	}
}

// TestBraceMatchingDrawsTheMatch checks that a matching brace is drawn
// differently from the text around it.
//
// Nothing asserted this. The menu recording notices brace matching being
// wrongly on, which is the opposite direction and is why it felt covered:
// emptying the set of pairs, so that nothing ever matches, left the whole
// suite green.
//
// The assertion is that the drawing changes rather than that a particular
// style is used, because which style is bbcode's business and is recorded
// against the C elsewhere. What is being pinned here is that the switch
// reaches the drawing at all.
func TestBraceMatchingDrawsTheMatch(t *testing.T) {
	t.Parallel()

	// The driven harness builds a terminal with no colour, which is right
	// for reading the text it drew and no use for seeing an attribute. So
	// this builds its own with colour on.
	//
	// That harness asks for no colour by writing termOptions{Sizer: ...},
	// which mentions a sizer and nothing else. Since the switches went
	// positive, an unset Color means off, so the literal is asking for
	// something by saying nothing and the request has no line to point at.
	// This one says Color: true for that reason as much as for the colour.
	drawnWith := func(matching bool) string {
		var sink bytes.Buffer
		tm := newTerm(&sink, termOptions{Color: true, Sizer: fixedSize{cols: 80, rows: 24}})
		bb := newBBCode(tm)
		for _, s := range defaultStyles {
			bb.styleDef(s[0], s[1])
		}
		ev := &env{
			term:          tm,
			bb:            bb,
			promptMarker:  "> ",
			cpromptMarker: "> ",
			opts:          editor.EditOptions{MatchPairs: DefaultMatchPairs},
			braceMatching: matching,
		}
		e := &editor.Editor{TermW: 80, CurRows: 1}
		e.Input.Replace("(x)")
		// Just after the closing brace. A brace counts as under the cursor
		// when the cursor is the position after it, which is what
		// highlightMatchBraces means by i == cursorPos-1, so a cursor sitting
		// on the brace itself matches nothing. An earlier version of this
		// test used 2 and failed, and the case was wrong rather than the code.
		e.Pos = 3
		ev.refresh(e)
		tm.flush()
		return sink.String()
	}

	on, off := drawnWith(true), drawnWith(false)
	if on == off {
		t.Errorf("a matching brace was drawn the same as the text around it, so "+
			"brace matching reaches nothing\ndrawn: %q", on)
	}
}
