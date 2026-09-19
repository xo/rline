package rline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xo/rline/internal/capture"
)

// TestExampleRunsUnderATerminal drives the example program through a
// pseudo-terminal and checks that it prompts, echoes what is typed, and
// leaves when told to.
//
// This is the only test that runs the package as a program rather than
// checking one part of it against a recording, so it is the first thing that
// would notice the pieces failing to fit together.
func TestExampleRunsUnderATerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	build := exec.Command("go", "build", "-o", bin, "./example")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("the example was not built: %v", err)
	}

	tr, err := capture.Record(context.Background(), bin, capture.Session{
		Name:  "example",
		About: "type a line, see it echoed, then leave",
		// The harness collects the startup output by reading until the
		// program goes quiet, and it cannot tell a program that has finished
		// writing from one that has not started. This program is built
		// moments earlier and started cold, so it writes nothing for longer
		// than the usual 200ms, and the first line of input was going out
		// before it was listening. The terminal echoed that line itself, and
		// raw mode then threw it away, because entering raw mode discards
		// whatever is already waiting. So the session showed "hello" without
		// the program ever having seen it. Waiting longer for the banner is
		// what closes that, and is why this fails on a slower machine before
		// it fails anywhere else.
		Quiet: 1500 * time.Millisecond,
		Steps: []capture.Step{
			{Send: "hello" + capture.KeyEnter},
			{Send: "exit" + capture.KeyEnter, Wait: 500 * time.Millisecond},
		},
	})
	if err != nil {
		t.Skipf("cannot record on this system: %v", err)
	}
	got := string(tr.Bytes())
	plain := stripEscapes(got)

	// The text is checked with the escape sequences taken out, because the
	// markup puts them in the middle of it: the banner really reads
	// "\e[1mrline\e[22m example", so searching the raw bytes for the words
	// next to each other would fail on correct output.
	for _, want := range []string{
		"rline example. Type exit to leave.", // the banner, markup applied
		"> ",                                 // the prompt marker
		"hello",                              // what was typed, drawn back
		"you said: hello",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("the session never showed %q.\nWhat it wrote:\n%s",
				want, capture.Escape([]byte(got)))
		}
	}
	// The markup itself must not reach the terminal.
	for _, unwanted := range []string{"[b]", "[/b]", "[ic-emphasis]"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the markup %q reached the terminal", unwanted)
		}
	}
	// The banner is drawn bold, so the attributes must reach it.
	if !strings.Contains(got, "\x1b[1m") {
		t.Error("the banner was not drawn bold")
	}
}

// stripEscapes removes the escape sequences from s, leaving the text a person
// would see.
func stripEscapes(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			sb.WriteByte(s[i])
			i++
			continue
		}
		// Step over the sequence. A CSI runs to a byte in the range 0x40 to
		// 0x7e, and anything shorter is skipped one byte at a time.
		i++
		if i < len(s) && s[i] == '[' {
			i++
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
		}
		if i < len(s) {
			i++
		}
	}
	return sb.String()
}

// TestExampleWithPipedInput checks the path taken when the input is a pipe.
//
// There is no keyboard to edit on, so lines are read plainly, but there is
// still an output to write to and the program's own output must appear. It
// went missing once: the reader was built without a terminal at all when the
// keyboard could not be opened, so every print silently did nothing while the
// lines were read correctly.
func TestExampleWithPipedInput(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	build := exec.Command("go", "build", "-o", bin, "./example")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader("hello\nworld\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running the example: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		"rline example",   // the banner
		"you said: hello", // the first line, read and written back
		"you said: world", // the second
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the run never showed %q.\nWhat it wrote:\n%s", want, got)
		}
	}
	// Both ends are pipes here, so nothing should be colored and no prompt
	// should be written: there is nobody to prompt, and a prompt would only
	// dirty the output.
	if strings.Contains(got, "\x1b") {
		t.Errorf("an escape sequence reached a pipe:\n%q", got)
	}
	if strings.Contains(got, "> ") {
		t.Errorf("a prompt was written to a pipe:\n%q", got)
	}
}

// exampleBinary returns where to build the example for this system.
//
// Windows needs the suffix. "go build -o" writes exactly the name it is given
// and adds nothing, and Windows decides what is runnable by the extension, so
// a name without one is refused however valid the bytes are.
func exampleBinary(t *testing.T) string {
	t.Helper()
	name := "example"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(t.TempDir(), name)
}
