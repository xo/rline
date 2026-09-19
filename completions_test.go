package rline

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const (
	// completionsCorpusPath holds what the C completion code did.
	completionsCorpusPath = "testdata/completions.txt"

	// completionsProbePath is where tools/build-probe-completions.sh puts
	// the probe.
	completionsProbePath = ".build/probe-completions"
)

// completionClasses is the order the probe names a character class by.
var completionClasses = []charClass{
	nil, charIsNonSeparator, charIsIDLetter, charIsFileNameLetter,
}

// TestCompletionsPort replays every recorded call to the C completion code
// and checks that the Go port does the same.
func TestCompletionsPort(t *testing.T) {
	if *update {
		regenerateCompletions(t)
	}
	b, err := os.ReadFile(completionsCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-completions.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) < 1000 {
		t.Fatalf("the corpus holds %d lines, which is too few to prove anything", len(lines))
	}
	counts := make(map[string]int, 16)
	bad := 0
	for i, line := range lines {
		kind, diff := checkCompletionLine(t, line)
		counts[kind]++
		if diff == "" {
			continue
		}
		bad++
		if bad <= maxReported {
			t.Errorf("%s:%d: %s\n  %s", completionsCorpusPath, i+1, diff, line)
		}
	}
	if bad > maxReported {
		t.Errorf("%d differences in total, %d shown", bad, maxReported)
	}
	for _, kind := range []string{
		"add", "display", "hint", "apply", "applymissing", "sort", "prefix",
		"prefixmixed", "word", "qword",
	} {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
		}
	}
	t.Logf("checked %d cases: %v", len(lines), counts)
}

// checkCompletionLine checks one recorded case.
func checkCompletionLine(t *testing.T, line string) (string, string) {
	t.Helper()
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", "empty line"
	}
	p := &fieldReader{t: t, f: f, i: 1}
	switch f[0] {
	case "add":
		maxOffers := p.num()
		in := p.list()
		c := &completions{completerMax: maxOffers}
		for i, entry := range in {
			if got, want := c.add(entry, "", "", 0, 0), p.flag(); got != want {
				return f[0], fmt.Sprintf("add %d of %q reported %v, want %v", i, entry, got, want)
			}
		}
		return f[0], diffCompletionViews(t, c, p)
	case "display":
		repl, display, help := p.nullableStr(), p.nullableStr(), p.nullableStr()
		wantShown, wantHelp := p.nullableStr(), p.nullableStr()
		c := &completions{completerMax: 10}
		c.add(repl, display, help, 0, 0)
		gotShown, gotHelp, ok := c.displayAt(0)
		if !ok {
			return f[0], "there is no completion to show"
		}
		// A Go caller cannot pass the absence of a string, so an empty
		// display means the same as none at all and shows the replacement.
		// The C code keeps them apart and shows the empty one. This is the
		// one place the two differ, and it is recorded in PLAN.md.
		if display == "" {
			wantShown = repl
		}
		if gotShown != wantShown || gotHelp != wantHelp {
			return f[0], fmt.Sprintf("displayAt gave %q and %q, want %q and %q",
				gotShown, gotHelp, wantShown, wantHelp)
		}
	case "hint":
		repl, before := p.str(), p.num()
		wantHint, wantHelp := p.next(), p.next()
		c := &completions{completerMax: 10}
		c.add(repl, "", "help", before, 0)
		hint, help, ok := c.hintAt(0)
		if !ok {
			if wantHint != "!" {
				return f[0], fmt.Sprintf("hintAt(%q, %d) found none, want %s", repl, before, wantHint)
			}
			return f[0], ""
		}
		if h := hexOrDash([]byte(hint)); h != wantHint {
			return f[0], fmt.Sprintf("hintAt(%q, %d) = %s, want %s", repl, before, h, wantHint)
		}
		if h := hexOrDash([]byte(help)); h != wantHelp {
			return f[0], fmt.Sprintf("hintAt(%q, %d) help = %s, want %s", repl, before, h, wantHelp)
		}
	case "apply":
		line, pos, repl := p.str(), p.num(), p.str()
		before, after, wantRes, wantBuf := p.num(), p.num(), p.num(), p.next()
		c := &completions{completerMax: 10}
		c.add(repl, "", "", before, after)
		buf := &buffer{}
		buf.appendString(line)
		if got := c.apply(0, buf, pos); got != wantRes {
			return f[0], fmt.Sprintf("apply gave %d, want %d (buffer %q)", got, wantRes, buf.string())
		}
		if got := hexOrDash(buf.bytes()); got != wantBuf {
			return f[0], fmt.Sprintf("apply left the buffer %s, want %s", got, wantBuf)
		}
	case "applymissing":
		index, wantRes, wantBuf := p.num(), p.num(), p.next()
		c := &completions{}
		buf := &buffer{}
		buf.appendString("line")
		if got := c.apply(index, buf, 2); got != wantRes {
			return f[0], fmt.Sprintf("apply at %d gave %d, want %d", index, got, wantRes)
		}
		if got := hexOrDash(buf.bytes()); got != wantBuf {
			return f[0], fmt.Sprintf("apply at %d left the buffer %s, want %s", index, got, wantBuf)
		}
	case "sort":
		in := p.list()
		c := &completions{completerMax: 100}
		for _, entry := range in {
			c.add(entry, "", "", 0, 0)
		}
		c.sort()
		want := p.list()
		if c.count() != len(want) {
			return f[0], fmt.Sprintf("sorting left %d, want %d", c.count(), len(want))
		}
		for i, w := range want {
			got, _, _ := c.displayAt(i)
			if got != w {
				return f[0], fmt.Sprintf("sorted entry %d is %q, want %q", i, got, w)
			}
		}
	case "prefix":
		line, pos, before := p.str(), p.num(), p.num()
		in := p.list()
		wantRes, wantBuf := p.num(), p.next()
		c := &completions{completerMax: 100}
		for _, entry := range in {
			c.add(entry, "", "", before, 0)
		}
		buf := &buffer{}
		buf.appendString(line)
		if got := c.applyLongestPrefix(buf, pos); got != wantRes {
			return f[0], fmt.Sprintf("applyLongestPrefix gave %d, want %d (buffer %q)",
				got, wantRes, buf.string())
		}
		if got := hexOrDash(buf.bytes()); got != wantBuf {
			return f[0], fmt.Sprintf("applyLongestPrefix left the buffer %s, want %s", got, wantBuf)
		}
		wantBefores := p.num()
		if c.count() != wantBefores {
			return f[0], fmt.Sprintf("the list holds %d, want %d", c.count(), wantBefores)
		}
		for i := range wantBefores {
			if got, want := c.items[i].deleteBefore, p.num(); got != want {
				return f[0], fmt.Sprintf("entry %d takes away %d before, want %d", i, got, want)
			}
		}
	case "prefixmixed":
		// The same as "prefix", except that each entry names how much of the
		// line it takes away. When they do not agree there is nothing safe
		// to fill in, which "prefix" cannot show, because it gives every
		// entry the same count.
		line, pos := p.str(), p.num()
		in := p.list()
		befores := make([]int, p.num())
		for i := range befores {
			befores[i] = p.num()
		}
		wantRes, wantBuf := p.num(), p.next()
		c := &completions{completerMax: 100}
		for i, entry := range in {
			c.add(entry, "", "", befores[i], 0)
		}
		buf := &buffer{}
		buf.appendString(line)
		if got := c.applyLongestPrefix(buf, pos); got != wantRes {
			return f[0], fmt.Sprintf("applyLongestPrefix gave %d, want %d (buffer %q)",
				got, wantRes, buf.string())
		}
		if got := hexOrDash(buf.bytes()); got != wantBuf {
			return f[0], fmt.Sprintf("applyLongestPrefix left the buffer %s, want %s", got, wantBuf)
		}
		wantCount := p.num()
		if c.count() != wantCount {
			return f[0], fmt.Sprintf("the list holds %d, want %d", c.count(), wantCount)
		}
		for i := range wantCount {
			if got, want := c.items[i].deleteBefore, p.num(); got != want {
				return f[0], fmt.Sprintf("entry %d takes away %d before, want %d", i, got, want)
			}
		}
	case "word", "qword":
		return f[0], checkWordCase(t, f[0] == "qword", p)
	default:
		return f[0], "unknown kind of case"
	}
	return f[0], ""
}

// checkWordCase checks one recorded word or quoted word completion.
func checkWordCase(t *testing.T, quoted bool, p *fieldReader) string {
	t.Helper()
	input, cursor, class := p.str(), p.num(), p.num()
	var escape byte
	var quotes string
	if quoted {
		escape = mustHex(t, p.next())[0]
		// The probe writes a null pointer when the caller named no quotes,
		// which means the default pair.
		quotes = p.nullableStr()
	}
	adds := p.list()
	wantWord := p.str()

	// The completer writes down the word it was handed and offers a fixed
	// set, which is how the deletion counts become readable.
	captured := ""
	inner := func(cenv *Completion, prefix string) {
		captured = prefix
		for _, a := range adds {
			cenv.add(a, "", "", 0, 0)
		}
	}
	c := &completions{}
	isWordChar := completionClasses[class]
	c.setCompleter(func(cenv *Completion, prefix string) {
		if quoted {
			completeQWordEx(cenv, prefix, inner, isWordChar, escape, quotes)
		} else {
			completeWord(cenv, prefix, inner, isWordChar)
		}
	}, nil)
	c.generate(input, cursor, 100)

	if captured != wantWord {
		return fmt.Sprintf("the completer was handed %q, want %q", captured, wantWord)
	}
	want := p.rawList()
	if c.count() != len(want) {
		return fmt.Sprintf("offered %d completions %v, want %d %q", c.count(), c.items, len(want), want)
	}
	for i, w := range want {
		cm := c.items[i]
		got := fmt.Sprintf("%s:%d:%d", hexOrDash([]byte(cm.replacement)), cm.deleteBefore, cm.deleteAfter)
		if got != w {
			return fmt.Sprintf("completion %d is %s, want %s", i, got, w)
		}
	}
	return ""
}

// diffCompletionViews compares what the menu would show for each completion
// against the rest of a recorded line.
func diffCompletionViews(t *testing.T, c *completions, p *fieldReader) string {
	t.Helper()
	n := p.num()
	if c.count() != n {
		return fmt.Sprintf("the list holds %d, want %d", c.count(), n)
	}
	for i := range n {
		want := p.next()
		display, help, _ := c.displayAt(i)
		hint, hintHelp, hintOK := c.hintAt(i)
		got := fmt.Sprintf("%s:%s:%s", hexOrDash([]byte(display)), nullableHex(help, help != ""), nullableHex(hint, hintOK))
		_ = hintHelp
		if got != want {
			return fmt.Sprintf("completion %d shows %s, want %s", i, got, want)
		}
	}
	return ""
}

// nullableHex renders a value the probe writes as a null pointer when it is
// not there.
func nullableHex(s string, present bool) string {
	if !present {
		return "!"
	}
	return hexOrDash([]byte(s))
}

// rawList returns a list of fields as they were written, led by how many
// there are. Some lists hold fields that join several values with a colon,
// which are not hexadecimal on their own.
func (p *fieldReader) rawList() []string {
	p.t.Helper()
	n := p.num()
	out := make([]string, n)
	for i := range n {
		out[i] = p.next()
	}
	return out
}

// nullableStr reads a field that the probe may have written as a null
// pointer, which becomes an empty string here.
func (p *fieldReader) nullableStr() string {
	p.t.Helper()
	s := p.next()
	if s == "!" {
		return ""
	}
	if s == "-" {
		return ""
	}
	return string(mustHex(p.t, s))
}

// regenerateCompletions runs the C probe and writes the corpus.
func regenerateCompletions(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(completionsProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-completions.sh", completionsProbePath)
	}
	out, err := exec.Command(completionsProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(completionsCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", completionsCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", completionsCorpusPath, len(out))
}
