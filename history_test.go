package rline

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// historyCorpusPath holds what the C history and undo code did.
	historyCorpusPath = "testdata/history.txt"

	// historyProbePath is where tools/build-probe-history.sh puts the probe.
	historyProbePath = ".build/probe-history"
)

// fieldReader walks the fields of a recorded line in order. Several kinds of
// line carry a list whose length is given by the field in front of it, so
// reading them by position is easier to get wrong than reading them in turn.
type fieldReader struct {
	t *testing.T
	f []string
	i int
}

// next returns the next field as it was written.
func (p *fieldReader) next() string {
	p.t.Helper()
	if p.i >= len(p.f) {
		p.t.Fatalf("the line ran out after %d fields: %s", len(p.f), strings.Join(p.f, " "))
	}
	s := p.f[p.i]
	p.i++
	return s
}

// str returns the next field as the bytes it stands for.
func (p *fieldReader) str() string {
	p.t.Helper()
	return string(mustHex(p.t, p.next()))
}

// num returns the next field as a number.
func (p *fieldReader) num() int {
	p.t.Helper()
	return mustInt(p.t, p.next())
}

// flag returns the next field as a yes or no.
func (p *fieldReader) flag() bool {
	p.t.Helper()
	return p.num() == 1
}

// list returns a list of strings, led by how many there are.
func (p *fieldReader) list() []string {
	p.t.Helper()
	n := p.num()
	out := make([]string, n)
	for i := range n {
		out[i] = p.str()
	}
	return out
}

// rest returns every field that is left, as written.
func (p *fieldReader) rest() []string {
	return p.f[p.i:]
}

// TestHistoryPort replays every recorded call to the C history and undo code
// and checks that the Go port does the same.
func TestHistoryPort(t *testing.T) {
	if *update {
		regenerateHistory(t)
	}
	b, err := os.ReadFile(historyCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-history.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) < 300 {
		t.Fatalf("the corpus holds %d lines, which is too few to prove anything", len(lines))
	}
	counts := make(map[string]int, 16)
	bad := 0
	for i, line := range lines {
		kind, diff := checkHistoryLine(t, line)
		counts[kind]++
		if diff == "" {
			continue
		}
		bad++
		if bad <= maxReported {
			t.Errorf("%s:%d: %s\n  %s", historyCorpusPath, i+1, diff, line)
		}
	}
	if bad > maxReported {
		t.Errorf("%d differences in total, %d shown", bad, maxReported)
	}
	for _, kind := range []string{
		"wentry", "rentry", "push", "update", "get", "remove", "search", "file", "undo",
	} {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
		}
	}
	t.Logf("checked %d cases: %v", len(lines), counts)
}

// checkHistoryLine checks one recorded case.
func checkHistoryLine(t *testing.T, line string) (string, string) {
	t.Helper()
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", "empty line"
	}
	p := &fieldReader{t: t, f: f, i: 1}
	switch f[0] {
	case "wentry":
		entry, want := p.str(), p.next()
		// The C writes the escaped entry and a newline, and writes nothing at
		// all when there is nothing left after escaping.
		got := escapeEntry(entry)
		if got != "" {
			got += "\n"
		}
		if h := hexOrDash([]byte(got)); h != want {
			return f[0], fmt.Sprintf("escapeEntry(%q) wrote %s, want %s", entry, h, want)
		}
	case "rentry":
		line, wantOK := p.str(), p.flag()
		h := newTestHistory(8, true)
		var buf buffer
		r := bufio.NewReader(strings.NewReader(line + "\n"))
		if got := h.readEntry(r, &buf); got != wantOK {
			return f[0], fmt.Sprintf("readEntry(%q) reported %v, want %v", line, got, wantOK)
		}
		return f[0], diffEntries(h, p)
	case "push":
		maxEntries, dups := p.num(), p.flag()
		in := p.list()
		h := newTestHistory(maxEntries, dups)
		for i, entry := range in {
			if got, want := h.push(entry), p.flag(); got != want {
				return f[0], fmt.Sprintf("push %d of %q reported %v, want %v", i, entry, got, want)
			}
		}
		return f[0], diffEntries(h, p)
	case "update":
		maxEntries, dups := p.num(), p.flag()
		in := p.list()
		h := newTestHistory(maxEntries, dups)
		for _, entry := range in {
			h.push(entry)
		}
		with := p.str()
		if got, want := h.update(with), p.flag(); got != want {
			return f[0], fmt.Sprintf("update(%q) reported %v, want %v", with, got, want)
		}
		return f[0], diffEntries(h, p)
	case "get":
		maxEntries := p.num()
		in := p.list()
		h := newTestHistory(maxEntries, true)
		for _, entry := range in {
			h.push(entry)
		}
		// The probe asks for one past each end, so the answers run from -1 to
		// the count plus one.
		for i, want := range p.rest() {
			n := i - 1
			got, ok := h.get(n)
			// A index outside the list answers with nothing, which the probe
			// writes as an exclamation mark for the C null pointer.
			if !ok {
				if want != "!" {
					return f[0], fmt.Sprintf("get(%d) found nothing, want %s", n, want)
				}
				continue
			}
			if h := hexOrDash([]byte(got)); h != want {
				return f[0], fmt.Sprintf("get(%d) = %s, want %s", n, h, want)
			}
		}
	case "remove":
		maxEntries, times := p.num(), p.num()
		in := p.list()
		h := newTestHistory(maxEntries, true)
		for _, entry := range in {
			h.push(entry)
		}
		for range times {
			h.removeLast()
		}
		return f[0], diffEntries(h, p)
	case "search":
		in := p.list()
		from, needle, backward := p.num(), p.str(), p.flag()
		wantOK, wantIdx, wantPos := p.flag(), p.num(), p.num()
		h := newTestHistory(16, true)
		for _, entry := range in {
			h.push(entry)
		}
		idx, pos, ok := h.search(from, needle, backward)
		if ok != wantOK {
			return f[0], fmt.Sprintf("search(%d, %q, %v) reported %v, want %v",
				from, needle, backward, ok, wantOK)
		}
		// The probe leaves its own markers in place when the search fails,
		// because the C code does not touch them then.
		if ok && (idx != wantIdx || pos != wantPos) {
			return f[0], fmt.Sprintf("search(%d, %q, %v) found %d at %d, want %d at %d",
				from, needle, backward, idx, pos, wantIdx, wantPos)
		}
	case "file":
		return f[0], checkHistoryFile(t, p)
	case "undo":
		in := p.list()
		restores := p.num()
		var s editStack
		for i, input := range in {
			s.capture(input, i)
		}
		for i := range restores {
			want := p.next()
			input, pos, ok := s.restore()
			got := fmt.Sprintf("%d:%s:%d", boolToInt(ok), hexOrDash([]byte(input)), pos)
			if !ok {
				// The probe writes the C null pointer as an exclamation mark
				// and leaves the position it started with.
				got = fmt.Sprintf("0:!:%d", -1)
			}
			if got != want {
				return f[0], fmt.Sprintf("restore %d gave %s, want %s", i, got, want)
			}
		}
	default:
		return f[0], "unknown kind of case"
	}
	return f[0], ""
}

// checkHistoryFile checks one recorded save and load. It writes the entries
// to a file, compares the bytes, then reads the file back.
func checkHistoryFile(t *testing.T, p *fieldReader) string {
	t.Helper()
	maxEntries, dups := p.num(), p.flag()
	in := p.list()
	wantFile := p.next()

	name := filepath.Join(t.TempDir(), "history.txt")
	h := &history{}
	h.enableDuplicates(dups)
	_ = h.loadFrom(name, maxEntries)
	for _, entry := range in {
		h.push(entry)
	}
	if err := h.save(); err != nil {
		return fmt.Sprintf("saving: %v", err)
	}
	b, err := os.ReadFile(name)
	if err != nil {
		// The C writes no file when there is nothing to write, and reading a
		// file that is not there is the same as reading an empty one.
		if !os.IsNotExist(err) {
			return fmt.Sprintf("reading the file back: %v", err)
		}
		b = nil
	}
	if got := hexOrDash(b); got != wantFile {
		return fmt.Sprintf("saved %s, want %s", got, wantFile)
	}

	reloaded := &history{}
	reloaded.enableDuplicates(dups)
	_ = reloaded.loadFrom(name, maxEntries)
	return diffEntries(reloaded, p)
}

// diffEntries compares the entries of h against the rest of a recorded line,
// which is the count and then each entry from the most recent.
func diffEntries(h *history, p *fieldReader) string {
	want := p.list()
	if got := h.count(); got != len(want) {
		return fmt.Sprintf("the list holds %d entries %v, want %d %q", got, h.entries, len(want), want)
	}
	for i, w := range want {
		got, ok := h.get(i)
		if !ok || got != w {
			return fmt.Sprintf("entry %d is %q, want %q (whole list %q)", i, got, w, h.entries)
		}
	}
	return ""
}

// newTestHistory returns a history with room for max entries, set up the way
// the probe sets one up: pointed at a file that is not there, so that nothing
// is read and only the room is allocated.
func newTestHistory(maxEntries int, dups bool) *history {
	h := &history{}
	h.enableDuplicates(dups)
	_ = h.loadFrom("/nonexistent/rline-probe-history", maxEntries)
	return h
}

// boolToInt renders a yes or no the way the probe writes one.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// regenerateHistory runs the C probe and writes the corpus.
func regenerateHistory(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(historyProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-history.sh", historyProbePath)
	}
	out, err := exec.Command(historyProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(historyCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", historyCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", historyCorpusPath, len(out))
}

// TestHistorySaveWithoutAFileDoesNothing checks that a history with nowhere
// to save is not an error.
func TestHistorySaveWithoutAFileDoesNothing(t *testing.T) {
	t.Parallel()
	h := &history{max: 8}
	h.push("a")
	if err := h.save(); err != nil {
		t.Errorf("saving with no file gave %v, want no error", err)
	}
}

// TestHistoryLoadStopsAtABadLine checks that a line the reader cannot make
// sense of stops the file, so that nothing after it is taken as real.
func TestHistoryLoadStopsAtABadLine(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "history.txt")
	content := "first\nsecond\n\\q\nthird\n"
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	h := &history{}
	err := h.loadFrom(name, 8)
	if got, want := h.entries, []string{"first", "second"}; !equalStrings(got, want) {
		t.Errorf("loaded %q, want %q", got, want)
	}
	// Stopping is reported as well as done. This is the third of the three
	// ways loading can fail, and the only one reachable with a file that
	// opens and reads perfectly well, so nothing else can reach it: the
	// error was thrown away here until a mutation that swallowed it came
	// back uncaught.
	if err == nil {
		t.Fatal("a line that could not be read was passed over without a word")
	}
	if !strings.Contains(err.Error(), name) {
		t.Errorf("the error is %v, which does not name the file", err)
	}
	if !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("the error is %v, which does not say what went wrong", err)
	}
}

// TestHistoryRoundTripsEveryByte checks that an entry made of every byte
// survives being written and read back, apart from the two that cannot.
func TestHistoryRoundTripsEveryByte(t *testing.T) {
	t.Parallel()
	var raw bytes.Buffer
	for b := range 256 {
		// A zero byte cannot be held, and a carriage return is dropped on the
		// way out on purpose, so neither can come back.
		if b == 0 || b == '\r' {
			continue
		}
		raw.WriteByte(byte(b))
	}
	entry := raw.String()

	name := filepath.Join(t.TempDir(), "history.txt")
	h := &history{}
	_ = h.loadFrom(name, 8)
	h.push(entry)
	if err := h.save(); err != nil {
		t.Fatalf("saving: %v", err)
	}
	back := &history{}
	_ = back.loadFrom(name, 8)
	got, ok := back.get(0)
	if !ok {
		t.Fatal("nothing came back")
	}
	if got != entry {
		t.Errorf("the entry came back as %q, want %q", got, entry)
	}
}

// equalStrings reports whether two lists hold the same strings.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
