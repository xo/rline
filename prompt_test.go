package rline

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xo/rline/internal/editor"
)

// --------------------------------------------------------------------------
// editline_test.go

// refreshProbePath is where tools/build-probe-refresh.sh puts the probe.
const refreshProbePath = ".build/probe-refresh"

// refreshCorpusPath returns where the recordings this build compares against
// live.
//
// This corpus is split, unlike the others, because the redraw has a compile
// time branch in it: the mark at the end of a wrapped row is a return symbol
// on macOS and a left arrow everywhere else, and 336 of the 1584 recorded
// redraws contain it.
//
// The split is by that branch and not by the system. Keying it on the system
// would be finer than the thing it stands for, and would demand a separate
// recording from every system that compiles the same branch. Windows and Linux
// draw the same glyph, so they share one set, and only macOS needs its own.
func refreshCorpusPath() string {
	return filepath.Join("testdata", corpusVariant, "refresh.txt")
}

// The settings the probe redraws under.
var (
	refreshTexts = []string{
		"", "a", "hello", "hello world",
		"0123456789012345678901234567890123456789012345",
		"line1\nline2", "a\nb\nc\nd\ne\nf\ng",
		"(unbalanced", "[matched]", "日本",
	}
	refreshPrompts = []string{"", "ps"}
	refreshHints   = []string{"", "hint"}
	refreshExtras  = []string{"", "menu line", "two\nlines"}
)

// TestRefreshMatchesC replays the script that tools/probe-refresh.c runs and
// checks that the port draws the same bytes and leaves the same state.
func TestRefreshMatchesC(t *testing.T) {
	if *update {
		refreshRegenerate(t)
	}
	path := refreshCorpusPath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no recordings for the %q build: %v\n"+
			"Record them on a machine that compiles the same wrap mark, which "+
			"for %q means macOS and otherwise means anything but macOS, and "+
			"that has a C compiler:\n"+
			"  ./tools/build-probe-refresh.sh && go test . -run TestRefreshMatchesC -update",
			corpusVariant, err, corpusVariant)
	}
	want := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	got := refreshReplay(t)
	if len(got) != len(want) {
		t.Errorf("the replay produced %d lines and the corpus holds %d", len(got), len(want))
	}
	bad := 0
	for i := range max(len(got), len(want)) {
		g, w := lineAt(got, i), lineAt(want, i)
		if g == w {
			continue
		}
		bad++
		if bad <= 20 {
			t.Errorf("%s:%d\n  got:  %s\n  want: %s", path, i+1, g, w)
		}
	}
	if bad > 20 {
		t.Errorf("%d differences in total, 20 shown", bad)
	}
	if bad == 0 {
		t.Logf("checked %d redraws", (len(want)+1)/2)
	}
}

// refreshReplay runs the same script as the C probe.
func refreshReplay(t *testing.T) []string {
	t.Helper()
	restore := saveEnv(t)
	defer restore()
	setTermEnv("", "xterm-256color", "")

	var sink bytes.Buffer
	tm := newTerm(&sink, termOptions{Sizer: fixedSize{cols: 40, rows: 6}})
	ev := &env{
		term:          tm,
		bb:            newBBCode(tm),
		promptMarker:  "> ",
		cpromptMarker: "| ",
		opts: editor.EditOptions{
			MatchPairs:   "()[]{}",
			AutoPairs:    "()[]{}",
			MultilineEOL: '\\',
		},
		noHighlight: true,
	}
	e := &editor.Editor{}

	out := make([]string, 0, 3200)
	emit := func() string {
		s := escapeBB(sink.Bytes())
		sink.Reset()
		return s
	}
	caseno := 0
	for _, text := range refreshTexts {
		step := 1
		if len(text) > 8 {
			step = 7
		}
		for pos := 0; pos <= len(text); pos += step {
			for _, prompt := range refreshPrompts {
				for _, hint := range refreshHints {
					for _, extra := range refreshExtras {
						for _, noIndent := range []bool{false, true} {
							for _, noBrace := range []bool{false, true} {
								e.Input.Replace(text)
								e.Hint.Reset()
								e.Hint.WriteString(hint)
								e.Extra.Replace(extra)
								e.HintHelp.Reset()
								e.Pos = pos
								e.TermW = 40
								e.CurRows = 1
								e.CurRow = 0
								e.PromptText = prompt
								ev.noMultilineIndent = noIndent
								ev.noBraceMatch = noBrace
								tm.flush()
								out = append(out, emit())
								ev.refresh(e)
								tm.flush()
								out = append(out, fmt.Sprintf("refresh %d %s %d %d %d %s",
									caseno, emit(), e.CurRows, e.CurRow, e.Pos,
									escapeBB([]byte(e.Input.String()))))
								caseno++
							}
						}
					}
				}
			}
		}
	}
	return out
}

// refreshRegenerate runs the C probe and writes the corpus.
//
// It refuses to run anywhere but Linux and macOS. The "default" set is shared
// by every system that is not macOS, so a system that recorded it would
// overwrite the recording the others compare against. Today that cannot happen
// by accident, because the probe opens a pseudo-terminal and so builds nowhere
// else, but that is a property of the probe rather than a decision, and it
// stays true only until someone writes a probe that does not need one.
func refreshRegenerate(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Fatalf("refusing to record on %s: the %q recordings are shared with "+
			"other systems, and only linux and darwin may write them",
			runtime.GOOS, corpusVariant)
	}
	if _, err := os.Stat(refreshProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-refresh.sh", refreshProbePath)
	}
	out, err := exec.Command(refreshProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	path := refreshCorpusPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	t.Logf("wrote %s: %d bytes", path, len(out))
}

// --------------------------------------------------------------------------
// editlinehelp_test.go

// helpProbePath is where tools/build-probe-help.sh puts the probe, and
// helpDefaultProbePath the second one it builds on macOS, which has the
// branch turned off so that the other variant can be recorded from here too.
const (
	helpProbePath        = ".build/probe-help"
	helpDefaultProbePath = ".build/probe-help-default"
)

// helpCorpusPath is where the lines the C help screen is built from are kept
// for this build.
//
// Three rows of the help name a different key on macOS, and one of them is
// two rows there rather than one, so the recording is per branch in the same
// way the redraw is. corpusVariant comes from the same place, beside the wrap
// mark, so the two cannot disagree about which branch this is.
func helpCorpusPath() string {
	return filepath.Join("testdata", corpusVariant, "help.txt")
}

// TestHelpMatchesC checks the help screen against the C, line by line.
//
// It is a lot of literal text, and a mistyped key name or a missing space
// would never show up anywhere else, because nothing else reads it and no
// recorded session presses F1. The markup is compared as it is handed to the
// printer rather than as it comes out, since rendering it is bbcode's job
// and has its own corpus.
func TestHelpMatchesC(t *testing.T) {
	if *update {
		regenerateHelp(t)
	}
	b, err := os.ReadFile(helpCorpusPath())
	if err != nil {
		t.Fatalf("reading %s: %v\nThis build has no recording for its branch. Record one with\n  ./tools/build-probe-help.sh && go test . -run TestHelpMatchesC -update",
			helpCorpusPath(), err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")

	var wantBanner string
	want := make([]string, 0, len(lines))
	for i, line := range lines {
		kind, body, found := strings.Cut(line, " ")
		if !found {
			t.Fatalf("%s:%d: cannot read %q", helpCorpusPath(), i+1, line)
		}
		switch kind {
		case "banner":
			wantBanner = string(mustHex(t, body))
		case "row":
			want = append(want, string(mustHex(t, body)))
		default:
			t.Fatalf("%s:%d: unknown kind %q", helpCorpusPath(), i+1, kind)
		}
	}
	if wantBanner == "" {
		t.Fatal("the corpus holds no banner")
	}
	if len(want) < 40 {
		t.Fatalf("the corpus holds %d rows, which is too few", len(want))
	}

	if got := helpBanner(); got != wantBanner {
		t.Errorf("the banner differs.\n got %q\nwant %q", got, wantBanner)
	}
	got := helpLines()
	if len(got) != len(want) {
		t.Fatalf("the help has %d rows, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("row %d differs.\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

// regenerateHelp runs the C probe and writes the corpus for this branch.
//
// On macOS it writes the other branch as well, from the second probe that is
// built with the branch turned off. The help table is the only thing that
// probe reads, and nothing else in the C reaches it, so turning the branch
// off gives exactly the table the other build has. That is why one machine
// can record both, unlike the redraw, where the recording comes from running
// the whole editor.
func regenerateHelp(t *testing.T) {
	t.Helper()
	record(t, helpProbePath, helpCorpusPath())
	if _, err := os.Stat(helpDefaultProbePath); err == nil {
		record(t, helpDefaultProbePath, filepath.Join("testdata", "default", "help.txt"))
	}
}

// record runs a probe and writes what it printed.
func record(t *testing.T, probe, path string) {
	t.Helper()
	if _, err := os.Stat(probe); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-help.sh", probe)
	}
	out, err := exec.Command(probe).Output()
	if err != nil {
		t.Fatalf("running %s: %v", probe, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	t.Logf("wrote %s: %d bytes", path, len(out))
}

// --------------------------------------------------------------------------
// resize_test.go

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
func newResizeEnv(t *testing.T) (*env, *editor.Editor, *bytes.Buffer, *resizableSize) {
	t.Helper()
	sink := &bytes.Buffer{}
	sz := &resizableSize{cols: 80, rows: 24}
	tm := newTerm(sink, termOptions{NoColor: true, Sizer: sz})
	h := &history{}
	_ = h.loadFrom("", DefaultHistoryEntries)
	ev := &env{
		term:          tm,
		tty:           newTTY(&feedKeys{}),
		bb:            newBBCode(tm),
		history:       h,
		completions:   &completions{},
		promptMarker:  "> ",
		cpromptMarker: "> ",
		opts:          editor.EditOptions{MatchPairs: DefaultMatchPairs, AutoPairs: DefaultAutoPairs, MultilineEOL: DefaultMultilineEOL},
		noHighlight:   true,
		noBraceMatch:  true,
		noHint:        true,
	}
	e := &editor.Editor{Opts: ev.opts, TermW: 80, CurRows: 1}
	return ev, e, sink, sz
}

// TestResizeReportsAChange checks that a resize is only acted on when the
// width actually moved, since the redraw it causes is what makes a line
// flicker.
func TestResizeReportsAChange(t *testing.T) {
	t.Parallel()
	ev, e, sink, sz := newResizeEnv(t)
	e.Input.Replace("hello")
	e.Pos = 5
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
	if e.TermW != 40 {
		t.Errorf("the editor kept a width of %d, want 40", e.TermW)
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
			e.TermW = test.from
			// Long enough to wrap at every width being tested, and made of a
			// word that can be counted in the output.
			e.Input.Replace(strings.Repeat("abcde ", 12))
			e.Pos = e.Input.Length()
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
	e.Input.Replace(strings.Repeat("x", 100))
	e.Pos = 50
	ev.refresh(e)

	_, before := ev.rowCol(e)
	sz.cols = 40
	ev.resize(e)
	_, after := ev.rowCol(e)

	if e.Pos != 50 {
		t.Errorf("the cursor moved to %d, want 50", e.Pos)
	}
	// A narrower window puts the same character further down the screen.
	if after.Row <= before.Row {
		t.Errorf("the cursor was on row %d at 80 columns and row %d at 40, want further down",
			before.Row, after.Row)
	}
}

// TestResizeWithContentBelowTheLine checks a resize while a completion menu or
// a search is showing, which is when the rows below the line have to be
// measured as well.
func TestResizeWithContentBelowTheLine(t *testing.T) {
	t.Parallel()
	ev, e, sink, sz := newResizeEnv(t)
	e.Input.Replace("select")
	e.Pos = 6
	e.Extra.Replace("[ic-info]1. one[/]\n[ic-info]2. two[/]\n")
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
