// Tests for markup and highlighting, which are one subject and one file. See
// the head of bbcode.go. The attribute, the buffer of them and the color one
// carries are tested in ansi.

package rline

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/xo/rline/ansi"
	"github.com/xo/rline/internal/text"
)

// --------------------------------------------------------------------------
// appendmarked_test.go

const (
	// attrBufCorpusPath holds the recorded calls where the attribute buffer
	// meets rline's own text buffer. The rest are checked in ansi.
	attrBufCorpusPath = "testdata/attrbuf.txt"

	// attrProbePath is where tools/build-probe-attr.sh puts the probe.
	attrProbePath = ".build/probe-attr"
)

// attrBufKinds are the recorded calls this package answers. The buffer itself
// is ansi.AttrBuf and the ansi package checks it; what is left here is where
// it meets rline's own text buffer, which is "abuf app" and the two append
// kinds. The names carry two fields because "abuf" spans both packages.
var attrBufKinds = []string{"abuf app", "append", "appendstr"}

// attrBufKind names a recorded call the way attrBufKinds does.
func attrBufKind(f []string) string {
	if f[0] == "abuf" && len(f) > 1 {
		return f[0] + " " + f[1]
	}
	return f[0]
}

// TestAttrBufMatchesC replays every recorded call to the attribute buffer in
// attr.c and checks that the Go port answers the same. The attribute itself
// moved to the ansi package and is checked there.

func TestAttrBufMatchesC(t *testing.T) {
	if *update {
		attrRegenerate(t)
	}
	b, err := os.ReadFile(attrBufCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-attr.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	// A floor would let the corpus shrink. This test is driven by the corpus
	// rather than replaying a script against it, so a line that goes missing
	// is one case fewer checked and nothing else. The exact count is the
	// smallest thing that notices, and changing it is a deliberate edit
	// beside the corpus it describes.
	if len(lines) != 9 {
		t.Fatalf("testdata/attrbuf.txt holds %d lines, want %d: a corpus that changed size was "+
			"either regenerated on purpose, in which case set this number, or "+
			"lost lines, in which case it now checks less than it says",
			len(lines), 9)
	}
	dumps := attrReplay()
	counts := make(map[string]int, 16)
	bad := 0
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) == 0 {
			t.Fatalf("%s:%d: empty line", attrBufCorpusPath, i+1)
		}
		counts[attrBufKind(f)]++
		got, want := attrCheck(t, f, dumps)
		if got == want {
			continue
		}
		bad++
		if bad <= 20 {
			t.Errorf("%s:%d: %s\n  got:  %s\n  want: %s", attrBufCorpusPath, i+1, strings.Join(f[:min(3, len(f))], " "), got, want)
		}
	}
	if bad > 20 {
		t.Errorf("%d differences in total, 20 shown", bad)
	}
	for _, kind := range attrBufKinds {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
		}
	}
	for kind := range counts {
		if !slices.Contains(attrBufKinds, kind) {
			t.Errorf("the corpus holds %d %s cases, which belong to ansi", counts[kind], kind)
		}
	}
	t.Logf("checked %d calls: %v", len(lines), counts)
}

// attrCheck computes the Go answer for one recorded call, and returns it with
// the recorded one.
func attrCheck(t *testing.T, f []string, dumps map[string][]string) (string, string) {
	t.Helper()
	switch f[0] {
	case "abuf":
		key := f[1] + " " + f[2]
		got, ok := dumps[key]
		if !ok {
			t.Fatalf("the Go replay has no step %q", key)
		}
		return strings.Join(got, " "), strings.Join(f[3:], " ")
	case "append":
		key := "append " + f[1]
		got, ok := dumps[key]
		if !ok {
			t.Fatalf("the Go replay has no step %q", key)
		}
		return got[0], f[2]
	case "appendstr":
		got := dumps["appendstr"]
		return hex.EncodeToString([]byte(got[0])), f[1]
	}
	t.Fatalf("unknown kind of call %q", f[0])
	return "", ""
}

// attrReplay runs the appending part of what tools/probe-attr.c runs, and
// records both what it returned and what the buffer held afterwards. The rest
// of the probe's buffer steps are replayed in the ansi package.
func attrReplay() map[string][]string {
	out := make(map[string][]string, 8)
	ab := &ansi.AttrBuf{}
	sb := &text.Buffer{}
	for i, c := range []struct {
		s string
		a string
		n bool
	}{
		{"abc", "31", true},
		{"de", "1", true},
		{"", "32", true},
		{"fg", "32", false},
	} {
		target := ab
		if !c.n {
			target = nil
		}
		out["append "+strconv.Itoa(i)] = []string{strconv.Itoa(appendMarked(target, sb, c.s, ansi.ParseSGR(c.a)))}
		n := ab.Length()
		row := make([]string, 0, n+1)
		row = append(row, strconv.Itoa(n))
		for _, a := range ab.Extend(n) {
			row = append(row, attrString(a))
		}
		out["app "+strconv.Itoa(i)] = row
	}
	out["appendstr"] = []string{sb.String()}
	return out
}

// attrRegenerate runs the C probe and writes the corpus.
func attrRegenerate(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(attrProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-attr.sh", attrProbePath)
	}
	out, err := exec.Command(attrProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	var w strings.Builder
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if slices.Contains(attrBufKinds, attrBufKind(strings.Fields(line))) {
			w.WriteString(line)
			w.WriteByte('\n')
		}
	}
	if err := os.WriteFile(attrBufCorpusPath, []byte(w.String()), 0o644); err != nil {
		t.Fatalf("writing %s: %v", attrBufCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", attrBufCorpusPath, w.Len())
}

// attrString renders an attribute the way the C probe prints one.
func attrString(a ansi.Attr) string {
	return fmt.Sprintf("%08x %08x %d %d %d %d",
		uint32(a.Fg), uint32(a.Bg), a.Bold, a.Italic, a.Reverse, a.Underline)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --------------------------------------------------------------------------
// bbcode_test.go

const (
	// bbCorpusPath holds what the C bbcode.c produced.
	bbCorpusPath = "testdata/bbcode.txt"

	// bbProbePath is where tools/build-probe-bbcode.sh puts the probe.
	bbProbePath = ".build/probe-bbcode"
)

// bbStyleNames are the style names the probe resolves on their own.
var bbStyleNames = []string{
	"b", "i", "u", "r", "em", "url", "red", "ansi-red", "#123456",
	"mystyle", "other", "nosuchthing", "", "bold", "color=red",
}

// bbCorpus is the markup the probe parses.
var bbCorpus = []string{
	"", "plain", "[b]bold[/b]", "[b]bold", "[/b]", "[]", "[ ]",
	"[b][i]both[/i][/b]", "[b][i]both[/b][/i]", "[b]a[/i]b[/b]",
	"[red]red[/red]", "[red]red[/]", "[#ff0000]hex[/]", "[#f00]short[/]",
	"[#ZZZZZZ]bad[/]", "[color=red]x[/]", "[color=#00ff00]x[/]",
	"[color=none]x[/]", "[color=nosuchcolor]x[/]",
	"[bgcolor=blue]x[/]", "[on blue]x[/]", "[on red]x[/]",
	"[ansi-red]x[/]", "[ansi-default]x[/]", "[ansi-color=33]x[/]",
	"[ansi-color=256]x[/]", "[ansi-color=999]x[/]", "[ansi-bgcolor=4]x[/]",
	"[ansi-sgr=1;31]x[/]", "[ansi-sgr=0]x[/]",
	"[bold=on]x[/]", "[bold=off]x[/]", "[bold=true]x[/]", "[bold=false]x[/]",
	"[bold=1]x[/]", "[bold=0]x[/]", "[bold=nonsense]x[/]", "[bold=]x[/]",
	"[italic=off]x[/]", "[underline=off]x[/]", "[reverse=off]x[/]",
	"[u]u[/u]", "[i]i[/i]", "[r]r[/r]", "[em]em[/em]", "[url]url[/url]",
	"[!pre]raw [b]not a tag[/b][/pre]", "[!pre]unterminated [b]x",
	"[!b]raw[/b]", "escaped \\[b] text", "backslash \\\\ here", "trailing \\\\",
	"esc \x1b[31m inside [b]bold[/b]",
	"[width=10]ab[/]", "[width=10;right]ab[/]", "[width=10;center]ab[/]",
	"[width=10;left;.]ab[/]", "[width=3]abcdefgh[/]",
	"[width=8;left;\\s;on]abcdefghij[/]", "[width=8;right;\\s;on]abcdefghij[/]",
	"[max-width=4]abcdefg[/]", "[max-width=4;right]abcdefg[/]",
	"[width=0]ab[/]", "[width=-3]ab[/]", "[width=notanumber]ab[/]",
	"[b][red]both[/red][/b]", "[b][red]both[/b][/red]",
	"[mystyle]custom[/mystyle]", "[other]second[/other]",
	"[B]upper[/B]", "[RED]upper[/RED]", "[color=RED]upper[/]",
	"[b   ]spaces[/b]", "[ b ]spaces[/ b ]",
	"[b]\xe6\x97\xa5\xe6\x9c\xac[/b]", "[width=6]\xe6\x97\xa5\xe6\x9c\xac[/]",
	"[color=\"red\"]quoted[/]", "[color=\"\"]empty[/]",
	"a[b]b[/b]c[i]d[/i]e",
	// The readers behind these four take decisions nothing above reaches: a
	// leading space, a sign, digits with something after them, an empty
	// value, and for the colors each hex length. Kept in step with corpus[]
	// in tools/probe-bbcode.c, in the same order.
	"[ansi-color= 5]x[/]",
	"[ansi-color=+5]x[/]",
	"[ansi-color=-1]x[/]",
	"[ansi-color=5x]x[/]",
	"[ansi-color=]x[/]",
	"[ansi-color=x5]x[/]",
	"[ansi-color=0]x[/]",
	"[ansi-color=15]x[/]",
	"[ansi-color=255]x[/]",
	"[ansi-bgcolor=255]x[/]",
	"[ansi-bgcolor=256]x[/]",
	"[ansi-bgcolor= 2]x[/]",
	"[color=#abc]x[/]",
	"[color=#ABCDEF]x[/]",
	"[color=#ab]x[/]",
	"[color=#abcdefff]x[/]",
	"[color=#]x[/]",
	"[color=#xyzxyz]x[/]",
	"[bgcolor=#123456]x[/]",
	"[bgcolor=#abc]x[/]",
	"[color=0]x[/]",
	"[color=255]x[/]",
	"[color=256]x[/]",
	"[color=-1]x[/]",
	"[color= 9]x[/]",
	"[color=9z]x[/]",
	"[bgcolor=3]x[/]",
	"[ansi-sgr=31]x[/]",
	"[ansi-sgr=38;5;200]x[/]",
	"[ansi-sgr=38;2;1;2;3]x[/]",
	"[ansi-sgr=48;5;9]x[/]",
	"[ansi-sgr=]x[/]",
	"[ansi-sgr=999]x[/]",
	"[ansi-sgr=1;]x[/]",
	"[ansi-sgr=;1]x[/]",
	"[ansi-sgr=38;5;]x[/]",
}

// TestBBCodeMatchesC replays the script that tools/probe-bbcode.c runs and
// checks that the Go port produces the same text, attributes and output.
func TestBBCodeMatchesC(t *testing.T) {
	if *update {
		bbRegenerate(t)
	}
	b, err := os.ReadFile(bbCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-bbcode.sh, then go test -update)", err)
	}
	want := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	got := bbReplay(t)
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
			t.Errorf("%s:%d\n  got:  %s\n  want: %s", bbCorpusPath, i+1, g, w)
		}
	}
	if bad > 20 {
		t.Errorf("%d differences in total, 20 shown", bad)
	}
	if bad == 0 {
		t.Logf("checked %d lines over %d pieces of markup", len(want), len(bbCorpus))
	}
}

// bbReplay runs the same script as the C probe.
func bbReplay(t *testing.T) []string {
	t.Helper()
	restore := saveEnv(t)
	defer restore()
	setTermEnv("", "xterm-256color", "")

	out := make([]string, 0, 1+len(bbStyleNames)+3*len(bbCorpus))
	var sink bytes.Buffer
	tm := newTerm(&sink, termOptions{Sizer: fixedSize{cols: 80, rows: 24}})
	tm.setBufferMode(unbuffered)
	bb := newBBCode(tm)
	out = append(out, "out discard "+escapeBB(sink.Bytes()))
	sink.Reset()

	bb.styleDef("mystyle", "bold color=green")
	bb.styleDef("other", "underline bgcolor=navy")

	for _, name := range bbStyleNames {
		out = append(out, "style "+escapeBB([]byte(name))+" "+bbAttr(bb.style(name)))
	}

	for i, s := range bbCorpus {
		var o text.Buffer
		var ab ansi.AttrBuf
		bb.appendTo(s, &o, &ab)
		n := o.Length()
		row := make([]string, 0, n+4)
		row = append(row, fmt.Sprintf("append %d %s %d", i, escapeBB(o.Bytes()), n))
		for _, a := range ab.Extend(n) {
			row = append(row, bbAttr(a))
		}
		out = append(out, strings.Join(row, " "))
		out = append(out, fmt.Sprintf("width %d %d", i, bb.columnWidth(s)))
	}

	for i, s := range bbCorpus {
		bb.print(s)
		tm.flush()
		out = append(out, fmt.Sprintf("print %d out  %s", i, escapeBB(sink.Bytes())))
		sink.Reset()
	}
	return out
}

// bbAttr renders an attribute the way the bbcode probe prints one.
func bbAttr(a ansi.Attr) string {
	return fmt.Sprintf("%08x/%08x/%d/%d/%d/%d",
		uint32(a.Fg), uint32(a.Bg), a.Bold, a.Italic, a.Reverse, a.Underline)
}

// escapeBB renders bytes the way the bbcode probe prints them, which spells
// out a space so that a field never looks empty.
func escapeBB(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c == 0x1b:
			sb.WriteString(`\e`)
		case c == '\\':
			sb.WriteString(`\\`)
		case c == '\r':
			sb.WriteString(`\r`)
		case c == '\n':
			sb.WriteString(`\n`)
		case c == '\t':
			sb.WriteString(`\t`)
		case c == ' ':
			sb.WriteString(`\s`)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&sb, `\x%02x`, c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// bbRegenerate runs the C probe and writes the corpus.
func bbRegenerate(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(bbProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-bbcode.sh", bbProbePath)
	}
	out, err := exec.Command(bbProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(bbCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", bbCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", bbCorpusPath, len(out))
}

// --------------------------------------------------------------------------
// highlight_test.go

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
	matchAttr := ansi.ParseSGR("1")
	errorAttr := ansi.ParseSGR("31")
	out := make([]string, 0, 2400)

	for b, braces := range hlBraceSets {
		for _, line := range hlLines {
			for cp := -1; cp <= len(line)+1; cp++ {
				m, balanced := text.FindMatchingBrace(line, cp, braces)
				out = append(out, fmt.Sprintf("match %d %s %d %d %d",
					b, escapeHL(line), cp, m, boolInt(balanced)))
			}
		}
	}
	for b, braces := range hlBraceSets {
		for _, line := range hlLines {
			for cp := -1; cp <= len(line)+1; cp++ {
				var ab ansi.AttrBuf
				if len(line) > 0 {
					ab.SetAt(0, len(line), ansi.Attr{})
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
				var ab ansi.AttrBuf
				ab.SetAt(0, len(in), ansi.Attr{})
				env := &LineStyle{input: in, attrs: &ab, bb: bb}
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
		var ab ansi.AttrBuf
		if len(f[0]) > 0 {
			ab.SetAt(0, len(f[0]), ansi.Attr{})
		}
		env := &LineStyle{input: f[0], attrs: &ab, bb: bb}
		env.StyleMarkup(f[0], f[1])
		out = append(out, fmt.Sprintf("hlfmt %d %s", i, hlAttrs(&ab)))
	}
	return out
}

// hlAttrs renders an attribute buffer the way the probe prints one.
func hlAttrs(ab *ansi.AttrBuf) string {
	n := ab.Length()
	parts := make([]string, 0, n+1)
	parts = append(parts, fmt.Sprint(n))
	for _, a := range ab.Extend(n) {
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

// TestPropertyBeatsAStyleOfTheSameName covers a case the corpus cannot.
//
// updateProperty answers with the name it consumed, and an empty string when
// the name was not a property at all. updateWithStyles reads that answer to
// decide whether to go on and search the styles: a property is finished, and
// anything else may still be a style or a color name.
//
// The C probe never defines a style whose name collides with a property, so
// the whole recorded corpus passes whether that answer is returned or not.
// Every property sets its field before the answer is read, which is why the
// difference only shows when a style of the same name exists to be applied on
// top. This pins the answer itself.
func TestPropertyBeatsAStyleOfTheSameName(t *testing.T) {
	t.Parallel()
	bb := newBBCode(nil)
	bb.styleDef("bold", "color=red")
	got := bb.style("bold")
	if got.Bold != ansi.FlagOn {
		t.Errorf("the bold property was not applied: %+v", got)
	}
	if got.Fg != ansi.None {
		t.Errorf("the style named bold was applied as well as the property, "+
			"giving color %#08x; a property name is not a style name", uint32(got.Fg))
	}
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
	var ab ansi.AttrBuf
	ab.SetAt(0, 5, ansi.ParseSGR("31"))
	env := &LineStyle{input: "hello", attrs: &ab, bb: bb}
	env.StyleMarkup("hello", "")
	want := ansi.ParseSGR("31")
	for i, got := range ab.Extend(5) {
		if got != want {
			t.Errorf("byte %d became %v, want it left alone as %v", i, got, want)
		}
	}
}

// ----------------------------------------------------------------------------
// The highlighting surface a program actually uses

// newLineStyle returns a LineStyle over s, and the attribute buffer behind it,
// which is how a test sees what a highlighter did.
func newLineStyle(t *testing.T, s string) (*LineStyle, *ansi.AttrBuf) {
	t.Helper()
	var sink bytes.Buffer
	tm := newTerm(&sink, termOptions{NoColor: true})
	bb := newBBCode(tm)
	bb.styleDef("keyword", "bold")
	var ab ansi.AttrBuf
	ab.SetAt(0, len(s), ansi.Attr{})
	return &LineStyle{input: s, attrs: &ab, bb: bb}, &ab
}

// TestStyleCountsBytesAndStyleRunesCountsCharacters checks the pair that
// replaced one method with a negative count meaning characters.
//
// The negative count was the reason for the split, so what matters is that
// the two disagree where a character is more than one byte, and agree where
// every character is one.
func TestStyleCountsBytesAndStyleRunesCountsCharacters(t *testing.T) {
	t.Parallel()
	// Three characters of three bytes each.
	const line = "日本語"

	t.Run("Style counts bytes", func(t *testing.T) {
		t.Parallel()
		l, ab := newLineStyle(t, line)
		l.Style(0, 3, "keyword")
		marked := markedBytes(ab, len(line))
		if marked != 3 {
			t.Errorf("Style(0, 3) marked %d bytes, want 3 — one character", marked)
		}
	})

	t.Run("StyleRunes counts characters", func(t *testing.T) {
		t.Parallel()
		l, ab := newLineStyle(t, line)
		l.StyleRunes(0, 3, "keyword")
		marked := markedBytes(ab, len(line))
		if marked != 9 {
			t.Errorf("StyleRunes(0, 3) marked %d bytes, want 9 — three characters", marked)
		}
	})

	t.Run("they agree over ASCII", func(t *testing.T) {
		t.Parallel()
		a, aab := newLineStyle(t, "select")
		a.Style(0, 6, "keyword")
		b, bab := newLineStyle(t, "select")
		b.StyleRunes(0, 6, "keyword")
		if x, y := markedBytes(aab, 6), markedBytes(bab, 6); x != y {
			t.Errorf("Style marked %d bytes and StyleRunes marked %d, over ASCII", x, y)
		}
	})

	t.Run("a style that is not defined marks nothing", func(t *testing.T) {
		t.Parallel()
		l, ab := newLineStyle(t, line)
		l.Style(0, 9, "")
		if marked := markedBytes(ab, len(line)); marked != 0 {
			t.Errorf("an empty style marked %d bytes", marked)
		}
	})
}

// markedBytes counts how many of the first n bytes carry any attribute.
func markedBytes(ab *ansi.AttrBuf, n int) int {
	count := 0
	for _, a := range ab.Extend(n) {
		if a != (ansi.Attr{}) {
			count++
		}
	}
	return count
}

// TestLineStyleTextIsTheLine checks the method that replaced handing the
// highlighter its line twice.
func TestLineStyleTextIsTheLine(t *testing.T) {
	t.Parallel()
	l, _ := newLineStyle(t, "select 1")
	if got := l.Text(); got != "select 1" {
		t.Errorf("Text gave %q, want %q", got, "select 1")
	}
	var nilStyle *LineStyle
	if got := nilStyle.Text(); got != "" {
		t.Errorf("Text on nothing gave %q", got)
	}
}

// TestHighlighterFuncRunsTheFunction checks the adapter that makes an
// ordinary function into a Highlighter.
// TestStyleRefusesANegativePosition checks the promise both marking methods
// make in their doc comments: a negative position is refused.
//
// It is a promise rather than a detail, because a negative number already
// means something here — inside the marking, a negative position is read as
// an offset in characters, which is the convention the C uses. So without
// the guard Style(-3, ...) would quietly mark from the third character
// instead of doing nothing, which is the difference between refusing a bad
// argument and acting on a misread one.
//
// Nothing held the guard before. Taking it out of either method left the
// whole suite green, while the comment beside it said what it was for.
func TestStyleRefusesANegativePosition(t *testing.T) {
	t.Parallel()
	// Three characters of three bytes each, so a position read as characters
	// would land somewhere a position read as bytes never could.
	const line = "日本語"

	// The position has to land inside the line once it is read as
	// characters, or the marking stops for want of anything to mark and the
	// guard is never the reason nothing happened. -1 is the second
	// character, three bytes in; -3 is the end, and an earlier version of
	// this test used it and passed with the guard taken out.
	for _, test := range []struct {
		name string
		mark func(l *LineStyle)
	}{
		{"Style", func(l *LineStyle) { l.Style(-1, 3, "keyword") }},
		{"StyleRunes", func(l *LineStyle) { l.StyleRunes(-1, 1, "keyword") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			l, ab := newLineStyle(t, line)
			test.mark(l)
			if marked := markedBytes(ab, len(line)); marked != 0 {
				t.Errorf("a negative position marked %d bytes, want none: it was "+
					"read as an offset rather than refused", marked)
			}
		})
	}
}

// TestStyleWithNoNameChangesNothing checks that naming no style marks
// nothing, which is what a highlighter that has decided a stretch needs no
// styling should be able to say.
//
// The guard that returns early for it is not what makes this true: an
// unnamed style resolves to an attribute that says nothing, and laying one
// of those over a stretch leaves it as it was. So taking the guard out
// changes no attribute, and this test would pass without it. It is here for
// the promise rather than for the guard, and it should not be read as
// covering the guard — nothing does, and nothing needs to.
func TestStyleWithNoNameChangesNothing(t *testing.T) {
	t.Parallel()
	const line = "hello"
	l, ab := newLineStyle(t, line)
	l.Style(0, len(line), "keyword")
	before := markedBytes(ab, len(line))
	l.Style(0, len(line), "")
	if after := markedBytes(ab, len(line)); after != before {
		t.Errorf("naming no style changed the marking from %d bytes to %d", before, after)
	}
}

func TestHighlighterFuncRunsTheFunction(t *testing.T) {
	t.Parallel()
	var sink bytes.Buffer
	tm := newTerm(&sink, termOptions{NoColor: true})
	bb := newBBCode(tm)
	bb.styleDef("keyword", "bold")
	var ab ansi.AttrBuf

	var sawText string
	h := HighlighterFunc(func(l *LineStyle) {
		sawText = l.Text()
		l.Style(0, 6, "keyword")
	})
	runHighlight(bb, "select 1", &ab, h)

	if sawText != "select 1" {
		t.Errorf("the highlighter was given %q, want %q", sawText, "select 1")
	}
	if marked := markedBytes(&ab, 8); marked != 6 {
		t.Errorf("the highlighter marked %d bytes, want 6", marked)
	}
}

// TestStyleNamesResolveInOrder pins which answer wins when a name could be
// more than one thing.
//
// A tag's name is looked up in four places in turn: the properties, the
// styles the caller defined, the builtin styles, and the HTML colour names.
// The order between them is a decision, and the recordings cannot reach any
// of the boundaries: tools/probe-bbcode.c defines exactly two styles,
// "mystyle" and "other", and neither collides with a property, a builtin or
// a colour, so every recorded lookup finds its answer in one place only.
// Measured rather than supposed — swapping the caller styles with the
// builtins, putting the colours in front of the caller styles, and reversing
// the search for the newest definition all leave the whole suite green.
//
// The fourth boundary, a property against a style of the same name, is
// pinned separately by the Linux session, who found it while restructuring
// the parser. This covers the other three.
func TestStyleNamesResolveInOrder(t *testing.T) {
	restore := saveEnv(t)
	defer restore()
	setTermEnv("", "xterm-256color", "")

	// styleFor returns the attributes a tag of this name resolves to.
	styleFor := func(define func(bb *bbCode), name string) ansi.Attr {
		var sink bytes.Buffer
		tm := newTerm(&sink, termOptions{NoColor: true, Sizer: fixedSize{cols: 80, rows: 24}})
		bb := newBBCode(tm)
		define(bb)
		return bb.style(name)
	}

	t.Run("the newest definition of a name wins", func(t *testing.T) {
		got := styleFor(func(bb *bbCode) {
			bb.styleDef("twice", "color=red")
			bb.styleDef("twice", "color=lime")
		}, "twice")
		if want := ansi.RGBHex(0x00ff00); got.Fg != want {
			t.Errorf("the color is %08x, want %08x: the older definition won", got.Fg, want)
		}
	})

	t.Run("a caller's style beats a builtin of that name", func(t *testing.T) {
		// "b" is builtin and means bold. A caller redefining it must be
		// obeyed, or a program cannot name its own styles freely.
		got := styleFor(func(bb *bbCode) {
			bb.styleDef("b", "color=red")
		}, "b")
		if got.Bold == ansi.FlagOn {
			t.Error("the builtin bold won, so a caller cannot redefine a builtin name")
		}
		if want := ansi.RGBHex(0xff0000); got.Fg != want {
			t.Errorf("the color is %08x, want %08x", got.Fg, want)
		}
	})

	t.Run("a caller's style beats a colour name", func(t *testing.T) {
		// "red" is an HTML colour. A caller who defines a style called red
		// has said what red means to them.
		got := styleFor(func(bb *bbCode) {
			bb.styleDef("red", "underline")
		}, "red")
		if got.Underline != ansi.FlagOn {
			t.Error("the colour name won, so a caller cannot define a style named after a colour")
		}
		if got.Fg != ansi.None {
			t.Errorf("the color is %08x, want none: the colour name was applied as well", got.Fg)
		}
	})
}
