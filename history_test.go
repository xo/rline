package rline

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xo/rline/internal/editor"
	"github.com/xo/rline/internal/text"
)

const (
	// historyCorpusPath holds what the C history and undo code did.
	historyCorpusPath = "testdata/history.txt"

	// historyProbePath is where tools/build-probe-history.sh puts the probe.
	historyProbePath = ".build/probe-history"
)

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
	// A floor would let the corpus shrink. These tests are driven by the
	// corpus rather than replaying a script against it, so a line that goes
	// missing is one case fewer checked and nothing else: measured by
	// deleting a line from the middle and watching the suite stay green.
	// The exact count is the smallest thing that notices, and changing it is
	// a deliberate edit beside the corpus it describes.
	if len(lines) != 424 {
		t.Fatalf("testdata/history.txt holds %d lines, want %d: a corpus that changed size "+
			"was either regenerated on purpose, in which case set this number, "+
			"or lost lines, in which case it now checks less than it says",
			len(lines), 424)
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
		var buf text.Buffer
		r := bufio.NewReader(strings.NewReader(line + "\n"))
		h.readEntry(r, &buf)
		if !wantOK {
			// The C refuses a line with a backslash that starts no escape it
			// knows, and stops the file there. The port keeps the line as it
			// is, backslash and all (D67). No line the C refused also holds
			// an escape that it reads, so the whole line comes back.
			if got, ok := h.get(0); h.count() != 1 || !ok || got != line {
				return f[0], fmt.Sprintf("readEntry(%q) read %q, want the line as it is", line, h.entries)
			}
			return f[0], ""
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
		var s editor.EditStack
		for i, input := range in {
			s.Capture(input, i)
		}
		for i := range restores {
			want := p.next()
			input, pos, ok := s.Restore()
			got := fmt.Sprintf("%d:%s:%d", boolInt(ok), hexOrDash([]byte(input)), pos)
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

// TestHistoryLoadKeepsALineItCannotDecode checks that a backslash that
// starts no escape is kept as text, and that the lines after it are read, so
// that a history file that another line editor wrote still loads and saves
// (D67). usql found this with a psql command in an old file.
func TestHistoryLoadKeepsALineItCannotDecode(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "history.txt")
	// The last line has no newline, so its backslash ends the file.
	content := "select 1;\n\\dt\nselect 2;\n\\x\n\\x 1\n\\x4g\na\\\nb\\"
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	h := &history{}
	if err := h.loadFrom(name, 16); err != nil {
		t.Fatalf("loading gave %v, want no error", err)
	}
	want := []string{"select 1;", `\dt`, "select 2;", `\x`, `\x 1`, `\x4g`, `a\`, `b\`}
	if !equalStrings(h.entries, want) {
		t.Errorf("loaded %q, want %q", h.entries, want)
	}
	h.push("select 3;")
	if err := h.save(); err != nil {
		t.Fatalf("saving gave %v, want no error", err)
	}
	back := &history{}
	if err := back.loadFrom(name, 16); err != nil {
		t.Fatalf("loading again gave %v, want no error", err)
	}
	if want := slices.Concat(want, []string{"select 3;"}); !equalStrings(back.entries, want) {
		t.Errorf("after a save the file holds %q, want %q", back.entries, want)
	}
}

// TestHistoryReadsAnEscapeCutShortByTheEnd checks that an escape that the end
// of the file cuts short is kept as text, as one that a newline cuts short is
// (D67).
func TestHistoryReadsAnEscapeCutShortByTheEnd(t *testing.T) {
	t.Parallel()
	for _, line := range []string{`a\`, `a\x`, `a\x4`} {
		h := newTestHistory(8, true)
		var buf text.Buffer
		h.readEntry(bufio.NewReader(strings.NewReader(line)), &buf)
		if got, ok := h.get(0); !ok || h.count() != 1 || got != line {
			t.Errorf("reading %q with no newline after it gave %q, want the line as it is", line, h.entries)
		}
	}
}

// TestHistoryErrReportsTheLastReadOrWrite checks that HistoryErr holds the
// error of the last load or save, and goes back to nil when one works (D67).
func TestHistoryErrReportsTheLastReadOrWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &history{}
	s := &Session{env: &env{history: h}}
	if err := s.HistoryErr(); err != nil {
		t.Fatalf("a history that was never read reports %v", err)
	}
	_ = h.loadFrom(filepath.Join(dir, "no-such-directory", "h.txt"), 8)
	if err := s.HistoryErr(); err != nil {
		t.Fatalf("a file that is not there yet reports %v, want nil", err)
	}
	h.push("select 1;")
	saveErr := h.save()
	if saveErr == nil {
		t.Fatal("saving into a directory that is not there gave no error")
	}
	if err := s.HistoryErr(); !errors.Is(err, saveErr) {
		t.Errorf("after a failed save HistoryErr is %v, want %v", err, saveErr)
	}
	_ = h.loadFrom(filepath.Join(dir, "h.txt"), 8)
	h.push("select 1;")
	if err := h.save(); err != nil {
		t.Fatalf("saving: %v", err)
	}
	if err := s.HistoryErr(); err != nil {
		t.Errorf("after a save that worked HistoryErr is %v, want nil", err)
	}
	_ = h.loadFrom(filepath.Join(dir, "a\x00b.txt"), 8)
	if err := s.HistoryErr(); err == nil {
		t.Error("after a load that failed HistoryErr is nil")
	}
	// A history that is turned off reads nothing, so nothing went wrong.
	_ = h.loadFrom(filepath.Join(dir, "a\x00b.txt"), 0)
	if err := s.HistoryErr(); err != nil {
		t.Errorf("after the history was turned off HistoryErr is %v, want nil", err)
	}
}

// TestHistoryKeepsMoreThanTheC checks that the list holds what it is asked to,
// and is not held at the 200 lines of the C, and that the default is the 500
// lines that psql keeps (D67).
func TestHistoryKeepsMoreThanTheC(t *testing.T) {
	t.Parallel()
	if DefaultHistoryEntries != 500 {
		t.Errorf("DefaultHistoryEntries is %d, want 500, which psql keeps", DefaultHistoryEntries)
	}
	for _, limit := range []int{-1, 1000} {
		h := &history{}
		_ = h.loadFrom("", limit)
		for i := range 2000 {
			h.push(fmt.Sprintf("select %d;", i))
		}
		want := limit
		if limit < 0 {
			want = DefaultHistoryEntries
		}
		if h.count() != want {
			t.Errorf("with a limit of %d the list holds %d entries, want %d", limit, h.count(), want)
		}
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

// TestHistoryPushRemovesEveryCopy checks that adding an entry takes out every
// earlier copy of it, two side by side included, where the C took out only
// the first of two neighbours (D68).
func TestHistoryPushRemovesEveryCopy(t *testing.T) {
	t.Parallel()
	h := newTestHistory(8, true)
	for _, e := range []string{"a", "a", "b", "a", "a"} {
		h.push(e)
	}
	h.enableDuplicates(false)
	h.push("a")
	if want := []string{"b", "a"}; !equalStrings(h.entries, want) {
		t.Errorf("the list holds %q, want %q", h.entries, want)
	}
}
