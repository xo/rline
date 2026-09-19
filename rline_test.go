package rline

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/rline/internal/capture"
)

// --------------------------------------------------------------------------
// api_test.go

// TestReadLineWithoutTerminal checks the path taken when there is nothing to
// edit on, which is what a program reading a script or a pipe gets.
func TestReadLineWithoutTerminal(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	r := &Session{
		noEdit: true,
		plain:  bufio.NewReader(strings.NewReader("first\nsecond\r\nlast")),
	}
	for _, want := range []string{"first", "second", "last"} {
		got, err := r.ReadLine("> ")
		if err != nil {
			t.Fatalf("ReadLine: %v", err)
		}
		if got != want {
			t.Errorf("ReadLine gave %q, want %q", got, want)
		}
	}
	// The reader has run out, so the next read ends it.
	if _, err := r.ReadLine("> "); !errors.Is(err, io.EOF) {
		t.Errorf("ReadLine at the end gave %v, want %v", err, io.EOF)
	}
	if out.Len() != 0 {
		t.Errorf("a reader with no terminal wrote %q", out.String())
	}
}

// TestReadLineAfterClose checks that a closed reader says so rather than
// touching a terminal it has already put back.
func TestReadLineAfterClose(t *testing.T) {
	t.Parallel()
	r := &Session{noEdit: true, plain: bufio.NewReader(strings.NewReader("x\n"))}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("closing twice: %v", err)
	}
	if _, err := r.ReadLine("> "); !errors.Is(err, ErrClosed) {
		t.Errorf("ReadLine after Close gave %v, want %v", err, ErrClosed)
	}
}

// TestOptions checks that each option reaches the setting it names.
func TestOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opt  Option
		want func(*config) bool
	}{
		{"prompt", WithPrompt("$ ", ""), func(c *config) bool {
			return c.promptMarker == "$ " && c.cpromptMarker == "$ "
		}},
		{"prompt with continuation", WithPrompt("$ ", "| "), func(c *config) bool {
			return c.promptMarker == "$ " && c.cpromptMarker == "| "
		}},
		{"history file", WithHistoryFile("h.txt"), func(c *config) bool {
			return c.historyFile == "h.txt"
		}},
		{"history limit", WithHistoryLimit(12), func(c *config) bool {
			return c.historyEntries == 12
		}},
		{"history off", WithHistory(false), func(c *config) bool { return c.historyEntries == 0 }},
		{"history on", WithHistory(true), func(c *config) bool {
			return c.historyEntries == DefaultHistoryEntries
		}},
		{"no color", WithColor(false), func(c *config) bool { return c.noColor }},
		{"color", WithColor(true), func(c *config) bool { return !c.noColor }},
		{"no beep", WithBeep(false), func(c *config) bool { return c.silent }},
		{"single line", WithMultiline(false), func(c *config) bool { return c.singlelineOnly }},
		{"no highlighting", WithHighlighting(false), func(c *config) bool { return c.noHighlight }},
		{"no brace matching", WithBraceMatching(false), func(c *config) bool { return c.noBraceMatch }},
		{"no brace insertion", WithBraceInsertion(false), func(c *config) bool { return c.opts.NoAutoBrace }},
		{"no hints", WithHints(false), func(c *config) bool { return c.noHint }},
		{"no inline help", WithInlineHelp(false), func(c *config) bool { return c.noHelp }},
		{"no indent", WithMultilineIndent(false), func(c *config) bool { return c.noMultilineIndent }},
		{"auto tab", WithAutoTab(true), func(c *config) bool { return c.completeAutoTab }},
		{"hint delay", WithHintDelay(time.Second), func(c *config) bool { return c.hintDelay == time.Second }},
		{"match braces", WithMatchBraces("<>"), func(c *config) bool { return c.opts.MatchBraces == "<>" }},
		{"auto braces", WithAutoBraces("<>"), func(c *config) bool { return c.opts.AutoBraces == "<>" }},
		{"input fd", WithInputFd(3), func(c *config) bool { return c.inFd == 3 }},
		{"input reader", WithInput(strings.NewReader("x")), func(c *config) bool {
			return c.in != nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := &config{}
			test.opt(c)
			if !test.want(c) {
				t.Errorf("%s did not reach its setting", test.name)
			}
		})
	}
}

// TestDefaultsMatchTheC checks the settings a Session starts with against what
// isocline starts with, since a caller who passes no options should get what
// the C gives.
func TestDefaultsMatchTheC(t *testing.T) {
	t.Parallel()
	if DefaultPromptMarker != "> " {
		t.Errorf("the prompt marker is %q, want %q", DefaultPromptMarker, "> ")
	}
	if DefaultMatchBraces != "()[]{}" {
		t.Errorf("the matched braces are %q", DefaultMatchBraces)
	}
	if DefaultAutoBraces != `()[]{}""''` {
		t.Errorf("the inserted braces are %q", DefaultAutoBraces)
	}
	if DefaultHintDelay != 400*time.Millisecond {
		t.Errorf("the hint delay is %v, want 400ms", DefaultHintDelay)
	}
	if DefaultMultilineEOL != '\\' {
		t.Errorf("the line continuation is %q", DefaultMultilineEOL)
	}
}

// TestDefaultStylesAreDefined checks that every style the editor draws with is
// defined, so that nothing falls back to no attributes at all.
func TestDefaultStylesAreDefined(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	tm := newTerm(&out, termOptions{})
	bb := newBBCode(tm)
	for _, s := range defaultStyles {
		bb.styleDef(s[0], s[1])
	}
	for _, name := range []string{
		"ic-prompt", "ic-info", "ic-diminish", "ic-emphasis",
		"ic-hint", "ic-error", "ic-bracematch",
	} {
		if bb.style(name) == (attr{}) {
			t.Errorf("the style %q sets nothing", name)
		}
	}
}

// TestWritesToTerminalLooksAtTheWriter checks that a plain file is not taken
// for a terminal, whatever is on the standard input.
//
// This is where colour is decided, so getting it wrong writes escape
// sequences into a redirected file. It was wrong on Windows, where the
// question was answered about the standard input rather than about the
// writer, and that answer is only wrong while a console is on the standard
// input at the same time.
//
// So read the log line before trusting this test on Windows. The go tool
// hands the test binary a null standard input whatever window it was started
// from, so an ordinary run takes the branch where there is no console, and
// there this would have passed against the bug it was written to catch. A
// false there means the run proved nothing, not that nothing was wrong.
// TestConsoleWritesToTerminalOnAConsole in console_windows_test.go is the
// same check where it can fail, with the recipe for running it. On Unix this
// one is a real check, because isATTY there answers about the descriptor it
// is given.
func TestWritesToTerminalLooksAtTheWriter(t *testing.T) {
	t.Parallel()
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatalf("making a file: %v", err)
	}
	defer func() { _ = f.Close() }()
	t.Logf("the standard input is a terminal: %v", isATTY(int(os.Stdin.Fd())))
	if writesToTerminal(f) {
		t.Errorf("a plain file is taken for a terminal, so colour would be written into it")
	}
	// A writer that is not a file is the caller's own, and is left alone.
	if !writesToTerminal(&bytes.Buffer{}) {
		t.Error("a writer that is not a file is not taken for a terminal")
	}
}

// --------------------------------------------------------------------------
// example_test.go

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

// TestExamplePromptFollowsTheStatement checks which marker is drawn as a
// statement is built up.
//
// A statement now lives in one buffer across as many rows as it takes, so the
// markers cannot be read as a sequence: every redraw draws every row, the
// first with one marker and the rest with the other. What holds is which
// markers appear at all.
func TestExamplePromptFollowsTheStatement(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	for _, test := range []struct {
		name          string
		steps         []capture.Step
		wantCont      bool
		wantStatement string
	}{
		{
			name:     "a statement finished on one row never continues",
			steps:    []capture.Step{{Send: "select 1;" + capture.KeyEnter}},
			wantCont: false, wantStatement: "ran 1 line(s): select 1;",
		},
		{
			name: "an unfinished statement carries on to another row",
			steps: []capture.Step{
				{Send: "select 1" + capture.KeyEnter},
				{Send: "from t;" + capture.KeyEnter},
			},
			wantCont: true, wantStatement: "ran 2 line(s): select 1 from t;",
		},
		{
			name:     "an empty line starts over rather than continuing",
			steps:    []capture.Step{{Send: capture.KeyEnter}, {Send: capture.KeyEnter}},
			wantCont: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tr, err := capture.Record(context.Background(), bin, capture.Session{
				Name:  "prompts",
				About: test.name,
				Steps: append(test.steps, capture.Step{
					Send: `\q` + capture.KeyEnter, Wait: 400 * time.Millisecond,
				}),
			})
			if err != nil {
				t.Skipf("cannot record on this system: %v", err)
			}
			plain := stripEscapes(string(tr.Bytes()))
			if got := strings.Contains(plain, "...> "); got != test.wantCont {
				t.Errorf("the continuation marker appeared: %v, want %v", got, test.wantCont)
			}
			if !strings.Contains(plain, "sql> ") {
				t.Error("the first marker was never drawn")
			}
			if test.wantStatement != "" && !strings.Contains(plain, test.wantStatement) {
				t.Errorf("the run never showed %q.\nWhat it wrote:\n%s", test.wantStatement, plain)
			}
		})
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

// TestExampleArrowsMoveBetweenRows checks that up and down move the cursor
// between the rows of one statement.
//
// This is what the caller buys by saying a line is unfinished rather than
// reading each row separately: the rows are one buffer, so the cursor can go
// between them and the whole thing comes back at once.
func TestExampleArrowsMoveBetweenRows(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	tr, err := capture.Record(context.Background(), bin, capture.Session{
		Name:  "arrows",
		About: "up and down between the rows of one statement",
		Steps: []capture.Step{
			{Send: "select 1" + capture.KeyEnter},
			{Send: "from t" + capture.KeyEnter},
			{Send: capture.KeyUp},   // onto the second row
			{Send: "X"},             // marks which row the cursor was on
			{Send: capture.KeyDown}, // back to the third
			{Send: ";" + capture.KeyEnter, Wait: 500 * time.Millisecond},
			{Send: `\q` + capture.KeyEnter, Wait: 300 * time.Millisecond},
		},
	})
	if err != nil {
		t.Skipf("cannot record on this system: %v", err)
	}
	plain := stripEscapes(string(tr.Bytes()))
	// Up put the cursor at the start of the second row, so the mark landed in
	// front of "from". Down then put it back on the third, where the semicolon
	// went.
	want := "ran 3 line(s): select 1 Xfrom t ;"
	if !strings.Contains(plain, want) {
		t.Errorf("the run never showed %q.\nWhat it wrote:\n%s", want, plain)
	}
}

// TestExamplePasswordIsHidden checks that Password shows nothing, records
// nothing, and still edits.
//
// The check that matters is the first: the typed text must not appear anywhere
// in what the terminal was sent, because a password that reaches the screen is
// worse than one that is not read at all.
func TestExamplePasswordIsHidden(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	tr, err := capture.Record(context.Background(), bin, capture.Session{
		Name:  "password",
		About: "a password is read without being shown",
		Steps: []capture.Step{
			{Send: `\pass` + capture.KeyEnter},
			{Send: "hunter2"},
			{Send: capture.KeyBackspace}, // proves editing works while hidden
			{Send: "3" + capture.KeyEnter, Wait: 500 * time.Millisecond},
			{Send: `\q` + capture.KeyEnter, Wait: 300 * time.Millisecond},
		},
	})
	if err != nil {
		t.Skipf("cannot record on this system: %v", err)
	}
	raw := string(tr.Bytes())
	plain := stripEscapes(raw)
	// The example echoes the password back on purpose, so that what Password
	// collected can be checked against what was typed. That echo is the only
	// place it may appear: while it was being typed the editor redraws the
	// line on every key, so a password that was not hidden would appear many
	// times over.
	if n := strings.Count(raw, "hunter3"); n != 1 {
		t.Errorf("the password appears %d times in what the terminal was sent, want 1", n)
	}
	for _, partial := range []string{"hunter2", "hunte\x1b", "unter2"} {
		if strings.Contains(raw, partial) {
			t.Errorf("the password text %q reached the terminal while it was typed", partial)
		}
	}
	if !strings.Contains(plain, "password: ") {
		t.Error("the password prompt was never drawn")
	}
	// "hunter2" with the 2 taken off and a 3 put on, so the backspace was
	// acted on rather than ignored or shown.
	if !strings.Contains(plain, `password was "hunter3" (7 bytes)`) {
		t.Errorf("the password did not come back as expected.\nWhat it wrote:\n%s", plain)
	}
}

// TestExampleWalksTheHistory checks that the arrow keys step through what has
// been entered, and that a line picked out of the history runs.
func TestExampleWalksTheHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	tr, err := capture.Record(context.Background(), bin, capture.Session{
		Name:  "history",
		About: "walking back and forward through the history",
		Steps: []capture.Step{
			{Send: "select alpha;" + capture.KeyEnter},
			{Send: "select beta;" + capture.KeyEnter},
			{Send: capture.KeyUp, Wait: 400 * time.Millisecond},   // beta
			{Send: capture.KeyUp, Wait: 400 * time.Millisecond},   // alpha
			{Send: capture.KeyDown, Wait: 400 * time.Millisecond}, // beta again
			{Send: capture.KeyEnter, Wait: 400 * time.Millisecond},
			{Send: `\q` + capture.KeyEnter, Wait: 300 * time.Millisecond},
		},
	})
	if err != nil {
		t.Skipf("cannot record on this system: %v", err)
	}
	var ran []string
	for _, ln := range strings.Split(stripEscapes(string(tr.Bytes())), "\n") {
		if ln = strings.Trim(ln, "\r "); strings.HasPrefix(ln, "ran ") {
			ran = append(ran, ln)
		}
	}
	// Two statements were typed, and the third came back out of the history:
	// two steps back then one forward lands on the newer of the two.
	want := []string{
		"ran 1 line(s): select alpha;",
		"ran 1 line(s): select beta;",
		"ran 1 line(s): select beta;",
	}
	if strings.Join(ran, "|") != strings.Join(want, "|") {
		t.Errorf("the statements that ran were %v, want %v", ran, want)
	}
}

// TestExampleSearchesTheHistory checks the incremental search that Ctrl-R
// opens: it draws its own prompt, narrows as more is typed, and the entry it
// found becomes the line.
func TestExampleSearchesTheHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	tr, err := capture.Record(context.Background(), bin, capture.Session{
		Name:  "history search",
		About: "finding an older entry by typing part of it",
		Steps: []capture.Step{
			{Send: "select alpha;" + capture.KeyEnter},
			{Send: "select beta;" + capture.KeyEnter},
			{Send: capture.CtrlR, Wait: 400 * time.Millisecond},
			// "alp" appears only in the older entry, so the search has to walk
			// past the newer one to find it.
			{Send: "alp", Wait: 400 * time.Millisecond},
			{Send: capture.KeyEnter, Wait: 400 * time.Millisecond}, // take it
			{Send: capture.KeyEnter, Wait: 400 * time.Millisecond}, // run it
			{Send: `\q` + capture.KeyEnter, Wait: 300 * time.Millisecond},
		},
	})
	if err != nil {
		t.Skipf("cannot record on this system: %v", err)
	}
	plain := stripEscapes(string(tr.Bytes()))
	if !strings.Contains(plain, "history search") {
		t.Error("the search prompt was never drawn")
	}
	if !strings.Contains(plain, "use tab for the next match") {
		t.Error("the reminder below the line was never drawn")
	}
	// The entry the search found is the one that ran, not the newer one.
	last := ""
	for _, ln := range strings.Split(plain, "\n") {
		if ln = strings.Trim(ln, "\r "); strings.HasPrefix(ln, "ran ") {
			last = ln
		}
	}
	if last != "ran 1 line(s): select alpha;" {
		t.Errorf("the search ran %q, want the entry it found", last)
	}
}

// TestExampleCompletionMenuColumns checks the two column layouts the menu
// chooses between, which depend on how wide the entries are rather than on how
// many there are.
//
// Tab completes the longest start every answer shares before it offers a menu,
// so the prefix here is chosen for what is left afterwards: "c" leaves fifteen
// short words, and "co" leaves six longer ones.
func TestExampleCompletionMenuColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	for _, test := range []struct {
		name    string
		prefix  string
		wantRow string
		wantAll string
	}{
		{
			name:    "three columns",
			prefix:  "c",
			wantRow: " 1 case      4 count     7 commit ",
			wantAll: "page-down",
		},
		{
			name:   "two columns",
			prefix: "co",
			// Longer words leave room for two columns rather than three.
			wantRow: " 1 count        4 collate",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tr, err := capture.Record(context.Background(), bin, capture.Session{
				Name:  test.name,
				About: test.name,
				Cols:  80,
				Rows:  24,
				Steps: []capture.Step{
					{Send: test.prefix},
					{Send: capture.KeyTab, Wait: 600 * time.Millisecond},
					{Send: capture.KeyEscape, Wait: 200 * time.Millisecond},
					{Send: capture.CtrlU},
					{Send: `\q` + capture.KeyEnter, Wait: 300 * time.Millisecond},
				},
			})
			if err != nil {
				t.Skipf("cannot record on this system: %v", err)
			}
			plain := stripEscapes(string(tr.Bytes()))
			if !strings.Contains(plain, test.wantRow) {
				t.Errorf("the menu never drew %q.\nWhat it wrote:\n%s", test.wantRow, plain)
			}
			if test.wantAll != "" && !strings.Contains(plain, test.wantAll) {
				t.Errorf("the menu never offered %q", test.wantAll)
			}
		})
	}
}

// TestExampleNoHintInsideAWord checks that nothing is suggested while the
// cursor sits inside a word.
//
// A hint is only ever the rest of a single answer, and the answer is worked
// out from what is in front of the cursor. With the cursor after "wh" in
// "where", the answer is "where" and the hint is "re", which is drawn between
// the cursor and the "ere" already there: the line reads "whereere" with two
// letters in the hint color. Taking it would produce that text for real.
//
// The completer is what fixes this, not the editor. It is handed the whole
// line and the cursor, so it can see that a word follows and say nothing.
func TestExampleNoHintInsideAWord(t *testing.T) {
	if testing.Short() {
		t.Skip("building the example takes a moment")
	}
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	tr, err := capture.Record(context.Background(), bin, capture.Session{
		Name:  "hint inside a word",
		About: "typing into the middle of a word suggests nothing",
		Steps: []capture.Step{
			{Send: "where", Wait: 600 * time.Millisecond},
			{Send: capture.KeyLeft},
			{Send: capture.KeyLeft},
			{Send: capture.KeyLeft, Wait: 600 * time.Millisecond},
			{Send: "e", Wait: 600 * time.Millisecond},
			{Send: "r", Wait: 800 * time.Millisecond},
			{Send: capture.CtrlU},
			{Send: `\q` + capture.KeyEnter, Wait: 300 * time.Millisecond},
		},
	})
	if err != nil {
		t.Skipf("cannot record on this system: %v", err)
	}
	// Only the redraws of the line itself, because the banner is written in
	// the same color a hint uses.
	var drawn []string
	for _, ln := range strings.Split(capture.Escape(tr.Bytes()), "\n") {
		if strings.Contains(ln, "wh") {
			drawn = append(drawn, ln)
		}
	}
	joined := strings.Join(drawn, "\n")
	if strings.Contains(joined, `\e[90m`) {
		t.Errorf("something was suggested inside a word.\nThe line was drawn as:\n%s", joined)
	}
	// What was typed is what is drawn, with nothing extra between.
	if !strings.Contains(joined, "wherere") {
		t.Errorf("the line was never drawn as typed.\nThe line was drawn as:\n%s", joined)
	}
}

// TestMarkupWriterKeepsANewlineOutOfTheColour checks that a newline written
// through the markup writer is not drawn inside whatever attributes the
// markup left open.
//
// The drawing writes each run of text with the attributes that run carries
// and resets only afterwards, so a newline inside the last run goes out while
// those attributes are still set. With a background colour left open that
// fills the rest of the row. Println used to write the newline separately,
// after the reset; fmt.Fprintln hands it over as part of the string, so it
// has to be taken off again.
//
// The expectations are the bytes written out, rather than a comparison with
// another code path, so that this still says what is wanted if that path
// changes.
func TestMarkupWriterKeepsANewlineOutOfTheColour(t *testing.T) {
	restore := saveEnv(t)
	defer restore()
	setTermEnv("", "xterm-256color", "")

	markupEnv := func() (*env, *bytes.Buffer) {
		var sink bytes.Buffer
		tm := newTerm(&sink, termOptions{Sizer: fixedSize{cols: 40, rows: 6}})
		bb := newBBCode(tm)
		for _, s := range defaultStyles {
			bb.styleDef(s[0], s[1])
		}
		return &env{term: tm, bb: bb}, &sink
	}

	for _, test := range []struct {
		name string
		in   string
		want string
	}{
		{
			// Nothing open, so nothing to get out of.
			name: "plain text",
			in:   "plain",
			want: "\x1b[m" + "plain" + "\n",
		},
		{
			// Closed markup resets before the newline either way.
			name: "markup that closes itself",
			in:   "[ic-info]closed[/]",
			want: "\x1b[m" + "\x1b[90m" + "closed" + "\x1b[39m" + "\n",
		},
		{
			// The reset has to come first. With the newline inside the run
			// this reads "...open\n\x1b[39m".
			name: "a colour left open",
			in:   "[ic-info]left open",
			want: "\x1b[m" + "\x1b[90m" + "left open" + "\x1b[39m" + "\n",
		},
		{
			// The one that shows on screen rather than only in the bytes: a
			// background still set when the row ends fills the rest of it.
			name: "a background left open",
			in:   "[bgcolor=navy]background left open",
			want: "\x1b[m" + "\x1b[48;5;18m" + "background left open" + "\x1b[49m" + "\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ev, sink := markupEnv()
			if _, err := fmt.Fprintln(&MarkupWriter{env: ev}, test.in); err != nil {
				t.Fatalf("Fprintln gave %v", err)
			}
			ev.term.flush()
			if got := sink.String(); got != test.want {
				t.Errorf("Write gave %q, want %q", got, test.want)
			}

			// WriteString is the same promise by a shorter route, and is
			// checked separately because it is a second copy of the rule:
			// reverting it alone went unnoticed until this was added.
			ev2, sink2 := markupEnv()
			if _, err := (&MarkupWriter{env: ev2}).WriteString(test.in + "\n"); err != nil {
				t.Fatalf("WriteString gave %v", err)
			}
			ev2.term.flush()
			if got := sink2.String(); got != test.want {
				t.Errorf("WriteString gave %q, want %q", got, test.want)
			}
		})
	}
}

// TestMarkupWriterWithNowhereToWrite checks the promise that a MarkupWriter with no
// terminal behind it throws away what it is given rather than failing, which
// is what lets a caller use Markup without asking whether there is one.
//
// A short write would make fmt report an error, so the count matters as much
// as the absence of a panic.
func TestMarkupWriterWithNowhereToWrite(t *testing.T) {
	t.Parallel()
	for _, w := range []*MarkupWriter{nil, {}, {env: nil}} {
		n, err := fmt.Fprintf(w, "[ic-error]%s[/]\n", "message")
		if err != nil {
			t.Errorf("writing to a writer with nowhere to write gave %v", err)
		}
		if want := len("[ic-error]message[/]\n"); n != want {
			t.Errorf("reported %d bytes written, want %d", n, want)
		}
		if _, err := w.WriteString("more\n"); err != nil {
			t.Errorf("WriteString gave %v", err)
		}
	}
}

// ----------------------------------------------------------------------------
// The history a program can read and write

// TestHistoryIsReadableAndSymmetric checks the four ways a program touches the
// history: adding, reading back, saving and loading.
//
// Reading it back was missing entirely — a program could add, save and clear
// but never look — and saving took no file name while loading took one, so
// which file the history lived in was two settings that could disagree.
func TestHistoryIsReadableAndSymmetric(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fname := filepath.Join(dir, "history.txt")

	h := &history{}
	h.loadFrom(fname, DefaultHistoryEntries)
	r := &Session{env: &env{history: h}}

	for _, line := range []string{"first", "second", "third"} {
		r.AddHistory(line)
	}
	// Newest first, which is the order the up arrow walks.
	if got, want := r.History(), []string{"third", "second", "first"}; !slices.Equal(got, want) {
		t.Errorf("History gave %q, want %q", got, want)
	}

	if err := r.SaveHistory(); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}
	r.ClearHistory()
	if got := r.History(); len(got) != 0 {
		t.Errorf("after ClearHistory the history holds %q", got)
	}

	// Loading takes no file name either, so it reads the one that was saved.
	if err := r.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if got, want := r.History(), []string{"third", "second", "first"}; !slices.Equal(got, want) {
		t.Errorf("after LoadHistory the history holds %q, want %q", got, want)
	}

	// And the file can be changed, which is the reason LoadHistory exists
	// rather than the reason it takes an argument.
	other := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(other, []byte("elsewhere\n"), 0o600); err != nil {
		t.Fatalf("writing the other file: %v", err)
	}
	r.SetHistoryFile(other)
	if err := r.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory after SetHistoryFile: %v", err)
	}
	if got, want := r.History(), []string{"elsewhere"}; !slices.Equal(got, want) {
		t.Errorf("after changing the file the history holds %q, want %q", got, want)
	}
}

// TestHistoryIsACopy checks that changing what History returned does not
// change the history.
func TestHistoryIsACopy(t *testing.T) {
	t.Parallel()
	h := &history{}
	h.loadFrom("", DefaultHistoryEntries)
	r := &Session{env: &env{history: h}}
	r.AddHistory("one")
	r.AddHistory("two")

	got := r.History()
	got[0] = "changed"
	if again := r.History(); again[0] != "two" {
		t.Errorf("changing what History returned changed the history: %q", again)
	}
}

// TestWriteStringMatchesWrite checks that the two ways of writing plain text
// put out the same bytes, on the Session and on the markup MarkupWriter alike.
func TestWriteStringMatchesWrite(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		text string
	}{
		{"plain", "hello\n"},
		{"a bracket is not a tag", "a[b]c\n"},
		{"no line ending", "no ending"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var a, b bytes.Buffer
			ra := &Session{env: &env{term: newTerm(&a, termOptions{NoColor: true})}}
			rb := &Session{env: &env{term: newTerm(&b, termOptions{NoColor: true})}}
			if _, err := ra.Write([]byte(test.text)); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if _, err := rb.WriteString(test.text); err != nil {
				t.Fatalf("WriteString: %v", err)
			}
			if a.String() != b.String() {
				t.Errorf("Write gave %q and WriteString gave %q", a.String(), b.String())
			}
		})
	}
}
