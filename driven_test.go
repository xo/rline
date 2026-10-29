package rline

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/inputrc"

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
	kLinefeed  = "\n" // ctrl+J, which readline treats as Enter
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
	kUndo      = "\x1f" // ctrl+_, which is undo
	kAltB      = "\x1bb"
	kAltD      = "\x1bd"
	kAltF      = "\x1bf"

	// kPause makes the reader answer nothing once, which is how a terminal
	// looks when the user stops typing.
	kPause = "\x00"
)

// feedKeys hands out a fixed set of keystrokes one byte at a time.
type feedKeys struct {
	keys  string
	pos   int
	spent bool

	// zeroIsKey sends a zero byte as the key ctrl+space, rather than as a
	// pause.
	zeroIsKey bool

	// pauses are the places in keys where the reader answers nothing once
	// before it goes on, as a pause in typing does.
	pauses map[int]bool
}

// readByte satisfies byteReader.
//
// When the keys run out it gives one Ctrl-C, which ends the line. Without that
// a sequence that never finishes the line would hang the whole suite rather
// than failing, and the empty answer it produces instead is easy to read.
func (f *feedKeys) readByte(_ time.Duration) (byte, bool) {
	if f.pauses[f.pos] {
		delete(f.pauses, f.pos)
		return 0, false
	}
	if f.pos < len(f.keys) {
		c := f.keys[f.pos]
		f.pos++
		if c == 0 && !f.zeroIsKey {
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

	// ZeroIsKey sends a zero byte in the keys as ctrl+space, which readline
	// calls \C-@, rather than as a pause.
	ZeroIsKey bool

	// Pauses are the places in the keys where typing pauses, which ends the
	// wait of the decoder for the rest of an escape sequence.
	Pauses map[int]bool

	// CompletionMenu binds Tab to isocline's completion menu (D51), and
	// AutoTab completes again after the menu completes.
	CompletionMenu bool
	AutoTab        bool
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
		keys:          newDecoder(&feedKeys{keys: keys, zeroIsKey: opt.ZeroIsKey, pauses: opt.Pauses}),
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
		ev.keys.isUTF8 = false
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
	ev.completionMenu = opt.CompletionMenu
	ev.completeAutoTab = opt.AutoTab
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

		// Words, as readline moves over them: back to the start of a word,
		// and forward to its end.
		{"back a word", "one two" + kAltB + kEnter, "one two", 4},
		{"back two words", "one two" + kAltB + kAltB + kEnter, "one two", 0},
		{"forward a word", "one two" + kCtrlA + kAltF + kEnter, "one two", 3},
		{"delete the word before", "one two" + kCtrlW + kEnter, "one ", 4},
		{"delete the word after", "one two" + kCtrlA + kAltD + kEnter, " two", 0},

		// Esc is a prefix, as in readline: Esc and f is forward-word however
		// long the wait between them (D46).
		{"escape waits for the next key", "one two" + kCtrlA + kEsc + kPause + "f" + kEnter, "one two", 3},

		// Killing.
		{"kill to the end", "hello" + kCtrlA + kCtrlK + kEnter, "", 0},
		{"kill to the start", "hello" + kCtrlU + kEnter, "", 0},
		{"kill to the start from the middle", "hello" + kLeft + kLeft + kCtrlU + kEnter, "lo", 0},
		{"kill to the end from the middle", "hello" + kLeft + kLeft + kCtrlK + kEnter, "hel", 3},

		// Swapping. The cursor lands after the pair, as in readline.
		{"swap two characters", "ab" + kLeft + kCtrlT + kEnter, "ba", 2},
		{"swap at the start does nothing", "ab" + kCtrlA + kCtrlT + kEnter, "ab", 0},

		// Undo. readline takes back a run of typing as one change, and has
		// no redo.
		{"undo takes back the typing", "abc" + kUndo + kEnter, "", 0},
		{"undo takes back a kill", "abc" + kCtrlA + kCtrlK + kUndo + kEnter, "abc", 3},

		// Braces close themselves.
		{"an opening brace closes itself", "(" + kEnter, "()", 1},
		{"typing the closing brace steps over it", "()" + kEnter, "()", 2},
		{"a second opening brace closes itself too", "(" + kCtrlA + "(" + kEnter, "()()", 1},

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

// TestRowsOfAStatement drives a statement over more than one row. The rows
// come from WithContinue, which readline does not have (D45), and readline's
// commands treat the rows as one line (D46).
func TestRowsOfAStatement(t *testing.T) {
	t.Parallel()
	unfinished := func(s string) bool { return !strings.HasSuffix(s, ";") }
	for _, test := range []struct {
		name   string
		keys   string
		want   string
		cursor int
	}{
		{"enter carries on until the statement ends", "a" + kEnter + "b;" + kEnter, "a\nb;", 4},
		{"ctrl-j carries on as enter does", "a" + kLinefeed + "b;" + kLinefeed, "a\nb;", 4},
		{"home goes to the start of the statement", "a" + kEnter + "b;" + kHome + "X" + kEnter, "Xa\nb;", 1},
		{"backspace joins the rows", "a" + kEnter + kBackspace + "b;" + kEnter, "ab;", 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, cursor, _ := feedWith(t, test.keys, feedOpts{Continue: unfinished})
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

		// Ctrl-G is readline's abort. It rings the bell and leaves the line
		// as it was (D45).
		{"ctrl-g leaves the line", "abc\x07" + kEnter, "abc", key.Enter},

		// Ctrl-J is accept-line in readline, as Enter is.
		{"ctrl-j hands back the line", "abc" + kLinefeed, "abc", key.Enter},
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
		// readline's words are letters and digits, and a byte that is not
		// UTF-8 is neither, so the word before the cursor is the x.
		{"a word after them", "\xe9\xfc x" + kAltB + kEnter, "\xe9\xfc x", 3},
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
		// readline's backward-word goes to the start of the word.
		{"word motion over wide characters", "日本 語" + kAltB + kEnter, "日本 語", 7},
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

// TestCompletionList checks how readline shows completions (D45): Tab fills
// in what they share, a second Tab lists them below the line in columns, and
// the line is drawn again below the list.
func TestCompletionList(t *testing.T) {
	t.Parallel()
	words := wordsStartingWith("x", 30)
	complete := feedOpts{Completer: manyWords(words...)}
	for _, test := range []struct {
		name   string
		keys   string
		want   string
		listed bool
	}{
		{"one tab rings and lists nothing", "x" + kTab + kEnter, "x", false},
		{"a second tab lists them", "x" + kTab + kTab + kEnter, "x", true},
		{"esc ? lists them at once", "x" + kEsc + "?" + kPause + kEnter, "x", true},
		{"a key between the tabs lists nothing", "x" + kTab + kCtrlB + kCtrlF + kTab + kEnter, "x", false},
		{"a single answer goes in", "x1-" + kTab + kEnter, "x1-001", false},
		{"no answer leaves the line", "zz" + kTab + kTab + kEnter, "zz", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			line, _, _, out := feedSession(t, test.keys, complete)
			if line != test.want {
				t.Errorf("the line is %q, want %q", line, test.want)
			}
			drawn := stripEscapes(out)
			for _, w := range words {
				if strings.Contains(drawn, w) != test.listed && w != test.want {
					t.Fatalf("%q drawn is %v, want %v\ndrawn: %q", w, !test.listed, test.listed, drawn)
				}
			}
		})
	}

	// Thirty words of six columns fit nine to a row of 80, so the list is
	// four rows that run down and then across: x0-000 heads the first column
	// and x4-004 the second.
	_, _, _, out := feedSession(t, "x"+kTab+kTab+kEnter, complete)
	drawn := stripEscapes(out)
	if !strings.Contains(drawn, "x0-000  x4-004  x8-008") {
		t.Errorf("the list does not run down the columns\ndrawn: %q", drawn)
	}
	// The line is drawn again below the list.
	if i, j := strings.LastIndex(drawn, "x29"), strings.LastIndex(drawn, "> x"); j < i {
		t.Errorf("the line was not drawn again below the list\ndrawn: %q", drawn)
	}
}

// TestCompletionMenu checks isocline's completion menu, which
// WithCompletionMenu binds to Tab (D51). The menu itself is checked against
// the C in menu_test.go. This checks that the edit loop hands it the keys,
// runs the key that closes it, and keeps what it put in the line.
func TestCompletionMenu(t *testing.T) {
	t.Parallel()
	// The menu puts the shortest first, as isocline sorts them, so it reads
	// 1 alpha, 2 alpine, 3 alphabet and 4 alphanumeric.
	words := manyWords("alpha", "alphabet", "alphanumeric", "alpine")
	menu := feedOpts{Completer: words, CompletionMenu: true}
	for _, test := range []struct {
		name string
		keys string
		opt  feedOpts
		want string
	}{
		// Tab puts in "alp", which all four share, and opens the menu. With
		// the preview on nothing is selected, so Enter closes the menu and
		// the edit loop runs it, which finishes the line.
		{"enter with nothing selected", "al" + kTab + kEnter, menu, "alp"},
		{"tab selects the first", "al" + kTab + kTab + kEnter + kEnter, menu, "alpha"},
		{"down selects the next", "al" + kTab + kDown + kDown + kEnter + kEnter, menu, "alpine"},
		{"up selects the last", "al" + kTab + kUp + kEnter + kEnter, menu, "alphanumeric"},
		{"a digit picks an entry", "al" + kTab + "3" + kEnter, menu, "alphabet"},
		{"esc closes the menu", "al" + kTab + kEsc + kEnter, menu, "alp"},
		{"typing takes the preview", "al" + kTab + kTab + "x" + kEnter, menu, "alphax"},
		{"undo takes the pick back", "al" + kTab + "4" + kUndo + kEnter, menu, "alp"},
		{"one answer goes in", "alpi" + kTab + kEnter, menu, "alpine"},
		{"no answer leaves the line", "zz" + kTab + kEnter, menu, "zz"},
		// With WithAutoTab, picking alpha leaves alphabet and alphanumeric,
		// which share nothing more, so the menu opens again and 2 picks
		// alphanumeric.
		{"auto tab opens the menu again", "al" + kTab + "1" + "2" + kEnter,
			feedOpts{Completer: words, CompletionMenu: true, AutoTab: true}, "alphanumeric"},
		{"without auto tab the digit is typed", "al" + kTab + "1" + "2" + kEnter, menu, "alpha2"},
		// Without the option, Tab is readline's complete, which puts in what
		// the answers share and opens no menu.
		{"without the option tab is readline's", "al" + kTab + "1" + kEnter,
			feedOpts{Completer: words}, "alp1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			line, _, _, _ := feedSession(t, test.keys, test.opt)
			if line != test.want {
				t.Errorf("the line is %q, want %q", line, test.want)
			}
		})
	}

	// The menu is numbered, and the number goes in front of each entry.
	_, _, _, out := feedSession(t, "al"+kTab+kEsc+kEnter, menu)
	if drawn := stripEscapes(out); !strings.Contains(drawn, "4 alphanumeric") {
		t.Errorf("the menu was not drawn\ndrawn: %q", drawn)
	}

	// F1 shows the help, and the menu stays open, so 2 still picks alpine.
	// Outside the menu F1 runs nothing, and the 2 would be typed.
	_, _, _, out = feedSession(t, "al"+kTab+"\x1bOP2"+kEnter, menu)
	if drawn := stripEscapes(out); !strings.Contains(drawn, "In the completion menu:") ||
		!strings.HasSuffix(strings.TrimRight(drawn, "\r\n"), "> alpine") {
		t.Errorf("F1 in the menu did not show the help and keep the menu\ndrawn: %q", drawn)
	}

	// Ctrl-C closes the menu, and the edit loop then gives the line up.
	if _, _, c, _ := feedSession(t, "al"+kTab+"\x03", menu); c != key.CtrlC {
		t.Errorf("Ctrl-C in the menu finished the line with %v, want Ctrl-C", c)
	}

	// vi's insert mode binds Tab to the menu as well.
	ev, _ := feedEnv(t, "al"+kTab+"2"+kEnter, menu)
	ev.viMode = true
	e := &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
	ev.runEditLoop(e)
	if got := e.Input.String(); got != "alpine" {
		t.Errorf("in vi insert mode the line is %q, want %q", got, "alpine")
	}
}

// TestCompletionMenuInInputrc checks that an inputrc file goes over
// WithCompletionMenu, and can bind rline-menu-complete without it (D51).
func TestCompletionMenuInInputrc(t *testing.T) {
	t.Parallel()
	words := manyWords("alpha", "alphabet", "alphanumeric", "alpine")
	for _, test := range []struct {
		name, inputrc string
		menu          bool
		want          string
	}{
		{"the file binds tab back", "TAB: complete\n", true, "alp2"},
		{"a file that leaves tab alone keeps the menu", "set bell-style none\n", true, "alpine"},
		{"the file binds the menu", "\"\\C-xm\": rline-menu-complete\n", false, "alpine"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "inputrc")
			if err := os.WriteFile(path, []byte(test.inputrc), 0o600); err != nil {
				t.Fatal(err)
			}
			keys := "al" + kTab + "2" + kEnter
			if !test.menu {
				keys = "al\x18m2" + kEnter
			}
			ev, _ := feedEnv(t, keys, feedOpts{Completer: words, CompletionMenu: test.menu})
			ev.readInputrc(&config{inputrcFile: path, appName: DefaultAppName})
			if len(ev.notices) > 0 {
				t.Errorf("notices: %q", ev.notices)
			}
			e := &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
			ev.runEditLoop(e)
			if got := e.Input.String(); got != test.want {
				t.Errorf("the line is %q, want %q", got, test.want)
			}
		})
	}
}

// TestInputrcModeAfterEditingMode checks that "set editing-mode vi" changes
// the mode that a later "$if mode=vi" tests, as it does in readline, where
// bash 5.3 binds d in vi-command from a file of this shape. In inputrc v0.7.0
// the block was skipped.
func TestInputrcModeAfterEditingMode(t *testing.T) {
	t.Parallel()
	const inputrc = "set editing-mode vi\n" +
		"$if mode=vi\n" +
		"  set keymap vi-command\n" +
		"  \"d\": backward-char\n" +
		"$endif\n"
	path := filepath.Join(t.TempDir(), "inputrc")
	if err := os.WriteFile(path, []byte(inputrc), 0o600); err != nil {
		t.Fatal(err)
	}
	// Esc leaves the cursor on c, d moves it back to b, and x deletes b.
	// Without the binding, d waits for a motion and x cancels it.
	ev, _ := feedEnv(t, "abc"+kEsc+"dx"+kEnter, feedOpts{})
	ev.readInputrc(&config{inputrcFile: path, appName: DefaultAppName})
	e := &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
	ev.runEditLoop(e)
	if got := e.Input.String(); got != "ac" {
		t.Errorf("the line is %q, want %q", got, "ac")
	}
}

// TestOperateAndGetNext checks that Ctrl-O accepts the line, and that the
// next line starts with the history line after it, as readline does under
// bash: here the line after "second" is "third".
func TestOperateAndGetNext(t *testing.T) {
	t.Parallel()
	ev, _ := feedEnv(t, "\x10\x10\x0f"+kEnter, feedOpts{})
	for _, h := range []string{"first", "second", "third"} {
		ev.history.push(h)
	}
	e := &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
	if c := ev.runEditLoop(e); c != key.Enter || e.Input.String() != "second" {
		t.Fatalf("Ctrl-O ended the line with %#x and %q, want Enter and %q", c, e.Input.String(), "second")
	}
	// editLine keeps the accepted line as the newest one.
	ev.history.push("second")
	e = &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
	ev.runEditLoop(e)
	if got := e.Input.String(); got != "third" {
		t.Errorf("the next line is %q, want %q", got, "third")
	}
	// The line after that starts empty again.
	if ev.hasNext {
		t.Errorf("a line that Enter accepted asks for the history line %q next", ev.next)
	}
}

// TestReReadInitFile checks that C-x C-r reads the inputrc file again and
// uses its new bindings at once, and that without a file it changes nothing.
func TestReReadInitFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "inputrc")
	write := func(macro string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`"\C-xa": "`+macro+`"`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("one")
	keys := "\x18a\x18\x12\x18a" + kEnter
	ev, _ := feedEnv(t, keys, feedOpts{})
	ev.readInputrc(&config{inputrcFile: path, appName: DefaultAppName})
	write("two")
	e := &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
	ev.runEditLoop(e)
	if got := e.Input.String(); got != "onetwo" {
		t.Errorf("the line is %q, want %q", got, "onetwo")
	}

	// With no inputrc file, the keys keep readline's defaults.
	line, _, _, _ := feedSession(t, "ab"+"\x18\x12"+kEnter, feedOpts{})
	if line != "ab" {
		t.Errorf("without a file the line is %q, want %q", line, "ab")
	}
}

// TestScreenLineUsesTheTerminal checks that next-screen-line moves by the
// width of the terminal, counting the prompt, as readline does. On a
// terminal 20 wide, with the prompt "> " two wide, the row below the start
// of the line begins at the 18th character, and the cursor lands on the 20th.
func TestScreenLineUsesTheTerminal(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "inputrc")
	if err := os.WriteFile(path, []byte(`"\C-xn": next-screen-line`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ev, _ := feedEnv(t, strings.Repeat("a", 30)+kCtrlA+"\x18n"+"X"+kEnter, feedOpts{})
	ev.readInputrc(&config{inputrcFile: path, appName: DefaultAppName})
	ev.term = newTerm(&bytes.Buffer{}, termOptions{Sizer: fixedSize{cols: 20, rows: 24}})
	ev.bb = newBBCode(ev.term)
	e := &editor.Editor{Opts: ev.opts, TermW: 20, CurRows: 1}
	ev.runEditLoop(e)
	want := strings.Repeat("a", 20) + "X" + strings.Repeat("a", 10)
	if got := e.Input.String(); got != want {
		t.Errorf("the line is %q, want %q", got, want)
	}
}

// TestCompletionWithManyAnswers checks a list as long as readline's
// completion-query-items, which is 100. readline asks before it lists them,
// and stops at each screenful with --More--.
func TestCompletionWithManyAnswers(t *testing.T) {
	t.Parallel()
	words := wordsStartingWith("x", 300)
	complete := feedOpts{Completer: manyWords(words...)}
	const ask = "Display all 300 possibilities? (y or n)"

	_, _, _, out := feedSession(t, "x"+kTab+kTab+"n"+kEnter, complete)
	drawn := stripEscapes(out)
	if !strings.Contains(drawn, ask) {
		t.Errorf("readline's question was never asked\ndrawn: %q", drawn)
	}
	if strings.Contains(drawn, words[0]) {
		t.Errorf("n was answered and the list was drawn\ndrawn: %q", drawn)
	}

	// 300 words of six columns are 34 rows of nine, and a screen of 24
	// shows 23 rows before it asks for more. The columns run down, so the
	// 23rd word is at the foot of the first screenful and the 34th is on the
	// next. q stops there.
	sorted := slices.Sorted(slices.Values(words))
	line, _, _, out := feedSession(t, "x"+kTab+kTab+"y"+"q"+kEnter, complete)
	drawn = stripEscapes(out)
	if !strings.Contains(drawn, "--More--") {
		t.Errorf("a list taller than the screen did not stop\ndrawn: %q", drawn)
	}
	if !strings.Contains(drawn, sorted[22]) || strings.Contains(drawn, sorted[33]) {
		t.Errorf("q did not stop the list after the first screenful\ndrawn: %q", drawn)
	}
	if line != "x" {
		t.Errorf("the line is %q, want %q", line, "x")
	}

	// Space shows the rest.
	_, _, _, out = feedSession(t, "x"+kTab+kTab+"y"+" "+kEnter, complete)
	if drawn := stripEscapes(out); !strings.Contains(drawn, sorted[33]) {
		t.Errorf("space did not show the rest of the list\ndrawn: %q", drawn)
	}
	// Another key is no answer, and the bell rings until one comes.
	if line, _, _, _ := feedSession(t, "x"+kTab+kTab+"z"+"n"+kEnter, complete); line != "x" {
		t.Errorf("the line is %q after a key that is no answer, want %q", line, "x")
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
		// Only Right takes a hint (D46).
		{"the end key leaves it", "sel" + kEnd + kEnter, "sel"},
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
			// Every answer carries on "select", so that much goes in.
			name:  "as much as they share",
			words: []string{"select", "selected", "selecting"},
			keys:  "se" + kTab + kEnter,
			want:  "select", cursor: 6,
		},
		{
			name:  "nothing when they share nothing",
			words: []string{"select", "settle"},
			keys:  "se" + kTab + kEnter,
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

// TestKeepInterrupted checks WithKeepInterrupted (D52): Ctrl-C leaves the
// line on the screen with ^C after it, as GNU readline does under psql, and
// the next prompt starts below it. echo-control-characters off leaves out
// the ^C. Without the option the line is erased, as isocline erases it.
// Either way ReadLine returns ErrInterrupted and the history keeps nothing.
func TestKeepInterrupted(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		keep bool
		vars map[string]any
		want string
	}{
		{"kept with ^C", true, nil, "> abc^C"},
		{"kept without ^C", true, map[string]any{"echo-control-characters": false}, "> abc"},
		{"erased", false, nil, "> "},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ev, sink := feedEnv(t, "abc\x03", feedOpts{})
			ev.keepInterrupted, ev.vars = test.keep, test.vars
			r := &Session{env: ev, canEdit: true}
			if _, err := r.ReadLine(""); !errors.Is(err, ErrInterrupted) {
				t.Fatalf("ReadLine gave %v, want ErrInterrupted", err)
			}
			ev.term.flush()
			// The last row is what the screen keeps, and the cursor moved
			// below it.
			out := sink.String()
			if !strings.HasSuffix(out, "\n") {
				t.Errorf("the cursor did not move below the line: %q", out)
			}
			if last := lastRow(out); last != test.want {
				t.Errorf("the last row on the screen is %q, want %q\ndrawn: %q", last, test.want, out)
			}
			if slices.Contains(ev.history.all(), "abc") {
				t.Errorf("the history keeps the line that Ctrl-C gave up: %q", ev.history.all())
			}
		})
	}
}

// TestKeepInterruptedKeepsEveryRow checks that a statement over two rows
// stays whole on the screen when Ctrl-C gives it up with
// WithKeepInterrupted: the cursor goes to its end, and ^C follows the last
// row.
func TestKeepInterruptedKeepsEveryRow(t *testing.T) {
	t.Parallel()
	ev, sink := feedEnv(t, "select"+kEnter+"from t"+kCtrlA+"\x03", feedOpts{Continue: func(string) bool { return true }})
	ev.keepInterrupted = true
	r := &Session{env: ev, canEdit: true}
	if _, err := r.ReadLine(""); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("ReadLine gave %v, want ErrInterrupted", err)
	}
	ev.term.flush()
	out := sink.String()
	if last := lastRow(out); last != "> from t^C" {
		t.Errorf("the last row on the screen is %q, want %q\ndrawn: %q", last, "> from t^C", out)
	}
	if i := strings.LastIndex(out, "select"); i < 0 || i > strings.LastIndex(out, "from t") {
		t.Errorf("the first row was not drawn above the last one\ndrawn: %q", out)
	}
}

// lastRow returns what the screen shows on the row before the last line
// break of out. It follows a carriage return, a move to the right, an erase
// to the end of the row and the characters written, which is all that
// drawing one row uses, and skips any other escape sequence.
func lastRow(out string) string {
	out = strings.TrimSuffix(out, "\n")
	if i := strings.LastIndex(out, "\n"); i >= 0 {
		out = out[i+1:]
	}
	var row []rune
	col := 0
	rs := []rune(out)
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; {
		case r == '\r':
			col = 0
		case r == 0x1b && i+1 < len(rs) && rs[i+1] == '[':
			j := i + 2
			for j < len(rs) && (rs[j] < 0x40 || rs[j] > 0x7e) {
				j++
			}
			if j >= len(rs) {
				return string(row)
			}
			arg := string(rs[i+2 : j])
			switch rs[j] {
			case 'C':
				n, err := strconv.Atoi(arg)
				if err != nil {
					n = 1
				}
				col += n
			case 'K':
				row = row[:min(col, len(row))]
			}
			i = j
		default:
			for len(row) <= col {
				row = append(row, ' ')
			}
			row[col] = r
			col++
		}
	}
	return string(row)
}

// TestSetCompleterReplacesTheCompleter checks that a completer can be changed
// after the reader exists, which is what a program whose completions depend on
// something that changes while it runs needs.
func TestSetCompleterReplacesTheCompleter(t *testing.T) {
	t.Parallel()
	ev, _ := feedEnv(t, "se"+kTab+kEnter, feedOpts{})
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

// TestSessionMatchesReadline replays each case that tools/probe-readline.c
// recorded from GNU readline 8.3 through the whole loop: the decoder, the
// keymap, the line and the editor. The package internal/readline checks each
// key against the recording. This checks that the keys reach the commands
// and the line reaches the editor, so it compares the line and the cursor at
// the end of each case.
//
// It runs in the readline configuration of D45: no rows, no hints, no brace
// insertion and no highlighting, which readline does not have.
func TestSessionMatchesReadline(t *testing.T) {
	// tilde-expand reads HOME, and the probe set it to /home/probe.
	t.Setenv("HOME", "/home/probe")
	const path = "testdata/readline-emacs.txt"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	type rlCase struct {
		name    string
		history []string
		binds   map[string]inputrc.Bind
		keys    []string
		line    string
		point   int
		done    bool
		words   []string
		listed  []string
		rc      []string
		printed []string
	}
	var cases []*rlCase
	at := func(name string) *rlCase {
		if len(cases) == 0 || cases[len(cases)-1].name != name {
			cases = append(cases, &rlCase{name: name, binds: map[string]inputrc.Bind{}})
		}
		return cases[len(cases)-1]
	}
	text := func(h string) string {
		if h == "-" {
			return ""
		}
		return string(mustHex(t, h))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n")[1:] {
		f := strings.Fields(line)
		switch {
		case len(f) == 3 && f[0] == "history":
			c := at(f[1])
			c.history = append(c.history, text(f[2]))
		case len(f) == 4 && f[0] == "bind":
			at(f[1]).binds[inputrc.Unescape(text(f[3]))] = inputrc.Bind{Action: f[2]}
		case len(f) == 3 && f[0] == "rc":
			c := at(f[1])
			c.rc = append(c.rc, text(f[2]))
		case len(f) == 3 && f[0] == "prompt":
			// internal/readline compares the prompts. Here only the line
			// and the cursor are.
		case len(f) == 3 && f[0] == "out":
			// internal/readline compares what the commands print. Here the
			// first line of it has to be drawn.
			if c := at(f[1]); !c.done {
				first, _, _ := strings.Cut(strings.TrimLeft(text(f[2]), "\r\n"), "\n")
				c.printed = append(c.printed, first)
			}
		case len(f) >= 2 && (f[0] == "words" || f[0] == "list"):
			c := at(f[1])
			for _, h := range f[2:] {
				if f[0] == "words" {
					c.words = append(c.words, text(h))
				} else if !c.done {
					c.listed = append(c.listed, text(h))
				}
			}
		case len(f) == 6:
			c := at(f[0])
			if c.done {
				continue
			}
			keys := text(f[5])
			if f[1] != "-" {
				keys = text(f[1])
			}
			c.keys = append(c.keys, keys)
			c.line, c.point, c.done = text(f[5]), mustInt(t, f[2]), f[4] == "1"
		default:
			t.Fatalf("%s: cannot read %q", path, line)
		}
	}
	if len(cases) < 60 {
		t.Fatalf("%s holds %d cases, expected at least 60", path, len(cases))
	}
	for _, c := range cases {
		binds := c.binds
		// Typing pauses between the keys, as it does between the steps of the
		// probe, so that the decoder does not join the end of one key to the
		// start of the next.
		keys := ""
		pauses := map[int]bool{}
		for _, k := range c.keys {
			pauses[len(keys)] = true
			keys += k
		}
		if !c.done {
			// Enter hands the line back as it stands, so that it can be
			// compared.
			pauses[len(keys)] = true
			keys += kEnter
		}
		ev, sink := feedEnv(t, keys, feedOpts{ZeroIsKey: true, Pauses: pauses, Completer: wordsCompleter(c.words)})
		ev.keymap = emacsKeymap()
		bindAll(ev.keymap, binds)
		if c.rc != nil {
			// The lines go through the Session's own reading of an inputrc
			// file, with the case's bindings before them.
			rc := ""
			for seq, b := range c.binds {
				rc += fmt.Sprintf("\"%s\": %s\n", inputrc.Escape(seq), b.Action)
			}
			rc += strings.Join(c.rc, "\n") + "\n"
			path := filepath.Join(t.TempDir(), "inputrc")
			if err := os.WriteFile(path, []byte(rc), 0o600); err != nil {
				t.Fatal(err)
			}
			ev.readInputrc(&config{inputrcFile: path, appName: "rline-probe"})
			if len(ev.notices) > 0 {
				t.Errorf("%s: the notices are %q", c.name, ev.notices)
			}
		}
		ev.multiline = false
		ev.opts.AutoPair = false
		// The probe has no prompt, and next-screen-line counts the prompt.
		ev.promptMarker = ""
		for _, h := range c.history {
			ev.history.push(h)
		}
		e := &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
		ended := ev.runEditLoop(e)
		if c.done {
			// readline handed the line back, by accept-line or at the end of
			// the input.
			if ended != key.Enter && ended != key.CtrlD {
				t.Errorf("%s: the line was ended by %#x, want Enter or Ctrl-D", c.name, ended)
			}
			continue
		}
		if ended != key.Enter {
			t.Errorf("%s: the line was ended by %#x, want Enter", c.name, ended)
			continue
		}
		if got := e.Input.String(); got != c.line || e.Pos != c.point {
			t.Errorf("%s: the line is %q with the cursor at %d, and readline left %q with the cursor at %d",
				c.name, got, e.Pos, c.line, c.point)
		}
		ev.term.flush()
		for _, w := range c.listed {
			if !strings.Contains(sink.String(), w) {
				t.Errorf("%s: readline listed %q, and it was never drawn", c.name, w)
			}
		}
		for _, w := range c.printed {
			if !strings.Contains(sink.String(), w) {
				t.Errorf("%s: readline printed %q, and it was never drawn", c.name, w)
			}
		}
	}
}

// wordsCompleter completes the word before the cursor from words, as the
// probe's completion function does. A word ends at readline's default word
// break characters, and a single completion is followed by a space.
func wordsCompleter(words []string) Completer {
	if words == nil {
		return nil
	}
	return CompleterFunc(func(c *Completion, prefix string) {
		start := strings.LastIndexAny(prefix, " \t\n\"\\'`@$><=;|&{(") + 1
		for _, w := range words {
			if strings.HasPrefix(w, prefix[start:]) {
				c.AddCandidate(Candidate{Replacement: w, DeleteBefore: len(prefix) - start, Suffix: " "})
			}
		}
	})
}

// testInputrc is an inputrc file for the tests: a macro, bindings in two
// keymaps, bindings behind $if, a variable that rline reads, one it does not
// know, and a command that it does not have.
const testInputrc = `set show-all-if-ambiguous on
set no-such-variable on
"\C-xa": "hello "
"\C-xq": no-such-command
set keymap emacs-ctlx
"y": capitalize-word
set keymap emacs
$if rline
"\C-xz": unix-line-discard
$endif
$if other
"\C-xw": kill-line
$endif
`

// TestSessionReadsInputrc checks that the bindings, the macros and the
// variables of an inputrc file reach the keys, and that what rline does not
// support is listed (D41, D43).
func TestSessionReadsInputrc(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "inputrc")
	if err := os.WriteFile(path, []byte(testInputrc), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, keys, want string
	}{
		{"a macro types its text", "\x18a\x18a" + kEnter, "hello hello "},
		{"a binding in the Ctrl-X keymap", "abc" + kCtrlA + "\x18y" + kEnter, "Abc"},
		{"a binding for this program", "abc\x18z" + kEnter, ""},
		{"a binding for another program", "abc\x18w" + kEnter, "abc"},
		{"the defaults stay", "abc" + kCtrlA + kCtrlK + kEnter, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ev, _ := feedEnv(t, test.keys, feedOpts{})
			ev.readInputrc(&config{inputrcFile: path, appName: DefaultAppName})
			e := &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
			ev.runEditLoop(e)
			if got := e.Input.String(); got != test.want {
				t.Errorf("the line is %q, want %q", got, test.want)
			}
		})
	}

	// show-all-if-ambiguous lists the completions at the first Tab.
	ev, sink := feedEnv(t, "x"+kTab+kEnter, feedOpts{Completer: manyWords(wordsStartingWith("x", 5)...)})
	ev.readInputrc(&config{inputrcFile: path, appName: DefaultAppName})
	ev.runEditLoop(&editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1})
	ev.term.flush()
	if !strings.Contains(stripEscapes(sink.String()), "x4-004") {
		t.Errorf("show-all-if-ambiguous did not list the completions at once\ndrawn: %q", sink.String())
	}

	want := []string{
		"the command no-such-command is not supported",
		"the variable no-such-variable is not supported",
	}
	if !slices.Equal(ev.notices, want) {
		t.Errorf("the notices are %q, want %q", ev.notices, want)
	}
}

// TestSessionReadsTheUsersInputrc checks that WithUserInputrc reads the file
// that $INPUTRC names, as readline does, and that nothing is read without it.
func TestSessionReadsTheUsersInputrc(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inputrc")
	if err := os.WriteFile(path, []byte(testInputrc), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INPUTRC", path)
	for _, read := range []bool{true, false} {
		p, err := New(WithUserInputrc(read), WithStdin(strings.NewReader("")), WithStdout(&bytes.Buffer{}))
		if err != nil {
			t.Fatal(err)
		}
		if got := len(p.Notices()) > 0; got != read {
			t.Errorf("with WithUserInputrc(%v) the notices are %q", read, p.Notices())
		}
		_ = p.Close()
	}

	// When $INPUTRC names a file that is missing, readline reads nothing,
	// and does not go on to ~/.inputrc. The home here holds one that the
	// search would find, and that would give a notice.
	home := t.TempDir()
	// Windows names the file _inputrc, as readline does there.
	name := ".inputrc"
	if runtime.GOOS == "windows" {
		name = "_inputrc"
	}
	if err := os.WriteFile(filepath.Join(home, name), []byte(testInputrc), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("INPUTRC", filepath.Join(home, "missing"))
	p, err := New(WithUserInputrc(true), WithStdin(strings.NewReader("")), WithStdout(&bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	if n := p.Notices(); len(n) > 0 {
		t.Errorf("with $INPUTRC naming a missing file, ~/.inputrc was read: the notices are %q", n)
	}
	// Without $INPUTRC, ~/.inputrc is the one read.
	t.Setenv("INPUTRC", "")
	q, err := New(WithUserInputrc(true), WithStdin(strings.NewReader("")), WithStdout(&bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	if len(q.Notices()) == 0 {
		t.Error("with no $INPUTRC, ~/.inputrc was not read")
	}
}

// TestViMode checks that WithViMode edits as vi does, and that a line that
// switches the mode leaves the next line in that mode, as readline does.
func TestViMode(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, keys, want string
	}{
		{"typing goes in as in emacs", "abc" + kEnter, "abc"},
		{"esc and x delete in command mode", "abc" + kEsc + "0x" + kEnter, "bc"},
		{"dw deletes a word", "one two" + kEsc + "0dw" + kEnter, "two"},
		{"an arrow still moves in insert mode", "ac" + kLeft + "b" + kEnter, "abc"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ev, _ := feedEnv(t, test.keys, feedOpts{})
			ev.viMode = true
			e := &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
			ev.runEditLoop(e)
			if got := e.Input.String(); got != test.want {
				t.Errorf("the line is %q, want %q", got, test.want)
			}
		})
	}

	// Ctrl-E in command mode switches to emacs, and the next line starts
	// there: Ctrl-A goes to the start instead of being typed.
	ev, _ := feedEnv(t, "x"+kEsc+"\x05"+kEnter+"bc"+kCtrlA+"a"+kEnter, feedOpts{})
	ev.viMode = true
	ev.runEditLoop(&editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1})
	if ev.viMode {
		t.Fatal("the line switched to emacs, and the Session kept vi")
	}
	e := &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
	ev.runEditLoop(e)
	if got := e.Input.String(); got != "abc" {
		t.Errorf("the second line is %q, want %q", got, "abc")
	}
}
