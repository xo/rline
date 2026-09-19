//go:build linux || darwin

package capture

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// update rewrites the golden files instead of comparing against them.
var update = flag.Bool("update", false, "rewrite the golden files")

// demoPath is where tools/build-demo.sh puts the compiled isocline demo.
const demoPath = "../../.build/example"

// goldenDir returns the directory holding the golden files for the system
// the test is running on.
//
// A recording is only comparable against one made on the same system, because
// isocline itself writes different bytes on each. term_update_ansi16 is
// guarded by "#if __APPLE__", so on macOS the demo asks the terminal for its
// color palette with an OSC 4 sequence and waits for an answer. A bare
// pseudo-terminal never answers, so the demo waits out its timeout, which
// both adds the query to the output and moves the rest of the startup text
// into the next exchange. On Linux the query is not compiled in at all.
//
// So each system keeps its own set, and each one checks bytes. A set that is
// missing is a failure rather than a skip: a skip would put back the hole
// this layout exists to close.
func goldenDir() string {
	return filepath.Join("testdata", runtime.GOOS)
}

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
			golden := filepath.Join(goldenDir(), session.Name+".txt")
			got := tr.Encode()
			if *update {
				if err := os.MkdirAll(goldenDir(), 0o755); err != nil {
					t.Fatalf("making %s: %v", goldenDir(), err)
				}
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatalf("writing %s: %v", golden, err)
				}
				t.Logf("wrote %s: %d exchanges, %d bytes", golden, len(tr.Exchanges), len(tr.Bytes()))
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("reading %s: %v\nThis system has no recording for this session. Record one with\n  go test ./internal/capture -update\nafter building the demo with tools/build-demo.sh.", golden, err)
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

// TestRecordIsStable records every session twice and checks that both runs
// agree. A recording that changes between runs cannot prove anything about
// the port, and a golden file made from one is worse than none.
//
// This used to record only the first session, which is why it did not catch
// the one session that was not stable: completion-menu sends Escape, and the
// demo waits as long to decide what that means as the harness waits for the
// program to go quiet. Recording all of them costs one more pass of about
// twelve seconds and would have caught it at once.
func TestRecordIsStable(t *testing.T) {
	if _, err := os.Stat(demoPath); err != nil {
		t.Skipf("no demo at %s: run tools/build-demo.sh", demoPath)
	}
	for _, session := range Sessions {
		t.Run(session.Name, func(t *testing.T) {
			first, err := Record(context.Background(), demoPath, session)
			if err != nil {
				t.Fatalf("first recording: %v", err)
			}
			second, err := Record(context.Background(), demoPath, session)
			if err != nil {
				t.Fatalf("second recording: %v", err)
			}
			if first.Encode() == second.Encode() {
				return
			}
			line, a, b := firstDiff(first.Encode(), second.Encode())
			t.Errorf("two recordings of %s differ on line %d\nfirst:  %s\nsecond: %s\n"+
				"A session that records differently twice cannot have a golden file. "+
				"Look for a wait in the session that is as long as something the demo waits for.",
				session.Name, line, a, b)
		})
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
