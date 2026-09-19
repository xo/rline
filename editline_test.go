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

// refreshCorpusPath returns where the recordings for the running system live.
//
// This corpus is kept per system, unlike the others, because the C code it
// records is not the same on every system: the mark at the end of a wrapped
// row is chosen at compile time. The rule is that a corpus goes under a system
// directory when the C it records has a platform branch in it, and stays a
// single file when it does not.
func refreshCorpusPath() string {
	return filepath.Join("testdata", runtime.GOOS, "refresh.txt")
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
		t.Fatalf("no recordings for %s: %v\n"+
			"Record them on a %s machine with:\n"+
			"  ./tools/build-probe-refresh.sh && go test . -run TestRefreshMatchesC -update",
			runtime.GOOS, err, runtime.GOOS)
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
								e.hint.replace(hint)
								e.extra.replace(extra)
								e.hintHelp.clear()
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
func refreshRegenerate(t *testing.T) {
	t.Helper()
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
