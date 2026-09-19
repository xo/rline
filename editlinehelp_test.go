package rline

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const (
	// helpCorpusPath holds the lines the C help screen is built from.
	helpCorpusPath = "testdata/help.txt"

	// helpProbePath is where tools/build-probe-help.sh puts the probe.
	helpProbePath = ".build/probe-help"
)

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
	b, err := os.ReadFile(helpCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-help.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")

	var wantBanner string
	want := make([]string, 0, len(lines))
	for i, line := range lines {
		kind, body, found := strings.Cut(line, " ")
		if !found {
			t.Fatalf("%s:%d: cannot read %q", helpCorpusPath, i+1, line)
		}
		switch kind {
		case "banner":
			wantBanner = string(mustHex(t, body))
		case "row":
			want = append(want, string(mustHex(t, body)))
		default:
			t.Fatalf("%s:%d: unknown kind %q", helpCorpusPath, i+1, kind)
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

// regenerateHelp runs the C probe and writes the corpus.
func regenerateHelp(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(helpProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-help.sh", helpProbePath)
	}
	out, err := exec.Command(helpProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(helpCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", helpCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", helpCorpusPath, len(out))
}
