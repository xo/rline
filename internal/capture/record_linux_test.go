//go:build linux

package capture

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update rewrites the golden files instead of comparing against them.
var update = flag.Bool("update", false, "rewrite the golden files")

// demoPath is where tools/build-demo.sh puts the compiled isocline demo.
const demoPath = "../../.build/example"

// TestRecord records every session and compares it against its golden file.
func TestRecord(t *testing.T) {
	if _, err := os.Stat(demoPath); err != nil {
		t.Skipf("no demo at %s: run tools/build-demo.sh", demoPath)
	}
	for _, session := range Sessions {
		t.Run(session.Name, func(t *testing.T) {
			tr, err := Record(context.Background(), demoPath, session)
			if err != nil {
				t.Fatalf("recording: %v", err)
			}
			if len(tr.Bytes()) == 0 {
				t.Fatal("the program wrote nothing")
			}
			golden := filepath.Join("testdata", session.Name+".txt")
			got := tr.Encode()
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatalf("writing %s: %v", golden, err)
				}
				t.Logf("wrote %s: %d exchanges, %d bytes", golden, len(tr.Exchanges), len(tr.Bytes()))
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("reading %s: %v (run go test ./internal/capture -update)", golden, err)
			}
			if got == string(want) {
				return
			}
			line, gotLine, wantLine := firstDiff(got, string(want))
			t.Errorf("recording does not match %s\nfirst difference on line %d\n got: %s\nwant: %s",
				golden, line, gotLine, wantLine)
		})
	}
}

// TestRecordIsStable records a session twice and checks that both runs agree.
// A recording that changes between runs cannot prove anything about the port.
func TestRecordIsStable(t *testing.T) {
	if _, err := os.Stat(demoPath); err != nil {
		t.Skipf("no demo at %s: run tools/build-demo.sh", demoPath)
	}
	session := Sessions[0]
	first, err := Record(context.Background(), demoPath, session)
	if err != nil {
		t.Fatalf("first recording: %v", err)
	}
	second, err := Record(context.Background(), demoPath, session)
	if err != nil {
		t.Fatalf("second recording: %v", err)
	}
	if first.Encode() != second.Encode() {
		line, a, b := firstDiff(first.Encode(), second.Encode())
		t.Errorf("two recordings of %s differ on line %d\nfirst:  %s\nsecond: %s",
			session.Name, line, a, b)
	}
}

func TestRecordNoSteps(t *testing.T) {
	t.Parallel()
	_, err := Record(context.Background(), demoPath, Session{Name: "empty"})
	if !errors.Is(err, ErrNoSteps) {
		t.Errorf("Record with no steps returned %v, want %v", err, ErrNoSteps)
	}
}

// firstDiff returns the number of the first line that differs, and that line
// from each text. The number counts from one.
func firstDiff(got, want string) (int, string, string) {
	a, b := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range max(len(a), len(b)) {
		x, y := at(a, i), at(b, i)
		if x != y {
			return i + 1, x, y
		}
	}
	return 0, "", ""
}

// at returns the line at index i, or a marker when the text ended.
func at(lines []string, i int) string {
	if i >= len(lines) {
		return "<end of text>"
	}
	return lines[i]
}
