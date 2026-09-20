package rline

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/xo/rline/internal/capture"

	"github.com/xo/rline/ansi"
)

// --------------------------------------------------------------------------
// api_test.go

// TestReadLineWithoutTerminal checks the path taken when there is nothing to
// edit on, which is what a program reading a script or a pipe gets.
func TestReadLineWithoutTerminal(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	r := &Session{
		canEdit: false,
		plain:   bufio.NewReader(strings.NewReader("first\nsecond\r\nlast")),
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
	r := &Session{canEdit: false, plain: bufio.NewReader(strings.NewReader("x\n"))}
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
			return c.historyLimit == 12
		}},
		{"history off", WithHistory(false), func(c *config) bool { return c.historyLimit == 0 }},
		{"history on", WithHistory(true), func(c *config) bool {
			return c.historyLimit == DefaultHistoryEntries
		}},
		{"no color", WithColor(false), func(c *config) bool { return !c.color }},
		{"color", WithColor(true), func(c *config) bool { return c.color }},
		{"no beep", WithBeep(false), func(c *config) bool { return !c.beep }},
		{"single line", WithMultiline(false), func(c *config) bool { return !c.multiline }},
		{"no brace matching", WithBraceMatching(false), func(c *config) bool { return !c.braceMatching }},
		{"no brace insertion", WithBraceInsertion(false), func(c *config) bool { return !c.opts.AutoPair }},
		{"no hints", WithHints(false), func(c *config) bool { return !c.hints }},
		{"no inline help", WithInlineHelp(false), func(c *config) bool { return !c.inlineHelp }},
		{"no indent", WithMultilineIndent(false), func(c *config) bool { return !c.multilineIndent }},
		{"auto tab", WithAutoTab(true), func(c *config) bool { return c.autoTab }},
		{"hint delay", WithHintDelay(time.Second), func(c *config) bool { return c.hintDelay == time.Second }},
		{"match braces", WithMatchPairs("<>"), func(c *config) bool { return c.opts.MatchPairs == "<>" }},
		{"auto braces", WithAutoPairs("<>"), func(c *config) bool { return c.opts.AutoPairs == "<>" }},
		{"input reader", WithStdin(strings.NewReader("x")), func(c *config) bool {
			return c.stdin != nil
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
	if DefaultMatchPairs != "()[]{}" {
		t.Errorf("the matched braces are %q", DefaultMatchPairs)
	}
	if DefaultAutoPairs != `()[]{}""''` {
		t.Errorf("the inserted braces are %q", DefaultAutoPairs)
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
	tm := newTerm(&out, termOptions{Color: true})
	bb := newBBCode(tm)
	for _, s := range defaultStyles {
		bb.styleDef(s[0], s[1])
	}
	for _, name := range []string{
		"ic-prompt", "ic-info", "ic-diminish", "ic-emphasis",
		"ic-hint", "ic-error", "ic-bracematch",
	} {
		if bb.style(name) == (ansi.Attr{}) {
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
// one is a real check, because isTerminal there answers about the descriptor it
// is given.
func TestWritesToTerminalLooksAtTheWriter(t *testing.T) {
	t.Parallel()
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatalf("making a file: %v", err)
	}
	defer func() { _ = f.Close() }()
	t.Logf("the standard input is a terminal: %v", isTerminal(int(os.Stdin.Fd())))
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
	// The example keeps its history in the working directory, so it runs in
	// a temporary one. A pipe takes the path with no editing, which saves
	// no history today, but a test that would write into the repository if
	// that changed is a test waiting to make a mess.
	cmd.Dir = t.TempDir()
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
		tm := newTerm(&sink, termOptions{Color: true, Sizer: fixedSize{cols: 40, rows: 6}})
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
	name := filepath.Join(dir, "history.txt")

	h := &history{}
	_ = h.loadFrom(name, DefaultHistoryEntries)
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

// TestSaveHistoryReportsAFailure checks the one error SaveHistory can
// return, which is the file refusing to be written.
//
// The round trip above only ever saves successfully, so nothing exercised
// the failing half: dropping the error and returning nil went unnoticed.
// The C gives up silently here and the port deliberately does not, so the
// error reaching the caller is the promise being made.
func TestSaveHistoryReportsAFailure(t *testing.T) {
	t.Parallel()
	h := &history{}
	// A file inside a directory that does not exist. The load finds nothing
	// and reports nothing, which is the ordinary state of a first run, so
	// the save below fails for the reason this test is about.
	//
	// It used to name a directory as the history file, which failed on
	// every system for a reason that held. It stopped holding when saving
	// began refusing to write over a file it had not read in full: loading
	// a directory fails, so the refusal came first and carried no reason
	// underneath. The construct changed rather than the assertions.
	_ = h.loadFrom(filepath.Join(t.TempDir(), "no-such-directory", "h.txt"), DefaultHistoryEntries)
	r := &Session{env: &env{history: h}}
	r.AddHistory("something to save")

	err := r.SaveHistory()
	if err == nil {
		t.Fatal("saving into a directory reported success")
	}
	// The wrapping has to keep the reason, or the caller learns nothing
	// beyond that something went wrong.
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Errorf("the error is %v, which does not carry the reason underneath", err)
	}
	if !strings.Contains(err.Error(), "saving the history") {
		t.Errorf("the error is %v, which does not say what was being done", err)
	}
}

// TestHistoryIsACopy checks that changing what History returned does not
// change the history.
func TestHistoryIsACopy(t *testing.T) {
	t.Parallel()
	h := &history{}
	_ = h.loadFrom("", DefaultHistoryEntries)
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
			ra := &Session{env: &env{term: newTerm(&a, termOptions{})}}
			rb := &Session{env: &env{term: newTerm(&b, termOptions{})}}
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

// TestLoadHistoryReportsWhatItCannotRead checks that the error LoadHistory
// returns can actually happen.
//
// It could not. history.load swallowed a missing file, an unreadable one and
// a line it could not parse alike, and loadFrom returned nothing, so the
// error was structurally unreachable and a caller checking it had written
// dead code. ken-mba found that while attacking the history API. A method
// that promises an error it cannot deliver is the same species of lie as a
// Reader that does not Read, which is what this whole pass has been about.
func TestLoadHistoryReportsWhatItCannotRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	t.Run("a file that cannot be read is a failure too", func(t *testing.T) {
		t.Parallel()
		// The reading half rather than the opening half. A directory is the
		// only portable way near it: Linux and macOS open one and fail on
		// the first read.
		//
		// NetBSD does not. It opens a directory and reads raw directory data
		// out of it, so both halves succeed and LoadHistory correctly
		// answers nil. That is the second time this test rested on an
		// unchecked claim about a filesystem, after a path through a file
		// read as a missing path on Windows. So the behavior is measured
		// here rather than assumed, and when it is not available the test
		// says what went unchecked instead of passing quietly.
		dir := t.TempDir()
		if f, err := os.Open(dir); err != nil {
			t.Skipf("this system refuses to open a directory (%v), so the reading half "+
				"of history.load is not reached here. It is reached on Linux and macOS, "+
				"and the line that cannot be read is covered everywhere by "+
				"TestHistoryLoadStopsAtABadLine.", err)
		} else {
			_, readErr := f.Read(make([]byte, 64))
			_ = f.Close()
			if readErr == nil {
				t.Skipf("this system reads a directory as data, so opening and reading " +
					"one both succeed and the reading half of history.load is not " +
					"reached here. It is reached on Linux and macOS, and the line that " +
					"cannot be read is covered everywhere by TestHistoryLoadStopsAtABadLine.")
			}
		}

		h := &history{}
		s := &Session{env: &env{history: h}}
		_ = h.loadFrom(dir, DefaultHistoryEntries)
		if err := s.LoadHistory(); err == nil {
			t.Error("LoadHistory on a directory gave no error")
		}
	})

	t.Run("a file that is not there is not a failure", func(t *testing.T) {
		t.Parallel()
		h := &history{}
		s := &Session{env: &env{history: h}}
		_ = h.loadFrom(filepath.Join(dir, "never-written.txt"), DefaultHistoryEntries)
		if err := s.LoadHistory(); err != nil {
			t.Errorf("LoadHistory on a file that does not exist gave %v, want nil", err)
		}
		if got := s.History(); len(got) != 0 {
			t.Errorf("the history holds %q", got)
		}
	})

	t.Run("a path that cannot be opened is", func(t *testing.T) {
		t.Parallel()
		h := &history{}
		s := &Session{env: &env{history: h}}
		// A zero byte cannot appear in a path on any system, and the failure
		// it causes is not a missing file, which is what this needs: the
		// subtest below establishes that a missing file is deliberately not
		// an error, so reaching the opening half needs a path that fails for
		// some other reason.
		//
		// The first version of this used a path through a file, which is
		// ENOTDIR on Unix and reads as a missing path on Windows, so the
		// whole subtest passed a nil error there. windows-vm found it. The
		// comment claimed "every system this builds for" without anyone
		// having asked two of them, which is the same shape as the rest of
		// the list in PLAN.md.
		_ = h.loadFrom(filepath.Join(dir, "a\x00b.txt"), DefaultHistoryEntries)
		err := s.LoadHistory()
		if err == nil {
			t.Fatal("LoadHistory on an unopenable path gave no error")
		}
		// Not a missing file, or this would be testing the case above.
		if errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the error is %v, which reads as a missing file", err)
		}
		// The reason has to survive, so that a caller can tell one failure
		// from another.
		var pathErr *fs.PathError
		if !errors.As(err, &pathErr) {
			t.Errorf("the error is %v, which does not carry an *fs.PathError", err)
		}
		if !strings.Contains(err.Error(), "history file") {
			t.Errorf("the error is %q, which does not say what was being done", err)
		}
	})
}

// ----------------------------------------------------------------------------
// The error stream

// TestStderrGoesWhereItIsToldAndKeepsItsOrder checks the stream a program
// uses for its errors.
//
// The point of it is not that errors can go somewhere else — that is easy —
// but that they keep their order with respect to the output when they do.
// A program writing results to a file and errors to the screen has two
// destinations and one sequence of events, and the sequence has to survive.
func TestStderrGoesWhereItIsToldAndKeepsItsOrder(t *testing.T) {
	t.Parallel()

	t.Run("errors go to their own destination", func(t *testing.T) {
		t.Parallel()
		var out, errs bytes.Buffer
		s := &Session{
			env:    &env{term: newTerm(&out, termOptions{})},
			stderr: &errs,
		}
		if _, err := s.WriteString("result\n"); err != nil {
			t.Fatalf("WriteString: %v", err)
		}
		if _, err := fmt.Fprint(s.Stderr(), "trouble\n"); err != nil {
			t.Fatalf("writing an error: %v", err)
		}
		if got := out.String(); got != "result\n" {
			t.Errorf("the output holds %q, want only the result", got)
		}
		if got := errs.String(); got != "trouble\n" {
			t.Errorf("the error stream holds %q, want only the error", got)
		}
	})

	t.Run("the order survives two destinations", func(t *testing.T) {
		t.Parallel()
		// Both ends write into one buffer, which is the only way to see
		// whether the sequence held. Without the flush the terminal's own
		// buffering would let the second line out first.
		var both bytes.Buffer
		s := &Session{
			env:    &env{term: newTerm(&both, termOptions{})},
			stderr: &both,
		}
		for i := range 3 {
			if _, err := fmt.Fprintf(s, "out %d\n", i); err != nil {
				t.Fatalf("writing output: %v", err)
			}
			if _, err := fmt.Fprintf(s.Stderr(), "err %d\n", i); err != nil {
				t.Fatalf("writing an error: %v", err)
			}
		}
		want := "out 0\nerr 0\nout 1\nerr 1\nout 2\nerr 2\n"
		if got := both.String(); got != want {
			t.Errorf("the two streams came out as\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("the order survives the terminal holding text back", func(t *testing.T) {
		t.Parallel()
		// The case the flush is for. Every public write through a Session
		// flushes already, so ordering holds there whatever this does; what
		// it protects is text the terminal is still holding, which is the
		// state the editor is in while it draws. Reaching that state needs
		// the terminal directly, so this test does.
		var both bytes.Buffer
		tm := newTerm(&both, termOptions{})
		tm.setBufferMode(buffered)
		s := &Session{env: &env{term: tm}, stderr: &both}

		tm.write("drawn but not flushed")
		if both.Len() != 0 {
			t.Fatalf("the terminal wrote %q already, so this test is not in the state it needs",
				both.String())
		}
		if _, err := fmt.Fprint(s.Stderr(), "|trouble"); err != nil {
			t.Fatalf("writing an error: %v", err)
		}
		if want := "drawn but not flushed|trouble"; both.String() != want {
			t.Errorf("the two came out as %q, want %q", both.String(), want)
		}
	})

	t.Run("a session with nowhere to write throws it away", func(t *testing.T) {
		t.Parallel()
		s := &Session{}
		if _, err := fmt.Fprint(s.Stderr(), "trouble"); err != nil {
			t.Errorf("writing to a session with no destination gave %v", err)
		}
	})
}

// TestWithStderrReachesTheSession checks the option, since every other one is
// checked and this is the newest.
// TestStderrKeepsItsWriterContract checks the two things Stderr promises as
// an io.Writer, neither of which was held.
//
// A writer that reports fewer bytes than it was given makes fmt report an
// error, and a writer that loses the failure underneath it tells the caller
// nothing they can act on. Both matter more here than for most writers,
// because this is where a program says what went wrong: a shell whose error
// stream fails silently is a shell that stops reporting errors and does not
// say so.
//
// New defaults the destination to os.Stderr, so the nil case is reached by
// asking for it, which WithStderr(nil) does.
func TestStderrKeepsItsWriterContract(t *testing.T) {
	t.Parallel()

	t.Run("nowhere to write is not a short write", func(t *testing.T) {
		t.Parallel()
		s := &Session{}
		n, err := fmt.Fprintf(s.Stderr(), "error: %s\n", "something")
		if err != nil {
			t.Errorf("writing to a session with nowhere to write gave %v", err)
		}
		if want := len("error: something\n"); n != want {
			t.Errorf("reported %d bytes written, want %d", n, want)
		}
	})

	t.Run("a destination that fails says so", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("the pipe went away")
		s := &Session{stderr: failingWriter{err: boom}}
		n, err := s.Stderr().Write([]byte("error: something\n"))
		if err == nil {
			t.Fatal("a destination that refused the write reported success")
		}
		// The reason has to survive, or the caller cannot tell a closed pipe
		// from a full disk.
		if !errors.Is(err, boom) {
			t.Errorf("the error is %v, which does not carry the reason underneath", err)
		}
		// And it has to say what was being done, the way SaveHistory does.
		if !strings.Contains(err.Error(), "error stream") {
			t.Errorf("the error is %v, which does not say what was being done", err)
		}
		if n != 0 {
			t.Errorf("reported %d bytes written after a failure, want 0", n)
		}
	})
}

// failingWriter refuses every write, which is what a closed pipe or a full
// disk looks like from here.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWithStderrReachesTheSession(t *testing.T) {
	t.Parallel()
	var errs bytes.Buffer
	c := &config{}
	WithStderr(&errs)(c)
	if c.stderr != &errs {
		t.Error("WithStderr did not reach the setting")
	}
	// And with no option, errors go to standard error rather than nowhere.
	fresh := &config{}
	if fresh.stderr != nil {
		t.Error("a config starts with an error destination, so New cannot tell it was not set")
	}
}

// ----------------------------------------------------------------------------
// The error values

// TestErrorsAreConstants checks what the Error type is for.
//
// The errors this package returns are constants rather than package level
// variables. A var holding an error is writable by anything that can see it,
// and an error that changes underneath a caller comparing against it is a
// fault nobody looks for. A constant cannot be reassigned, and the compiler
// says so rather than the code going wrong at run time.
//
// Nothing here can test that a reassignment fails, because a package that
// reassigned one would not build. What this holds instead is everything that
// has to keep working once they are constants.
//
// There is no case for comparing one with ==. Being a constant does not make
// that safe: a wrapped error is not equal to what it wraps, whatever the
// type, so errors.Is is still the way to ask. The first version of this test
// had such a case, presented as something the constant form buys, and it
// does not.
func TestErrorsAreConstants(t *testing.T) {
	t.Parallel()

	// Every error in the package, by the name a caller looks it up under.
	// The text is not written out here: it is derived from the name, so the
	// check is the rule rather than a copy of the answer.
	all := map[string]Error{
		"ErrClosed":       ErrClosed,
		"ErrInterrupted":  ErrInterrupted,
		"errNotATerminal": errNotATerminal,
	}

	t.Run("the text is the name without the Err prefix", func(t *testing.T) {
		t.Parallel()
		// So the words a caller prints and the identifier they looked it up
		// by are the same words, and neither can drift from the other.
		// Context belongs in the wrapping where the error is returned.
		for name, err := range all {
			if want := sentinelText(name); string(err) != want {
				t.Errorf("%s reads %q, want %q", name, string(err), want)
			}
		}
	})

	t.Run("they satisfy error", func(t *testing.T) {
		t.Parallel()
		for name, err := range all {
			if got := error(err).Error(); got != string(err) {
				t.Errorf("%s gave the message %q, want %q", name, got, string(err))
			}
		}
	})

	t.Run("errors.Is finds them through a wrap", func(t *testing.T) {
		t.Parallel()
		// This is the property a caller depends on, and the one that would
		// break if the type stopped being comparable.
		wrapped := fmt.Errorf("reading a line: %w", ErrInterrupted)
		if !errors.Is(wrapped, ErrInterrupted) {
			t.Error("errors.Is cannot find ErrInterrupted through a wrap")
		}
		if errors.Is(wrapped, ErrClosed) {
			t.Error("errors.Is found ErrClosed in an interrupt")
		}
	})

	t.Run("two of them are not equal", func(t *testing.T) {
		t.Parallel()
		// A string type compares by value, so two errors with the same text
		// would be one error. These do not share text, and a future one must
		// not either.
		seen := map[Error]string{}
		for name, err := range all {
			if other, ok := seen[err]; ok {
				t.Errorf("%s and %s have the same text %q, so they are the same error",
					name, other, string(err))
			}
			seen[err] = name
		}
	})

}

// sentinelText returns the text an error named name must carry: the name with
// its Err or err prefix taken off, split where a word starts, and lowercased.
// ErrNoSteps reads "no steps".
func sentinelText(name string) string {
	name = strings.TrimPrefix(strings.TrimPrefix(name, "Err"), "err")
	var words []string
	start := 0
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			words = append(words, name[start:i])
			start = i
		}
	}
	return strings.ToLower(strings.Join(append(words, name[start:]), " "))
}

// TestHistoryFileMode checks the permission a history file is created with.
//
// A history file holds whatever was typed at the prompt, which for a shell
// can include a connection string with a password in it. The C forces 0600
// with a chmod after fopen; the port dropped that call and created the file
// 0666, so the mode was whatever the umask left. This holds the mode being
// chosen rather than inherited.
func TestHistoryFileMode(t *testing.T) {
	t.Parallel()

	// The umask narrows a mode and cannot widen it, so a test asserting an
	// exact mode is asserting the umask as much as the code. It is measured
	// rather than asked for: syscall.Umask does not exist on Windows or
	// plan9, and creating a file with every bit set shows which bits the
	// system took away. That is the same answer without a build tag.
	//
	// A system that does not keep the permission bits at all is skipped,
	// with what went unchecked named rather than passed over.
	probe := filepath.Join(t.TempDir(), "umask-probe")
	if err := os.WriteFile(probe, nil, 0o777); err != nil {
		t.Fatalf("making the probe file: %v", err)
	}
	probeInfo, err := os.Stat(probe)
	if err != nil {
		t.Fatalf("reading the probe file: %v", err)
	}
	allowed := probeInfo.Mode().Perm()
	if allowed&0o077 == 0o077 && allowed&0o700 != 0o700 {
		t.Skipf("this system reports %04o for a file asked to be 0777, so it does not "+
			"keep the permission bits and no mode was checked", allowed)
	}
	if runtime.GOOS == "windows" {
		t.Skip("this system does not keep the permission bits, so no mode was checked")
	}

	for _, test := range []struct {
		name string
		opt  fs.FileMode
		want fs.FileMode
	}{
		{"the default is owner only", 0, DefaultHistoryFileMode},
		{"a caller can widen it", 0o644, 0o644},
		{"a caller can narrow it", 0o400, 0o400},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			name := filepath.Join(t.TempDir(), "history.txt")
			h := &history{mode: test.opt}
			if err := h.loadFrom(name, DefaultHistoryEntries); err != nil {
				t.Fatalf("loadFrom: %v", err)
			}
			h.push("select 1")
			if err := h.save(); err != nil {
				t.Fatalf("save: %v", err)
			}
			info, err := os.Stat(name)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			// Whatever the umask took from the probe, it takes from this.
			want := test.want & allowed
			if got := info.Mode().Perm(); got != want {
				t.Errorf("the history file is %04o, want %04o (a file asked for 0777 became %04o)",
					got, want, allowed)
			}
		})
	}
}

// TestHistorySaveReportsAWriteFailure checks that a failure to write is
// reported rather than swallowed, which is what the C does.
func TestHistorySaveReportsAWriteFailure(t *testing.T) {
	t.Parallel()
	// A directory that does not exist, so the temporary file beside the
	// history file cannot be made. The history itself loaded cleanly from
	// nothing, so the refusal below is about writing rather than about the
	// file having been read in part.
	h := &history{}
	missing := filepath.Join(t.TempDir(), "no-such-directory", "h.txt")
	if err := h.loadFrom(missing, DefaultHistoryEntries); err != nil {
		t.Fatalf("loadFrom on a missing file should be no error: %v", err)
	}
	h.push("select 1")
	err := h.save()
	if err == nil {
		t.Fatal("save into a directory that does not exist gave no error")
	}
	if !strings.Contains(err.Error(), "history file") {
		t.Errorf("the error is %q, which does not say what was being done", err)
	}
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Errorf("the error is %v, which does not carry an *fs.PathError", err)
	}
}

// TestHistoryIsNotTruncatedWhenItWasNotFullyRead is the fault Ken spotted.
//
// save rewrites the file from the list held in memory, and load stops at a
// line it cannot read. So one malformed line used to cost every line after
// it: measured at fifty bytes becoming nineteen, on the next line the user
// typed, because editLine saves after every read.
func TestHistoryIsNotTruncatedWhenItWasNotFullyRead(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "h.txt")
	body := "one\ntwo\nthree\nfour\nbad\\q\nsix\nseven\neight\nnine\nten\n"
	if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	h := &history{}
	if err := h.loadFrom(name, DefaultHistoryEntries); err == nil {
		t.Fatal("loading a file with an unreadable line gave no error")
	}
	if len(h.entries) == 0 {
		t.Fatal("nothing was read at all, so this is not the case being tested")
	}

	h.push("eleven")
	if err := h.save(); err == nil {
		t.Error("saving over a file that was not read in full gave no error")
	}
	after, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != body {
		t.Errorf("the file changed:\n got %q\nwant %q", string(after), body)
	}
}

// TestHistorySaveIsAtomic checks that the file is replaced rather than
// truncated and rewritten, so that a failure part way through cannot leave it
// shorter than it was.
func TestHistorySaveIsAtomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	name := filepath.Join(dir, "h.txt")

	h := &history{}
	if err := h.loadFrom(name, DefaultHistoryEntries); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"one", "two", "three"} {
		h.push(line)
		if err := h.save(); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	// Saving repeatedly writes the list, not the list again and again: the
	// file mirrors what is held rather than growing.
	got, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if want := "one\ntwo\nthree\n"; string(got) != want {
		t.Errorf("the file holds %q, want %q", string(got), want)
	}
	// And nothing is left beside it.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("the directory holds %q, want only the history file", names)
	}
}

// TestExampleRemembersBetweenRuns checks that the example's history outlives
// the program, which is what a history file is for.
//
// The example kept its history in memory only, because the option that named
// the file was split in two and the name was dropped rather than carried
// across. Every other feature it demonstrates is the real thing; this one was
// a hollow version of it, and it is the feature whose file handling has the
// most careful code behind it.
//
// Two runs in one directory. The first types a statement and leaves. The
// second walks back to it with the up arrow and has to see it.
//
// It takes two presses, not one: leaving is itself a line the user typed, so
// the newest entry from the first run is the quit command and the statement
// is behind it. Expecting one press was this test's own first mistake.
func TestExampleRemembersBetweenRuns(t *testing.T) {
	t.Parallel()
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	dir := t.TempDir()

	first, err := capture.Record(context.Background(), bin, capture.Session{
		Name: "writes-history",
		Term: "xterm-256color",
		Dir:  dir,
		Steps: []capture.Step{
			{Send: "select remembered;" + capture.KeyEnter},
			{Send: `\q` + capture.KeyEnter},
		},
	})
	if err != nil {
		// Recording needs a pseudo-terminal, which is written for Linux and
		// macOS. Windows would need a pseudo console. So this checks
		// nothing there, and says so rather than passing.
		t.Skipf("cannot record on this system, so nothing was checked about the "+
			"example keeping its history between runs: %v", err)
	}
	if !strings.Contains(stripEscapes(string(first.Bytes())), "select remembered;") {
		t.Fatal("the first run did not echo the statement, so it never got that far")
	}

	// The file itself, before anything reads it back.
	saved, err := os.ReadFile(filepath.Join(dir, "rline_example_history"))
	if err != nil {
		t.Fatalf("the first run wrote no history file: %v", err)
	}
	if !strings.Contains(string(saved), "select remembered;") {
		t.Errorf("the history file holds %q, want the statement that was typed", string(saved))
	}
	// And not the command that ended the session, which nobody wants back.
	if strings.Contains(string(saved), `\q`) {
		t.Errorf("the history file holds %q, which includes the quit command", string(saved))
	}
	// Owner only, because a history file holds whatever was typed.
	info, err := os.Stat(filepath.Join(dir, "rline_example_history"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("the history file is %04o, which others can read", got)
	}

	second, err := capture.Record(context.Background(), bin, capture.Session{
		Name: "reads-history",
		Term: "xterm-256color",
		Dir:  dir,
		Steps: []capture.Step{
			{Send: capture.KeyUp},
			// Escape clears the line, so the quit command is typed into an
			// empty one rather than onto the end of what history brought
			// back.
			{Send: capture.KeyEscape},
			{Send: `\q` + capture.KeyEnter},
		},
	})
	if err != nil {
		t.Fatalf("the second run: %v", err)
	}
	if drawn := stripEscapes(string(second.Bytes())); !strings.Contains(drawn, "select remembered;") {
		t.Errorf("the up arrow in a new run did not bring back the statement\ndrawn: %q\nfile: %q", drawn, string(saved))
	}
}

// TestExampleCarriesOnAfterAnInterrupt checks that ctrl-C gives up on the
// line rather than on the program.
//
// The example used to return the error, so ctrl-C on an empty line printed
// "error: reading a line: interrupted" and quit. Ken found it by pressing
// it. That is the opposite of why ReadLine answers ErrInterrupted at all:
// the C cannot tell an abandoned line from an empty one, so a program built
// on it cannot throw away a half-typed statement and carry on. This one can,
// and now does.
func TestExampleCarriesOnAfterAnInterrupt(t *testing.T) {
	t.Parallel()
	bin := exampleBinary(t)
	if out, err := exec.Command("go", "build", "-o", bin, "./example").CombinedOutput(); err != nil {
		t.Fatalf("building the example: %v\n%s", err, out)
	}
	tr, err := capture.Record(context.Background(), bin, capture.Session{
		Name: "interrupt",
		Term: "xterm-256color",
		Dir:  t.TempDir(),
		Steps: []capture.Step{
			// A statement begun and then given up on.
			{Send: "select half_typed"},
			{Send: "\x03"},
			// The prompt has to come back and still work.
			{Send: "select after;" + capture.KeyEnter},
			{Send: `\q` + capture.KeyEnter},
		},
	})
	if err != nil {
		// As above: no pseudo-terminal here, so nothing was checked about
		// what ctrl-C does.
		t.Skipf("cannot record on this system, so nothing was checked about ctrl-C "+
			"giving up on the line rather than the program: %v", err)
	}
	drawn := stripEscapes(string(tr.Bytes()))

	if strings.Contains(drawn, "error:") {
		t.Errorf("the example reported an error after ctrl-C\ndrawn: %s", drawn)
	}
	// The statement typed after the interrupt has to have run, which is the
	// proof that the prompt came back rather than the program ending.
	if !strings.Contains(drawn, "ran 1 line(s): select after;") {
		t.Errorf("the statement after the interrupt did not run\ndrawn: %s", drawn)
	}
	// And the abandoned half must not be carried into it.
	if strings.Contains(drawn, "half_typed select after;") {
		t.Errorf("the abandoned statement was kept\ndrawn: %s", drawn)
	}
}
