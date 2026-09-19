//go:build linux

package capture

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// update rewrites the golden files instead of comparing against them.
var update = flag.Bool("update", false, "rewrite the golden files")

// demoPath is where the build script puts the compiled isocline demo.
const demoPath = "../../.build/example"

// sessions are the recorded sessions. Each one names a golden file in
// testdata.
var sessions = []Session{
	{
		Name: "basic",
		Steps: []Step{
			{Send: ""},
			{Send: "hello\r"},
			{Send: "exit\r"},
		},
	},
	{
		Name: "completion",
		Steps: []Step{
			{Send: ""},
			{Send: "p\t"},
			{Send: "\r"},
			{Send: "exit\r"},
		},
	},
	{
		Name: "highlight",
		Steps: []Step{
			{Send: ""},
			{Send: "fun"},
			{Send: "\r"},
			{Send: "exit\r"},
		},
	},
}

func TestRecord(t *testing.T) {
	if _, err := os.Stat(demoPath); err != nil {
		t.Skipf("no demo at %s: run tools/build-demo.sh", demoPath)
	}
	for _, session := range sessions {
		t.Run(session.Name, func(t *testing.T) {
			out, err := Record(context.Background(), demoPath, session)
			if err != nil {
				t.Fatalf("recording %s: %v", session.Name, err)
			}
			if len(out) == 0 {
				t.Fatalf("recording %s produced no output", session.Name)
			}
			golden := filepath.Join("testdata", session.Name+".txt")
			got := Escape(out)
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatalf("creating testdata: %v", err)
				}
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatalf("writing %s: %v", golden, err)
				}
				t.Logf("wrote %s, %d bytes recorded", golden, len(out))
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("reading %s: %v (run go test -update)", golden, err)
			}
			if got != string(want) {
				t.Errorf("recording %s does not match %s", session.Name, golden)
			}
		})
	}
}

func TestRecordNoSteps(t *testing.T) {
	t.Parallel()
	if _, err := Record(context.Background(), demoPath, Session{Name: "empty"}); err != ErrNoSteps {
		t.Errorf("Record with no steps returned %v, want %v", err, ErrNoSteps)
	}
}
