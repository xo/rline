package rline

import (
	"bytes"
	"strings"
	"testing"
)

// What happens when the window changes size.
//
// Nothing else covers this. The recorded corpora call functions at a fixed
// size, and the recorded sessions never resize the pseudo-terminal. The shape
// of these tests is taken from jline3, whose LineReaderResizeTest checks that
// a resize does not draw the line twice, which is the fault this code can have.

// resizableSize reports a size a test can change between calls.
type resizableSize struct {
	cols int
	rows int
}

// size returns the width and height.
func (s *resizableSize) size() (int, int, bool) { return s.cols, s.rows, true }

// newResizeEnv returns an editor drawing to a buffer, and the size it reads.
func newResizeEnv(t *testing.T) (*env, *editor, *bytes.Buffer, *resizableSize) {
	t.Helper()
	sink := &bytes.Buffer{}
	sz := &resizableSize{cols: 80, rows: 24}
	tm := newTerm(sink, termOptions{NoColor: true, Sizer: sz})
	h := &history{}
	h.loadFrom("", DefaultHistoryEntries)
	ev := &env{
		term:          tm,
		tty:           newTTY(&feedKeys{}),
		bb:            newBBCode(tm),
		history:       h,
		completions:   &completions{},
		promptMarker:  "> ",
		cpromptMarker: "> ",
		opts:          editOptions{MatchBraces: DefaultMatchBraces, AutoBraces: DefaultAutoBraces, MultilineEOL: DefaultMultilineEOL},
		noHighlight:   true,
		noBraceMatch:  true,
		noHint:        true,
	}
	e := &editor{opts: ev.opts, termW: 80, curRows: 1}
	return ev, e, sink, sz
}

// TestResizeReportsAChange checks that a resize is only acted on when the
// width actually moved, since the redraw it causes is what makes a line
// flicker.
func TestResizeReportsAChange(t *testing.T) {
	t.Parallel()
	ev, e, sink, sz := newResizeEnv(t)
	e.input.replace("hello")
	e.pos = 5
	ev.refresh(e)
	sink.Reset()

	if ev.resize(e) {
		t.Error("a resize to the same width reported a change")
	}
	if sink.Len() != 0 {
		t.Errorf("a resize to the same width wrote %q", sink.String())
	}
	sz.cols = 40
	if !ev.resize(e) {
		t.Error("a resize to a new width reported no change")
	}
	if e.termW != 40 {
		t.Errorf("the editor kept a width of %d, want 40", e.termW)
	}
	if sink.Len() == 0 {
		t.Error("a resize to a new width drew nothing")
	}
}

// TestResizeDrawsTheLineOnce checks that a line is not drawn twice when the
// window narrows enough to wrap it.
//
// This is the fault jline3 has a regression test for: a resize that redraws
// without accounting for the rows already on screen leaves the old copy above
// the new one.
func TestResizeDrawsTheLineOnce(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		from int
		to   int
	}{
		{"narrower, so the line wraps", 80, 40},
		{"narrower again, so it wraps further", 40, 20},
		{"wider, so it unwraps", 40, 80},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ev, e, sink, sz := newResizeEnv(t)
			sz.cols = test.from
			e.termW = test.from
			// Long enough to wrap at every width being tested, and made of a
			// word that can be counted in the output.
			e.input.replace(strings.Repeat("abcde ", 12))
			e.pos = e.input.length()
			ev.refresh(e)
			sink.Reset()

			sz.cols = test.to
			ev.resize(e)

			// Counting has to be done on the text with the escape sequences
			// and the row breaks taken out, because a wrap can fall in the
			// middle of the word and a naive count then misses it.
			if got, want := countWord(sink.String(), "abcde"), 12; got != want {
				t.Errorf("the line was drawn with %d copies of the word, want %d.\nWhat it wrote:\n%q",
					got, want, sink.String())
			}
		})
	}
}

// TestResizeKeepsThePosition checks that the cursor stays on the same
// character when the window changes width, even though it moves to a
// different row and column on screen.
func TestResizeKeepsThePosition(t *testing.T) {
	t.Parallel()
	ev, e, _, sz := newResizeEnv(t)
	e.input.replace(strings.Repeat("x", 100))
	e.pos = 50
	ev.refresh(e)

	_, before := ev.rowCol(e)
	sz.cols = 40
	ev.resize(e)
	_, after := ev.rowCol(e)

	if e.pos != 50 {
		t.Errorf("the cursor moved to %d, want 50", e.pos)
	}
	// A narrower window puts the same character further down the screen.
	if after.row <= before.row {
		t.Errorf("the cursor was on row %d at 80 columns and row %d at 40, want further down",
			before.row, after.row)
	}
}

// TestResizeWithContentBelowTheLine checks a resize while a completion menu or
// a search is showing, which is when the rows below the line have to be
// measured as well.
func TestResizeWithContentBelowTheLine(t *testing.T) {
	t.Parallel()
	ev, e, sink, sz := newResizeEnv(t)
	e.input.replace("select")
	e.pos = 6
	e.extra.replace("[ic-info]1. one[/]\n[ic-info]2. two[/]\n")
	ev.refresh(e)
	sink.Reset()

	sz.cols = 40
	ev.resize(e)

	out := sink.String()
	for _, want := range []string{"select", "1. one", "2. two"} {
		if n := strings.Count(out, want); n != 1 {
			t.Errorf("%q appears %d times after the resize, want 1.\nWhat it wrote:\n%q",
				want, n, out)
		}
	}
}

// countWord counts a word in what was drawn.
//
// Everything that a row boundary puts in the middle of the text is taken out
// first: the escape sequences, the row breaks, the prompt drawn in front of
// every row, and the mark drawn at the end of a row that filled the terminal.
// Without that a word split across two rows is missed, and the count says the
// line was drawn less than once.
func countWord(out, word string) int {
	flat := stripEscapes(out)
	flat = strings.NewReplacer(
		"\r", "", "\n", "",
		"> ", "", "| ", "",
		"\u2190", "", "\u21b5", "",
	).Replace(flat)
	return strings.Count(flat, word)
}
