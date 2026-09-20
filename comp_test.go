package rline

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// --------------------------------------------------------------------------
// completions_test.go

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
	// A floor would let the corpus shrink. These tests are driven by the
	// corpus rather than replaying a script against it, so a line that goes
	// missing is one case fewer checked and nothing else: measured by
	// deleting a line from the middle and watching the suite stay green.
	// The exact count is the smallest thing that notices, and changing it is
	// a deliberate edit beside the corpus it describes.
	if len(lines) != 1647 {
		t.Fatalf("testdata/completions.txt holds %d lines, want %d: a corpus that changed size "+
			"was either regenerated on purpose, in which case set this number, "+
			"or lost lines, in which case it now checks less than it says",
			len(lines), 1647)
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
	inner := CompleterFunc(func(cenv *Completion, prefix string) {
		captured = prefix
		for _, a := range adds {
			cenv.add(a, "", "", 0, 0)
		}
	})
	c := &completions{}
	isWordChar := completionClasses[class]
	c.setCompleter(CompleterFunc(func(cenv *Completion, prefix string) {
		if quoted {
			completeQWordEx(cenv, prefix, inner, isWordChar, escape, quotes)
		} else {
			completeWord(cenv, prefix, inner, isWordChar)
		}
	}), nil)
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

// --------------------------------------------------------------------------
// completers_test.go

// collect runs a completer over a line and returns what it offered.
func collect(t *testing.T, c *completions, input string, cursor, maxOffers int) []completion {
	t.Helper()
	c.generate(input, cursor, maxOffers)
	return c.items
}

// TestCompleteQWordUsesTheUsualQuoting checks that the short form is the long
// form with a backslash and the two usual quotes.
func TestCompleteQWordUsesTheUsualQuoting(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		input string
	}{
		{"a plain word", "ls fi"},
		{"inside single quotes", "ls 'my fi"},
		{"inside double quotes", "ls \"my fi"},
		{"an escaped space", "ls my\\ fi"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			inner := CompleterFunc(func(cenv *Completion, _ string) {
				cenv.add("my file.txt", "", "", 0, 0)
			})
			short := &completions{}
			short.setCompleter(CompleterFunc(func(cenv *Completion, prefix string) {
				completeQWord(cenv, prefix, inner, nil)
			}), nil)
			long := &completions{}
			long.setCompleter(CompleterFunc(func(cenv *Completion, prefix string) {
				completeQWordEx(cenv, prefix, inner, nil, defaultEscapeChar, defaultQuoteChars)
			}), nil)

			cursor := len(test.input)
			got := collect(t, short, test.input, cursor, 10)
			want := collect(t, long, test.input, cursor, 10)
			if len(got) != len(want) {
				t.Fatalf("the short form offered %d, the long form %d", len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("completion %d differs: %+v against %+v", i, got[i], want[i])
				}
			}
		})
	}
}

// TestAddCompletionsFiltersByPrefix checks the helper that offers the members
// of a fixed list that start with what was typed. The comparison ignores
// case, as the C one does.
func TestAddCompletionsFiltersByPrefix(t *testing.T) {
	t.Parallel()
	words := []string{"print", "printf", "println", "parse", "Print3"}
	for _, test := range []struct {
		name   string
		prefix string
		want   []string
	}{
		// Print3 matches too, because the comparison folds case.
		{"a shared start", "pri", []string{"print", "printf", "println", "Print3"}},
		{"case does not matter", "PRI", []string{"print", "printf", "println", "Print3"}},
		{"a start that ignores case", "print3", []string{"Print3"}},
		{"a longer start", "printf", []string{"printf"}},
		{"everything", "", words},
		{"nothing matches", "zz", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := &completions{}
			c.setCompleter(CompleterFunc(func(cenv *Completion, prefix string) {
				addCompletions(cenv, prefix, words)
			}), nil)
			c.generate(test.prefix, len(test.prefix), 100)
			if c.count() != len(test.want) {
				t.Fatalf("offered %d %v, want %d %q", c.count(), c.items, len(test.want), test.want)
			}
			for i, w := range test.want {
				if got := c.items[i].replacement; got != w {
					t.Errorf("completion %d is %q, want %q", i, got, w)
				}
			}
		})
	}
}

// TestAddCompletionsStopsWhenFull checks that the helper gives up once no
// more are accepted, rather than walking the whole list.
func TestAddCompletionsStopsWhenFull(t *testing.T) {
	t.Parallel()
	words := []string{"a1", "a2", "a3", "a4", "a5"}
	c := &completions{}
	finished := false
	c.setCompleter(CompleterFunc(func(cenv *Completion, prefix string) {
		finished = addCompletions(cenv, prefix, words)
	}), nil)
	c.generate("a", 1, 2)
	if finished {
		t.Error("the helper reported it got through the whole list")
	}
	if got := c.count(); got != 2 {
		t.Errorf("it offered %d, want 2", got)
	}
	if !c.stopCompleting() {
		t.Error("stopCompleting said no after the list filled up")
	}
}

// TestHasCompletionsAndStopCompleting checks the two questions a completer
// asks while it works: whether anything has been found yet, and whether to
// give up.
func TestHasCompletionsAndStopCompleting(t *testing.T) {
	t.Parallel()
	c := &completions{}
	if c.hasCompletions() {
		t.Error("an empty list said it has completions")
	}
	if !c.stopCompleting() {
		t.Error("a list that accepts nothing said to keep going")
	}
	c.completerMax = 2
	if c.stopCompleting() {
		t.Error("a list with room said to stop")
	}
	c.add("one", "", "", 0, 0)
	if !c.hasCompletions() {
		t.Error("a list with an entry said it has none")
	}
	c.add("two", "", "", 0, 0)
	if !c.stopCompleting() {
		t.Error("a full list said to keep going")
	}
}

// TestCompletionLimitsAreConsistent checks the two limits against each other.
// The menu shows at most one, and a completer is asked to stop at the other,
// which has to be the smaller of the two or the menu could never fill.
func TestCompletionLimitsAreConsistent(t *testing.T) {
	t.Parallel()
	if maxCompletionsToTry > maxCompletionsToShow {
		t.Errorf("the limit on trying, %d, is above the limit on showing, %d",
			maxCompletionsToTry, maxCompletionsToShow)
	}
	if maxCompletionsToTry <= 0 || maxCompletionsToShow <= 0 {
		t.Errorf("the limits are %d and %d, and both have to leave room for something",
			maxCompletionsToTry, maxCompletionsToShow)
	}
}

// TestGenerateRefusesACursorPastTheEnd checks that a cursor outside the line
// offers nothing rather than reading past it.
func TestGenerateRefusesACursorPastTheEnd(t *testing.T) {
	t.Parallel()
	called := false
	c := &completions{}
	c.setCompleter(CompleterFunc(func(cenv *Completion, prefix string) {
		called = true
		cenv.add("x", "", "", 0, 0)
	}), nil)
	for _, cursor := range []int{-1, 4, 100} {
		called = false
		if got := c.generate("abc", cursor, 10); got != 0 {
			t.Errorf("a cursor at %d offered %d completions, want 0", cursor, got)
		}
		if called {
			t.Errorf("a cursor at %d reached the completer", cursor)
		}
	}
	if got := c.generate("abc", 3, 10); got != 1 {
		t.Errorf("a cursor at the end offered %d, want 1", got)
	}
}

// TestGeneratePassesTheCompleterArg checks that whatever the program handed
// over when it set its completer reaches the completer again.
func TestGeneratePassesTheCompleterArg(t *testing.T) {
	t.Parallel()
	type marker struct{ n int }
	want := &marker{n: 42}
	var got any
	c := &completions{}
	c.setCompleter(CompleterFunc(func(cenv *Completion, _ string) {
		got = cenv.arg
	}), want)
	c.generate("x", 1, 10)
	if got != any(want) {
		t.Errorf("the completer was given %v, want %v", got, want)
	}
}

// --------------------------------------------------------------------------
// filenames_test.go

const (
	// filenamesCorpusPath holds what the C file name completion did.
	filenamesCorpusPath = "testdata/filenames.txt"

	// filenamesProbePath is where tools/build-probe-filenames.sh puts the
	// probe.
	filenamesProbePath = ".build/probe-filenames"
)

// makeProbeTree builds the same tree the probe builds, in dir.
//
// Everything here can be made without special privileges. A block device and
// a character device cannot, so those two file types are left to the unit
// test below, which finds existing ones.
func makeProbeTree(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{
		"apple.txt", "apricot.md", "cherry.TXT", "date.c", "UPPER.txt",
		".hidden", "no-ext",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "exec.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing exec.sh: %v", err)
	}
	for name, mode := range map[string]os.FileMode{
		"banana":  0o755,
		"sub dir": 0o755,
		"sticky":  0o755 | os.ModeSticky,
	} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatalf("making %s: %v", name, err)
		}
		if err := os.Chmod(filepath.Join(dir, name), mode); err != nil {
			t.Fatalf("setting the mode of %s: %v", name, err)
		}
	}
	inner := filepath.Join(dir, "banana", "inner.txt")
	if err := os.WriteFile(inner, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing inner.txt: %v", err)
	}
	if err := os.Symlink("apple.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatalf("making the link: %v", err)
	}
}

// TestFilenamesPort replays every recorded case and checks that the Go port
// does the same. The cases that read the file system run against the same
// tree the probe built.
func TestFilenamesPort(t *testing.T) {
	if *update {
		regenerateFilenames(t)
	}
	b, err := os.ReadFile(filenamesCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-filenames.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	// A floor would let the corpus shrink. This test is driven by the corpus
	// rather than replaying a script against it, so a line that goes missing
	// is one case fewer checked and nothing else. The exact count is the
	// smallest thing that notices, and changing it is a deliberate edit
	// beside the corpus it describes.
	if len(lines) != 540 {
		t.Fatalf("testdata/filenames.txt holds %d lines, want %d: a corpus that changed size was "+
			"either regenerated on purpose, in which case set this number, or "+
			"lost lines, in which case it now checks less than it says",
			len(lines), 540)
	}

	// The file cases complete against the working directory, so the test
	// moves into a copy of the tree the probe used. This cannot run beside
	// another test that also changes directory, so nothing here is parallel.
	dir := t.TempDir()
	makeProbeTree(t, dir)
	t.Chdir(dir)

	// The colour settings come from the environment, so start from a known
	// one. The corpus records what each case sets.
	for _, name := range []string{"CLICOLOR", "LS_COLORS", "LSCOLORS"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("clearing %s: %v", name, err)
		}
	}

	counts := make(map[string]int, 8)
	bad := 0
	for i, line := range lines {
		kind, diff := checkFilenameLine(t, line)
		counts[kind]++
		if diff == "" {
			continue
		}
		bad++
		if bad <= maxReported {
			t.Errorf("%s:%d: %s\n  %s", filenamesCorpusPath, i+1, diff, line)
		}
	}
	if bad > maxReported {
		t.Errorf("%d differences in total, %d shown", bad, maxReported)
	}
	for _, kind := range []string{"extmatch", "colorize", "files"} {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
		}
	}
	t.Logf("checked %d cases: %v", len(lines), counts)
}

// checkFilenameLine checks one recorded case.
func checkFilenameLine(t *testing.T, line string) (string, string) {
	t.Helper()
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", "empty line"
	}
	p := &fieldReader{t: t, f: f, i: 1}
	switch f[0] {
	case "extmatch":
		name, extensions, want := p.str(), p.str(), p.flag()
		if got := matchExtension(name, extensions); got != want {
			return f[0], fmt.Sprintf("matchExtension(%q, %q) = %v, want %v",
				name, extensions, got, want)
		}
	case "colorize":
		clicolor, gnu, bsd := p.envStr(), p.envStr(), p.envStr()
		ft := fileType(p.num())
		name, ext := p.str(), p.nullableStr()
		hasExt := f[6] != "!"
		dirSep := mustHex(t, p.next())[0]
		noColor, want := p.flag(), p.next()
		setEnvOrUnset(t, "CLICOLOR", clicolor)
		setEnvOrUnset(t, "LS_COLORS", gnu)
		setEnvOrUnset(t, "LSCOLORS", bsd)
		got := colorizeEntry(noColor, ft, name, ext, hasExt, dirSep)
		if h := hexOrDash([]byte(got)); h != want {
			return f[0], fmt.Sprintf("colorizeEntry gave %s, want %s\n  got  %q", h, want, got)
		}
	case "files":
		return f[0], checkFilesCase(t, p)
	default:
		return f[0], "unknown kind of case"
	}
	return f[0], ""
}

// checkFilesCase checks one recorded completion against the tree.
func checkFilesCase(t *testing.T, p *fieldReader) string {
	t.Helper()
	prefix := p.str()
	dirSep := mustHex(t, p.next())[0]
	roots, extensions := p.str(), p.str()
	noColor := p.flag()

	// The probe turns colouring off for these, so the display stays plain.
	setEnvOrUnset(t, "CLICOLOR", "")
	setEnvOrUnset(t, "LS_COLORS", "")
	setEnvOrUnset(t, "LSCOLORS", "")

	c := &completions{}
	c.setCompleter(CompleterFunc(func(cenv *Completion, word string) {
		completeFilename(cenv, word, noColor, dirSep, roots, extensions)
	}), nil)
	c.completerMax = 200
	cenv := &Completion{input: prefix, cursor: len(prefix)}
	cenv.add = func(replacement, display, help string, before, after int) bool {
		return c.add(replacement, display, help, before, after)
	}
	c.completer.Complete(cenv, prefix)

	got := make([]string, 0, c.count())
	for _, cm := range c.items {
		got = append(got, fmt.Sprintf("%s:%s:%d:%d",
			hexOrDash([]byte(cm.replacement)), hexOrDash([]byte(cm.display)),
			cm.deleteBefore, cm.deleteAfter))
	}
	// The order is whatever the directory gives, which is not the same on
	// every file system, so both sides are sorted. The menu sorts before it
	// shows, so nothing depends on the order.
	slices.Sort(got)
	want := p.rawList()
	slices.Sort(want)
	if len(got) != len(want) {
		return fmt.Sprintf("completing %q offered %d, want %d\n  got  %v\n  want %v",
			prefix, len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			return fmt.Sprintf("completing %q gave %s at %d, want %s", prefix, got[i], i, want[i])
		}
	}
	return ""
}

// envStr reads a field that names an environment variable's value, where a
// null pointer means the variable was not set at all.
func (p *fieldReader) envStr() string {
	p.t.Helper()
	s := p.next()
	if s == "!" {
		return "\x00unset"
	}
	if s == "-" {
		return ""
	}
	return string(mustHex(p.t, s))
}

// setEnvOrUnset sets a variable, or removes it when the recorded value says
// it was not set.
func setEnvOrUnset(t *testing.T, name, value string) {
	t.Helper()
	if value == "\x00unset" {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("clearing %s: %v", name, err)
		}
		return
	}
	t.Setenv(name, value)
}

// regenerateFilenames runs the C probe and writes the corpus.
func regenerateFilenames(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filenamesProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-filenames.sh", filenamesProbePath)
	}
	out, err := exec.Command(filenamesProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(filenamesCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", filenamesCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", filenamesCorpusPath, len(out))
}

// TestTypeOfRealEntries checks the mapping from a real directory entry to
// the type that decides its colour. Only a real file system can answer this,
// so the corpus cannot: it passes the type in rather than working it out.
//
// A block device and a character device cannot be made without privileges,
// so those two use ones the system already has, and are skipped when they
// are not there.
func TestTypeOfRealEntries(t *testing.T) {
	dir := t.TempDir()
	at := func(name string) string { return filepath.Join(dir, name) }

	mustWrite := func(name string, mode os.FileMode) string {
		t.Helper()
		path := at(name)
		if err := os.WriteFile(path, []byte("x"), mode); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("setting the mode of %s: %v", name, err)
		}
		return path
	}
	mustDir := func(name string, mode os.FileMode) string {
		t.Helper()
		path := at(name)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatalf("making %s: %v", name, err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("setting the mode of %s: %v", name, err)
		}
		return path
	}

	link := at("link")
	if err := os.Symlink("plain", link); err != nil {
		t.Skipf("this system will not make a symbolic link: %v", err)
	}

	tests := []struct {
		name string
		path string
		want fileType
		// fromPermissions says the answer is worked out from the permission
		// bits. Windows does not keep them: os.Chmod there maps only the
		// owner write bit onto the read-only attribute and drops the rest,
		// so a file always reads back as 0666 and a directory as 0777.
		// There is nothing to classify against, so these are skipped rather
		// than given different expected values.
		//
		// One of them would otherwise pass. A directory the group may write
		// expects the same answer that every directory gives on Windows, so
		// it would be green for the wrong reason and would stay green
		// through a real regression. That is worse than a skip.
		fromPermissions bool
	}{
		{"a symbolic link", link, ftSym, false},
		{"an ordinary file", mustWrite("plain", 0o644), ftDefault, false},
		{"a missing path", at("nothing-here"), ftDefault, false},
		{"a file anyone may run", mustWrite("runnable", 0o755), ftExe, true},
		{"a directory", mustDir("plaindir", 0o755), ftDir, true},
		{"a sticky directory", mustDir("stickydir", 0o755|os.ModeSticky), ftDirSticky, true},
		{"a directory the group may write", mustDir("groupdir", 0o775), ftDirOtherWritable, true},
		{"a sticky directory the group may write",
			mustDir("groupsticky", 0o775|os.ModeSticky), ftDirOtherWritableSticky, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.fromPermissions && runtime.GOOS == "windows" {
				t.Skip("this system does not keep the permission bits, so there is nothing to classify")
			}
			if got := typeOf(test.path); got != test.want {
				t.Errorf("typeOf(%s) = %d, want %d", test.name, got, test.want)
			}
		})
	}

	// A character device, found rather than assumed.
	//
	// This used to name /dev/null and say it is a character device every
	// system has. On illumos it is a symlink to
	// ../devices/pseudo/mm@0:null, and typeOf uses Lstat as the C does, so
	// it correctly answered "symlink" and the test failed. That was the
	// third time a test here rested on an unchecked claim about a
	// filesystem, after a path through a file on Windows and a directory
	// that reads as data on NetBSD.
	//
	// So the path is followed to whatever it really is, and each kind is
	// checked where the system has one.
	t.Run("a character device", func(t *testing.T) {
		info, err := os.Lstat("/dev/null")
		if err != nil {
			t.Skip("there is no /dev/null here, so no character device was checked")
		}
		path := "/dev/null"
		if info.Mode()&fs.ModeSymlink != 0 {
			// The link itself is a symlink, which is its own case below.
			if got := typeOf(path); got != ftSym {
				t.Errorf("typeOf(%s) = %d, want %d: it is a symlink here", path, got, ftSym)
			}
			if path, err = filepath.EvalSymlinks(path); err != nil {
				t.Skipf("/dev/null is a symlink that does not resolve (%v), "+
					"so no character device was checked", err)
			}
			if info, err = os.Lstat(path); err != nil {
				t.Skipf("%s does not stat (%v), so no character device was checked", path, err)
			}
		}
		if info.Mode()&fs.ModeCharDevice == 0 {
			t.Skipf("%s is %v rather than a character device, so none was checked",
				path, info.Mode())
		}
		if got := typeOf(path); got != ftChar {
			t.Errorf("typeOf(%s) = %d, want %d", path, got, ftChar)
		}
	})
}

// TestTypeOfSetuidDirectory is separate because a file system can refuse to
// set these bits, and that is not a failure of the port.
func TestTypeOfSetuidDirectory(t *testing.T) {
	for _, test := range []struct {
		name string
		mode os.FileMode
		want fileType
	}{
		{"set user", 0o755 | os.ModeSetuid, ftSetuid},
		{"set group", 0o755 | os.ModeSetgid, ftSetgid},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "d")
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatalf("making the directory: %v", err)
			}
			if err := os.Chmod(path, test.mode); err != nil {
				t.Skipf("this file system will not take the mode: %v", err)
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("reading it back: %v", err)
			}
			if info.Mode()&(os.ModeSetuid|os.ModeSetgid) == 0 {
				t.Skip("the file system dropped the bit")
			}
			if got := typeOf(path); got != test.want {
				t.Errorf("typeOf = %d, want %d", got, test.want)
			}
		})
	}
}

// TestIsDirFollowsALink checks that a link to a directory completes as a
// directory, which is why the type and the directory test use different
// calls: one follows a link and the other does not.
func TestIsDirFollowsALink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("making the target: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink("target", link); err != nil {
		t.Skipf("this system will not make a symbolic link: %v", err)
	}
	if !isDir(link) {
		t.Error("a link to a directory did not count as one")
	}
	if got := typeOf(link); got != ftSym {
		t.Errorf("the type of a link to a directory is %d, want %d", got, ftSym)
	}
}

// ----------------------------------------------------------------------------
// What a completer can see

// TestCompletionSeesTheWholeLine checks Text and Cursor, which are what a
// completer reads when the prefix it was handed is not enough to decide.
//
// The example needs exactly this: with the cursor in the middle of a word it
// must offer nothing, or completing "wh" inside "where" would give
// "whereere". The prefix alone cannot say, because it stops at the cursor.
func TestCompletionSeesTheWholeLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		line   string
		cursor int
		want   string
	}{
		{"at the end", "select wh", 9, "select wh"},
		{"inside a word", "select where", 9, "select where"},
		{"at the start", "abc", 0, "abc"},
		{"an empty line", "", 0, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var gotText string
			var gotCursor, gotPrefixLen int
			c := &completions{}
			c.setCompleter(CompleterFunc(func(cenv *Completion, prefix string) {
				gotText, gotCursor, gotPrefixLen = cenv.Text(), cenv.Cursor(), len(prefix)
			}), nil)
			c.generate(test.line, test.cursor, 10)

			if gotText != test.want {
				t.Errorf("Text gave %q, want the whole line %q", gotText, test.want)
			}
			if gotCursor != test.cursor {
				t.Errorf("Cursor gave %d, want %d", gotCursor, test.cursor)
			}
			// The prefix stops at the cursor; the line does not. That
			// difference is the reason both exist.
			if gotPrefixLen != test.cursor {
				t.Errorf("the prefix is %d bytes, want %d", gotPrefixLen, test.cursor)
			}
		})
	}

	t.Run("nothing answers nothing", func(t *testing.T) {
		t.Parallel()
		var c *Completion
		if got := c.Text(); got != "" {
			t.Errorf("Text on nothing gave %q", got)
		}
		if got := c.Cursor(); got != 0 {
			t.Errorf("Cursor on nothing gave %d", got)
		}
	})
}

// TestCompleterFuncRunsTheFunction checks the adapter that makes an ordinary
// function into a Completer.
func TestCompleterFuncRunsTheFunction(t *testing.T) {
	t.Parallel()
	c := &completions{}
	c.setCompleter(CompleterFunc(func(cenv *Completion, prefix string) {
		cenv.AddCandidate(Candidate{Replacement: prefix + "x", DeleteBefore: len(prefix)})
	}), nil)
	if n := c.generate("ab", 2, 10); n != 1 {
		t.Fatalf("the completer offered %d, want 1", n)
	}
	cm, ok := c.get(0)
	if !ok {
		t.Fatal("nothing was kept")
	}
	if cm.replacement != "abx" || cm.deleteBefore != 2 {
		t.Errorf("the candidate is %q taking away %d, want %q taking away 2",
			cm.replacement, cm.deleteBefore, "abx")
	}
}
