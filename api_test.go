package rline

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// TestReadLineWithoutTerminal checks the path taken when there is nothing to
// edit on, which is what a program reading a script or a pipe gets.
func TestReadLineWithoutTerminal(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	r := &Reader{
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
	r := &Reader{noEdit: true, plain: bufio.NewReader(strings.NewReader("x\n"))}
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

// TestDefaultsMatchTheC checks the settings a Reader starts with against what
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
