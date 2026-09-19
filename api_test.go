package rline

import (
	"bufio"
	"bytes"
	"errors"
	"io"
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
		{"history", WithHistory("h.txt", 12), func(c *config) bool {
			return c.historyFile == "h.txt" && c.historyEntries == 12
		}},
		{"no color", WithoutColor(), func(c *config) bool { return c.noColor }},
		{"no beep", WithoutBeep(), func(c *config) bool { return c.silent }},
		{"single line", SingleLine(), func(c *config) bool { return c.singlelineOnly }},
		{"no highlighting", WithoutHighlighting(), func(c *config) bool { return c.noHighlight }},
		{"no brace matching", WithoutBraceMatching(), func(c *config) bool { return c.noBraceMatch }},
		{"no brace insertion", WithoutBraceInsertion(), func(c *config) bool { return c.opts.NoAutoBrace }},
		{"no hints", WithoutHints(), func(c *config) bool { return c.noHint }},
		{"no indent", WithoutMultilineIndent(), func(c *config) bool { return c.noMultilineIndent }},
		{"auto tab", WithAutoTab(), func(c *config) bool { return c.completeAutoTab }},
		{"hint delay", WithHintDelay(time.Second), func(c *config) bool { return c.hintDelay == time.Second }},
		{"match braces", WithMatchBraces("<>"), func(c *config) bool { return c.opts.MatchBraces == "<>" }},
		{"auto braces", WithAutoBraces("<>"), func(c *config) bool { return c.opts.AutoBraces == "<>" }},
		{"input fd", WithInputFd(3), func(c *config) bool { return c.inFd == 3 }},
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
