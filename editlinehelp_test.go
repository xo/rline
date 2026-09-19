package rline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// helpProbePath is where tools/build-probe-help.sh puts the probe, and
// helpDefaultProbePath the second one it builds on macOS, which has the
// branch turned off so that the other variant can be recorded from here too.
const (
	helpProbePath        = ".build/probe-help"
	helpDefaultProbePath = ".build/probe-help-default"
)

// helpCorpusPath is where the lines the C help screen is built from are kept
// for this build.
//
// Three rows of the help name a different key on macOS, and one of them is
// two rows there rather than one, so the recording is per branch in the same
// way the redraw is. corpusVariant comes from the same place, beside the wrap
// mark, so the two cannot disagree about which branch this is.
func helpCorpusPath() string {
	return filepath.Join("testdata", corpusVariant, "help.txt")
}

// TestHelpMatchesC checks the help screen against the C, line by line.
//
// It is a lot of literal text, and a mistyped key name or a missing space
// would never show up anywhere else, because nothing else reads it and no
// recorded session presses F1. The markup is compared as it is handed to the
// printer rather than as it comes out, since rendering it is bbcode's job
// and has its own corpus.
func TestHelpMatchesC(t *testing.T) {
	if *update {
		regenerateHelp(t)
	}
	b, err := os.ReadFile(helpCorpusPath())
	if err != nil {
		t.Fatalf("reading %s: %v\nThis build has no recording for its branch. Record one with\n  ./tools/build-probe-help.sh && go test . -run TestHelpMatchesC -update",
			helpCorpusPath(), err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")

	var wantBanner string
	want := make([]string, 0, len(lines))
	for i, line := range lines {
		kind, body, found := strings.Cut(line, " ")
		if !found {
			t.Fatalf("%s:%d: cannot read %q", helpCorpusPath(), i+1, line)
		}
		switch kind {
		case "banner":
			wantBanner = string(mustHex(t, body))
		case "row":
			want = append(want, string(mustHex(t, body)))
		default:
			t.Fatalf("%s:%d: unknown kind %q", helpCorpusPath(), i+1, kind)
		}
	}
	if wantBanner == "" {
		t.Fatal("the corpus holds no banner")
	}
	if len(want) < 40 {
		t.Fatalf("the corpus holds %d rows, which is too few", len(want))
	}

	if got := helpBanner(); got != wantBanner {
		t.Errorf("the banner differs.\n got %q\nwant %q", got, wantBanner)
	}
	got := helpLines()
	if len(got) != len(want) {
		t.Fatalf("the help has %d rows, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("row %d differs.\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

// regenerateHelp runs the C probe and writes the corpus for this branch.
//
// On macOS it writes the other branch as well, from the second probe that is
// built with the branch turned off. The help table is the only thing that
// probe reads, and nothing else in the C reaches it, so turning the branch
// off gives exactly the table the other build has. That is why one machine
// can record both, unlike the redraw, where the recording comes from running
// the whole editor.
func regenerateHelp(t *testing.T) {
	t.Helper()
	record(t, helpProbePath, helpCorpusPath())
	if _, err := os.Stat(helpDefaultProbePath); err == nil {
		record(t, helpDefaultProbePath, filepath.Join("testdata", "default", "help.txt"))
	}
}

// record runs a probe and writes what it printed.
func record(t *testing.T, probe, path string) {
	t.Helper()
	if _, err := os.Stat(probe); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-help.sh", probe)
	}
	out, err := exec.Command(probe).Output()
	if err != nil {
		t.Fatalf("running %s: %v", probe, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	t.Logf("wrote %s: %d bytes", path, len(out))
}
