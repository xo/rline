package rline

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const (
	// hlCorpusPath holds what the C highlight.c produced.
	hlCorpusPath = "testdata/highlight.txt"

	// hlProbePath is where tools/build-probe-highlight.sh puts the probe.
	hlProbePath = ".build/probe-highlight"
)

// hlLines are the lines the probe matches braces in.
var hlLines = []string{
	"", "a", "()", "(", ")", "()()", "(())", "(()", "())", "([)]", "([])",
	"{[()]}", "{[(])}", "((()))", ")(", "a(b)c", "(a[b]c)", "]", "[",
	"(((((", ")))))", "([{}])", "([{}]", "{}{}{}", "(]", "[)",
	"a(b[c]d)e", "((a)", "(a))", "no braces here",
}

// hlBraceSets are the brace pairs the probe uses.
var hlBraceSets = []string{"()[]{}", "()", "<>", "", "()[]"}

// TestHighlightMatchesC replays the script that tools/probe-highlight.c runs.
func TestHighlightMatchesC(t *testing.T) {
	if *update {
		hlRegenerate(t)
	}
	b, err := os.ReadFile(hlCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-highlight.sh, then go test -update)", err)
	}
	want := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	got := hlReplay(t)
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
			t.Errorf("%s:%d\n  got:  %s\n  want: %s", hlCorpusPath, i+1, g, w)
		}
	}
	if bad > 20 {
		t.Errorf("%d differences in total, 20 shown", bad)
	}
	if bad == 0 {
		t.Logf("checked %d lines", len(want))
	}
}

// hlReplay runs the same script as the C probe.
func hlReplay(t *testing.T) []string {
	t.Helper()
	matchAttr := attrFromSGR("1")
	errorAttr := attrFromSGR("31")
	out := make([]string, 0, 2400)

	for b, braces := range hlBraceSets {
		for _, line := range hlLines {
			for cp := -1; cp <= len(line)+1; cp++ {
				m, balanced := findMatchingBrace(line, cp, braces)
				out = append(out, fmt.Sprintf("match %d %s %d %d %d",
					b, escapeHL(line), cp, m, boolInt(balanced)))
			}
		}
	}
	for b, braces := range hlBraceSets {
		for _, line := range hlLines {
			for cp := -1; cp <= len(line)+1; cp++ {
				var ab attrBuf
				if len(line) > 0 {
					ab.setAt(0, len(line), attr{})
				}
				highlightMatchBraces(line, &ab, cp, braces, matchAttr, errorAttr)
				out = append(out, fmt.Sprintf("braces %d %s %d %s",
					b, escapeHL(line), cp, hlAttrs(&ab)))
			}
		}
	}

	restore := saveEnv(t)
	defer restore()
	setTermEnv("", "xterm-256color", "")
	var sink bytes.Buffer
	tm := newTerm(&sink, termOptions{NoColor: true, Sizer: fixedSize{cols: 80, rows: 24}})
	bb := newBBCode(tm)

	inputs := []string{"hello", "ééé", "a日b", "abcdef"}
	poss := []int{-6, -3, -1, 0, 1, 3, 5, 6, 100}
	counts := []int{-3, -1, 0, 1, 2, 100}
	for i, in := range inputs {
		for _, pos := range poss {
			for _, count := range counts {
				var ab attrBuf
				ab.setAt(0, len(in), attr{})
				env := &Highlight{input: in, attrs: &ab, bb: bb}
				env.Style(pos, count, "bold")
				out = append(out, fmt.Sprintf("hl %d %d %d %s %d %d",
					i, pos, count, hlAttrs(&ab), env.cachedUPos, env.cachedCPos))
			}
		}
	}
	fmts := [][2]string{
		{"hello", "[b]he[/b]llo"},
		{"hello", "[red]hello[/]"},
		{"hello", "[b]toolong[/b]"},
		{"hello", "[b]hi[/b]"},
		// {"hello", ""} is not here. See TestHighlightFormattedEmpty.
		{"", "[b]x[/b]"},
	}
	for i, f := range fmts {
		var ab attrBuf
		if len(f[0]) > 0 {
			ab.setAt(0, len(f[0]), attr{})
		}
		env := &Highlight{input: f[0], attrs: &ab, bb: bb}
		env.Formatted(f[0], f[1])
		out = append(out, fmt.Sprintf("hlfmt %d %s", i, hlAttrs(&ab)))
	}
	return out
}

// hlAttrs renders an attribute buffer the way the probe prints one.
func hlAttrs(ab *attrBuf) string {
	n := ab.length()
	parts := make([]string, 0, n+1)
	parts = append(parts, fmt.Sprint(n))
	for _, a := range ab.slice(n) {
		parts = append(parts, bbAttr(a))
	}
	return strings.Join(parts, " ")
}

// escapeHL renders a string the way the highlight probe prints one, which
// spells out a space so that a field never looks empty.
func escapeHL(s string) string {
	if s == "" {
		return "-"
	}
	var sb strings.Builder
	for i := range len(s) {
		c := s[i]
		if c < 0x20 || c >= 0x7f || c == ' ' {
			fmt.Fprintf(&sb, `\x%02x`, c)
			continue
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

// hlRegenerate runs the C probe and writes the corpus.
func hlRegenerate(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(hlProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-highlight.sh", hlProbePath)
	}
	out, err := exec.Command(hlProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(hlCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", hlCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", hlCorpusPath, len(out))
}

// TestHighlightFormattedEmpty covers the case the corpus cannot.
//
// An empty format leaves nothing in the attribute buffer, and the C then reads
// the slot just past the end, because attrbuf_attr_at tests pos > count where
// it means pos >= count. That slot was never written, so the C answer is a
// heap pointer that changes between runs. The port answers with the empty
// attribute, which leaves the line as it was.
func TestHighlightFormattedEmpty(t *testing.T) {
	t.Parallel()
	var sink bytes.Buffer
	tm := newTerm(&sink, termOptions{NoColor: true})
	bb := newBBCode(tm)
	var ab attrBuf
	ab.setAt(0, 5, attrFromSGR("31"))
	env := &Highlight{input: "hello", attrs: &ab, bb: bb}
	env.Formatted("hello", "")
	want := attrFromSGR("31")
	for i, got := range ab.slice(5) {
		if got != want {
			t.Errorf("byte %d became %v, want it left alone as %v", i, got, want)
		}
	}
}
