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
)

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
		opts: editOptions{
			MatchBraces:  "()[]{}",
			AutoBraces:   "()[]{}",
			MultilineEOL: '\\',
		},
		noHighlight: true,
	}
	e := &editor{}

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
								e.input.replace(text)
								e.hint.Reset()
								e.hint.WriteString(hint)
								e.extra.replace(extra)
								e.hintHelp.Reset()
								e.pos = pos
								e.termW = 40
								e.curRows = 1
								e.curRow = 0
								e.promptText = prompt
								ev.noMultilineIndent = noIndent
								ev.noBraceMatch = noBrace
								tm.flush()
								out = append(out, emit())
								ev.refresh(e)
								tm.flush()
								out = append(out, fmt.Sprintf("refresh %d %s %d %d %d %s",
									caseno, emit(), e.curRows, e.curRow, e.pos,
									escapeBB([]byte(e.input.string()))))
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
