package editor

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// --------------------------------------------------------------------------
// editor_test.go

// update regenerates the corpus by running the C probe. It needs a compiler
// and the build script, so it runs only when asked.
var update = flag.Bool("update", false, "regenerate testdata/editline.txt from the C probe")

// boolInt renders a bool the way the C probe prints one.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// lineAt returns the line at index i, or a marker when the text ended.
func lineAt(lines []string, i int) string {
	if i >= len(lines) {
		return "<end of text>"
	}
	return lines[i]
}

const (
	// edCorpusPath holds the state the C editing operations left behind.
	edCorpusPath = "testdata/editline.txt"

	// edProbePath is where tools/build-probe-editline.sh puts the probe.
	edProbePath = "../../.build/probe-editline"
)

// edTexts are the lines the probe runs every operation against.
var edTexts = []string{
	"", "a", "hello", "hello world", "  spaced  out  ",
	"one two three", "line1\nline2", "a\nb\nc",
	"ééé", "a日b",
	"(nested [brackets])", "trailing   ", "   leading",
}

// edOps are the operations the probe runs, in the order it runs them.
var edOps = []struct {
	name string
	// fn reports whether it changed anything, which the key dispatch uses to
	// decide whether to redraw. The corpus does not record that, because the
	// C has no such value: it returns early instead.
	fn func(*Editor) bool
}{
	{"left", (*Editor).CursorLeft},
	{"right", (*Editor).CursorRight},
	{"line-end", (*Editor).CursorLineEnd},
	{"line-start", (*Editor).CursorLineStart},
	{"next-word", (*Editor).CursorNextWord},
	{"prev-word", (*Editor).CursorPrevWord},
	{"next-ws-word", (*Editor).CursorNextWSWord},
	{"prev-ws-word", (*Editor).CursorPrevWSWord},
	{"to-start", (*Editor).CursorToStart},
	{"to-end", (*Editor).CursorToEnd},
	{"match-brace", (*Editor).CursorMatchPair},
	{"backspace", (*Editor).Backspace},
	{"delete-char", (*Editor).DeleteChar},
	{"delete-all", (*Editor).DeleteAll},
	{"del-to-line-end", (*Editor).DeleteToLineEnd},
	{"del-to-line-start", (*Editor).DeleteToLineStart},
	{"delete-line", (*Editor).DeleteLine},
	{"del-to-word-start", (*Editor).DeleteToWordStart},
	{"del-to-word-end", (*Editor).DeleteToWordEnd},
	{"del-to-ws-start", (*Editor).DeleteToWSWordStart},
	{"del-to-ws-end", (*Editor).DeleteToWSWordEnd},
	{"delete-word", (*Editor).DeleteWord},
	{"swap-char", (*Editor).SwapChar},
	{"multiline-eol", (*Editor).MultilineEOL},
}

// edInserts are the bytes the probe inserts.
var edInserts = []byte{'x', '(', ')', '[', ']', '"', '\'', ' ', '\n'}

// TestEditorMatchesC replays the script that tools/probe-editline.c runs and
// checks that each operation leaves the same text, cursor and undo stacks.
func TestEditorMatchesC(t *testing.T) {
	if *update {
		edRegenerate(t)
	}
	b, err := os.ReadFile(edCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-editline.sh, then go test -update)", err)
	}
	want := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	got := edReplay()
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
			t.Errorf("%s:%d\n  got:  %s\n  want: %s", edCorpusPath, i+1, g, w)
		}
	}
	if bad > 20 {
		t.Errorf("%d differences in total, 20 shown", bad)
	}
	if bad == 0 {
		t.Logf("checked %d operations", len(want))
	}
}

// edReplay runs the same script as the C probe.
func edReplay() []string {
	out := make([]string, 0, 4200)
	opts := EditOptions{
		MatchPairs:   "()[]{}",
		AutoPairs:    `()[]{}""''`,
		MultilineEOL: '\\',
		AutoPair:     true,
	}
	start := func(text string, pos int) *Editor {
		e := &Editor{Opts: opts, TermW: 80, CurRows: 1}
		e.Input.Replace(text)
		e.Pos = pos
		return e
	}
	report := func(op, text string, pos int, e *Editor) string {
		return fmt.Sprintf("op %s %s %d -> %s %d %d %d %d",
			op, escapeEd(text), pos, escapeEd(e.Input.String()), e.Pos,
			boolInt(e.Modified), e.Undo.Count(), e.Redo.Count())
	}

	for _, text := range edTexts {
		for pos := 0; pos <= len(text); pos++ {
			for _, op := range edOps {
				e := start(text, pos)
				_ = op.fn(e)
				out = append(out, report(op.name, text, pos, e))
			}
		}
	}
	for _, text := range edTexts {
		for pos := 0; pos <= len(text); pos++ {
			for _, c := range edInserts {
				e := start(text, pos)
				e.InsertChar(c)
				out = append(out, report(fmt.Sprintf("insert-%02x", c), text, pos, e))
			}
		}
	}
	for _, text := range edTexts {
		e := start(text, 0)
		e.InsertChar('Z')
		out = append(out, report("undo-setup", text, 0, e))
		e.UndoRestore(true)
		out = append(out, report("undo", text, 0, e))
		e.RedoRestore()
		out = append(out, report("redo", text, 0, e))
		e.UndoRestore(true)
		e.UndoRestore(true)
		out = append(out, report("undo-twice", text, 0, e))
	}
	return out
}

// escapeEd renders a string the way the editline probe prints one.
func escapeEd(s string) string {
	if s == "" {
		return "-"
	}
	var sb strings.Builder
	for i := range len(s) {
		c := s[i]
		if c < 0x20 || c >= 0x7f || c == ' ' || c == '\\' {
			fmt.Fprintf(&sb, `\x%02x`, c)
			continue
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

// edRegenerate runs the C probe and writes the corpus.
func edRegenerate(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(edProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-editline.sh", edProbePath)
	}
	out, err := exec.Command(edProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(edCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", edCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", edCorpusPath, len(out))
}

// --------------------------------------------------------------------------
// undo_test.go

// TestEditStackIsAStack checks that the most recent saved line comes back
// first, which is what stepping back through edits means.
func TestEditStackIsAStack(t *testing.T) {
	t.Parallel()
	var s EditStack
	if got := s.Count(); got != 0 {
		t.Errorf("a new stack holds %d, want 0", got)
	}
	s.Capture("a", 1)
	s.Capture("ab", 2)
	if got := s.Count(); got != 2 {
		t.Errorf("the stack holds %d, want 2", got)
	}
	for _, want := range []EditState{{"ab", 2}, {"a", 1}} {
		input, pos, ok := s.Restore()
		if !ok {
			t.Fatalf("nothing came back where %q was saved", want.Input)
		}
		if input != want.Input || pos != want.Pos {
			t.Errorf("restored %q at %d, want %q at %d", input, pos, want.Input, want.Pos)
		}
	}
	if _, _, ok := s.Restore(); ok {
		t.Error("an empty stack gave something back")
	}
	if got := s.Count(); got != 0 {
		t.Errorf("the stack holds %d after being emptied, want 0", got)
	}
}

// TestEditStackReset checks that throwing the saved lines away leaves the
// stack usable. The edit loop does this when it starts a new line.
func TestEditStackReset(t *testing.T) {
	t.Parallel()
	var s EditStack
	for i := range 5 {
		s.Capture("line", i)
	}
	s.Reset()
	if got := s.Count(); got != 0 {
		t.Errorf("after reset the stack holds %d, want 0", got)
	}
	if _, _, ok := s.Restore(); ok {
		t.Error("after reset something came back")
	}
	s.Capture("again", 7)
	input, pos, ok := s.Restore()
	if !ok || input != "again" || pos != 7 {
		t.Errorf("after reset the stack gave %q at %d, want %q at %d", input, pos, "again", 7)
	}
}
