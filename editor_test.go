package rline

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const (
	// edCorpusPath holds the state the C editing operations left behind.
	edCorpusPath = "testdata/editline.txt"

	// edProbePath is where tools/build-probe-editline.sh puts the probe.
	edProbePath = ".build/probe-editline"
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
	fn func(*editor) bool
}{
	{"left", (*editor).cursorLeft},
	{"right", (*editor).cursorRight},
	{"line-end", (*editor).cursorLineEnd},
	{"line-start", (*editor).cursorLineStart},
	{"next-word", (*editor).cursorNextWord},
	{"prev-word", (*editor).cursorPrevWord},
	{"next-ws-word", (*editor).cursorNextWSWord},
	{"prev-ws-word", (*editor).cursorPrevWSWord},
	{"to-start", (*editor).cursorToStart},
	{"to-end", (*editor).cursorToEnd},
	{"match-brace", (*editor).cursorMatchBrace},
	{"backspace", (*editor).backspace},
	{"delete-char", (*editor).deleteChar},
	{"delete-all", (*editor).deleteAll},
	{"del-to-line-end", (*editor).deleteToLineEnd},
	{"del-to-line-start", (*editor).deleteToLineStart},
	{"delete-line", (*editor).deleteLine},
	{"del-to-word-start", (*editor).deleteToWordStart},
	{"del-to-word-end", (*editor).deleteToWordEnd},
	{"del-to-ws-start", (*editor).deleteToWSWordStart},
	{"del-to-ws-end", (*editor).deleteToWSWordEnd},
	{"delete-word", (*editor).deleteWord},
	{"swap-char", (*editor).swapChar},
	{"multiline-eol", (*editor).multilineEOL},
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
	opts := editOptions{
		MatchBraces:  "()[]{}",
		AutoBraces:   `()[]{}""''`,
		MultilineEOL: '\\',
	}
	start := func(text string, pos int) *editor {
		e := &editor{opts: opts, termW: 80, curRows: 1}
		e.input.replace(text)
		e.pos = pos
		return e
	}
	report := func(op, text string, pos int, e *editor) string {
		return fmt.Sprintf("op %s %s %d -> %s %d %d %d %d",
			op, escapeEd(text), pos, escapeEd(e.input.string()), e.pos,
			boolInt(e.modified), e.undo.count(), e.redo.count())
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
				e.insertChar(c)
				out = append(out, report(fmt.Sprintf("insert-%02x", c), text, pos, e))
			}
		}
	}
	for _, text := range edTexts {
		e := start(text, 0)
		e.insertChar('Z')
		out = append(out, report("undo-setup", text, 0, e))
		e.undoRestore(true)
		out = append(out, report("undo", text, 0, e))
		e.redoRestore()
		out = append(out, report("redo", text, 0, e))
		e.undoRestore(true)
		e.undoRestore(true)
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
