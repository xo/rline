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
			{Send: "select 1;" + capture.KeyEnter},
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
		"rline SQL example.", // the banner, markup applied
		"sql> ",              // the prompt marker
		"select 1;",          // what was typed, drawn back as it was typed
		"ran 1 line(s)",      // the statement, echoed plainly
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
	cmd.Stdin = strings.NewReader("select 1;\nselect 2;\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running the example: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		"rline SQL example",        // the banner
		"ran 1 line(s): select 1;", // the first statement
		"ran 1 line(s): select 2;", // the second
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
	if strings.Contains(got, "sql> ") {
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

// TestExampleHighlightsAndSpansLines drives the example through a statement
// written over three lines and checks that the highlighter colored it.
//
// It covers the two things that are hard to see from any single piece of the
// package: input running over more than one line, and attributes changing as
// the line is typed.
func TestExampleHighlightsAndSpansLines(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	tr, err := capture.Record(context.Background(), bin, capture.Session{
		Name:  "sql",
		About: "one statement over three reads, then one over three rows",
		// The same wait the other session needs, and for the same reason:
		// the harness reads until the program goes quiet and cannot tell a
		// program that has finished writing from one that has not started,
		// so a freshly built binary is still starting when the first line
		// goes out. The terminal echoes that line and raw mode then discards
		// it, which shows up here as the first of the three reads going
		// missing. See the comment in TestExampleRunsUnderATerminal.
		Quiet: 1500 * time.Millisecond,
		Steps: []capture.Step{
			// Three reads, joined by the caller at the semicolon.
			{Send: "select id" + capture.KeyEnter},
			{Send: "  from users -- note" + capture.KeyEnter},
			{Send: "  where name = 'bob';" + capture.KeyEnter},
			// One read holding three rows, joined by Ctrl-J.
			{Send: "select 1" + capture.CtrlJ},
			{Send: "  from dual" + capture.CtrlJ},
			{Send: "  where true;" + capture.KeyEnter},
			{Send: "exit" + capture.KeyEnter, Wait: 400 * time.Millisecond},
		},
	})
	if err != nil {
		t.Skipf("cannot record on this system: %v", err)
	}
	raw := string(tr.Bytes())
	plain := stripEscapes(raw)

	// Both statements reached the program whole, three lines each.
	for _, want := range []string{
		"ran 3 line(s): select id   from users -- note   where name = 'bob';",
		"ran 3 line(s): select 1   from dual   where true;",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("the run never showed %q.\nWhat it wrote:\n%s", want, plain)
		}
	}
	// The continuation prompt appeared, so the reader knew it was carrying on.
	if !strings.Contains(plain, "...> ") {
		t.Error("the continuation prompt was never drawn")
	}
	// The highlighter colored the keywords. A keyword is drawn inside a color
	// sequence, so the word appears with one in front of it.
	for _, word := range []string{"select", "from", "where"} {
		if !strings.Contains(raw, "m"+word) {
			t.Errorf("the keyword %q was never drawn with a color in front of it", word)
		}
	}
	// A comment and a string are colored differently from a keyword, so the
	// output holds more than one color.
	colors := map[string]bool{}
	for _, part := range strings.Split(raw, "\x1b[") {
		if i := strings.IndexByte(part, 'm'); i > 0 {
			colors[part[:i]] = true
		}
	}
	if len(colors) < 4 {
		t.Errorf("only %d distinct attributes were used, want several", len(colors))
	}
}

// TestExamplePromptFollowsTheStatement checks which prompt is drawn as a
// statement is built up.
//
// A statement that has no semicolon yet carries on, and the continuation
// marker says so. An empty line with nothing buffered starts over, and the
// first marker comes back.
func TestExamplePromptFollowsTheStatement(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	tr, err := capture.Record(context.Background(), bin, capture.Session{
		Name:  "prompts",
		About: "which prompt is drawn as a statement is built up",
		Steps: []capture.Step{
			{Send: capture.KeyEnter},              // nothing buffered, starts over
			{Send: "select 1" + capture.KeyEnter}, // no semicolon, carries on
			{Send: capture.KeyEnter},              // still carrying on
			{Send: "from t;" + capture.KeyEnter},  // ends it
			{Send: `\q` + capture.KeyEnter, Wait: 400 * time.Millisecond},
		},
	})
	if err != nil {
		t.Skipf("cannot record on this system: %v", err)
	}
	// Reduce the session to the order the markers appeared in.
	var order []string
	for _, ln := range strings.Split(capture.Escape(tr.Bytes()), "\n") {
		var now string
		switch {
		case strings.Contains(ln, "ran "):
			now = "ran"
		case strings.Contains(ln, "...> "):
			now = "...>"
		case strings.Contains(ln, "sql> "):
			now = "sql>"
		}
		if now != "" && (len(order) == 0 || order[len(order)-1] != now) {
			order = append(order, now)
		}
	}
	want := []string{"sql>", "...>", "ran", "sql>"}
	if strings.Join(order, " ") != strings.Join(want, " ") {
		t.Errorf("the prompts went %v, want %v", order, want)
	}
}

// TestExampleQuitsOnALine checks the three ways of leaving, each of which acts
// on the line just typed rather than on the statement being built, so that
// they work part way through an unfinished one.
func TestExampleQuitsOnALine(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	for _, test := range []struct {
		name  string
		input string
		want  []string
		gone  []string
	}{
		{
			name:  `\q at a fresh prompt`,
			input: "select 1;\n\\q\nselect 2;\n",
			want:  []string{"ran 1 line(s): select 1;"},
			gone:  []string{"select 2"},
		},
		{
			name:  `\q part way through a statement`,
			input: "select 1\n\\q\nfrom t;\n",
			want:  []string{},
			gone:  []string{"ran "},
		},
		{
			name:  "exit at a fresh prompt",
			input: "select 1;\nexit\nselect 2;\n",
			want:  []string{"ran 1 line(s): select 1;"},
			gone:  []string{"select 2"},
		},
		{
			name:  "exit part way through a statement",
			input: "select 1\nexit\nfrom t;\n",
			want:  []string{},
			gone:  []string{"ran "},
		},
		{
			name:  "quit part way through a statement",
			input: "select 1\nquit\nfrom t;\n",
			want:  []string{},
			gone:  []string{"ran "},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command(bin)
			cmd.Stdin = strings.NewReader(test.input)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("running the example: %v\n%s", err, out)
			}
			got := string(out)
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Errorf("the run never showed %q.\nWhat it wrote:\n%s", want, got)
				}
			}
			for _, gone := range test.gone {
				if strings.Contains(got, gone) {
					t.Errorf("the run showed %q after leaving.\nWhat it wrote:\n%s", gone, got)
				}
			}
		})
	}
}
