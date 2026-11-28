package rline

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/rline/ansi"
)

// fakeTerminal is a Terminal for the tests: what the test writes to keys is
// what the user types, and what rline writes is kept.
type fakeTerminal struct {
	in   *io.PipeReader
	keys *io.PipeWriter

	mu         sync.Mutex
	out        bytes.Buffer
	cols, rows int
	raw        []bool
	resized    chan struct{}
}

func newFakeTerminal() *fakeTerminal {
	in, keys := io.Pipe()
	return &fakeTerminal{in: in, keys: keys, cols: 80, rows: 24, resized: make(chan struct{}, 1)}
}

func (f *fakeTerminal) Read(p []byte) (int, error) {
	return f.in.Read(p) //nolint:wrapcheck // a Reader returns io.EOF itself, as io.Reader asks
}

func (f *fakeTerminal) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.out.Write(p) //nolint:wrapcheck // a Writer passes on what its buffer says
}

func (f *fakeTerminal) Size() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cols, f.rows
}

func (f *fakeTerminal) SetRaw(on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.raw = append(f.raw, on)
	return nil
}

func (f *fakeTerminal) Resized() <-chan struct{} { return f.resized }

func (f *fakeTerminal) output() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.out.String()
}

// typed sends each part as one read, as keys arrive from a host one message
// at a time. A part that starts with a tab comes at once, without the pause
// that comes before the others, so that one escape sequence can arrive in two
// reads, as a host can deliver it.
func (f *fakeTerminal) typed(t *testing.T, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if rest, ok := strings.CutPrefix(p, "\t"); ok {
			p = rest
		} else {
			time.Sleep(20 * time.Millisecond)
		}
		if _, err := io.WriteString(f.keys, p); err != nil {
			t.Error(err)
			return
		}
	}
	time.Sleep(20 * time.Millisecond)
}

// readLineFrom reads one line from s while keys are typed on f, and returns
// it with its error.
func readLineFrom(t *testing.T, s *Prompt, f *fakeTerminal, parts ...string) (string, error) {
	t.Helper()
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := s.ReadLine("$ ")
		done <- result{line, err}
	}()
	f.typed(t, parts...)
	select {
	case r := <-done:
		return r.line, r.err
	case <-time.After(5 * time.Second):
		t.Fatalf("the line never came back, and the terminal got %q", f.output())
	}
	return "", nil
}

// newHostSession returns a Session on a fake terminal.
func newHostSession(t *testing.T, f *fakeTerminal, opts ...Option) *Prompt {
	t.Helper()
	p, err := New(append([]Option{WithTerminal(f)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = f.keys.Close()
		_ = p.Close()
	})
	return p
}

// TestWithTerminalEditsLines checks that a Session on a Terminal edits lines
// as it does on a real terminal: an escape sequence that arrives in two
// reads is one key, a paste is one line, raw mode starts and stops around
// each line, and the draw goes to the Terminal (D79).
func TestWithTerminalEditsLines(t *testing.T) {
	f := newFakeTerminal()
	p := newHostSession(t, f)
	if !p.Interactive() {
		t.Fatal("a Session on a Terminal does not edit lines")
	}
	// Left comes as ESC [ in one read and D in the next.
	if line, err := readLineFrom(t, p, f, "ab", "\x1b[", "\tDX\r"); err != nil || line != "aXb" {
		t.Errorf("the line is %q, %v, want %q", line, err, "aXb")
	}
	if line, err := readLineFrom(t, p, f, "\x1b[200~one\ntwo\x1b[201~", "\r"); err != nil || line != "one\ntwo" {
		t.Errorf("the paste gave %q, %v, want %q", line, err, "one\ntwo")
	}
	out := f.output()
	for _, want := range []string{pasteOn, pasteOff} {
		if !strings.Contains(out, want) {
			t.Errorf("the terminal did not get %q:\n%q", want, out)
		}
	}
	// The default marker follows the prompt text.
	if screen := screenOf(out, 80); len(screen) == 0 || screen[0] != "$ > aXb" {
		t.Errorf("the terminal shows %q, want the first row to be %q", screen, "$ > aXb")
	}
	f.mu.Lock()
	raw := append([]bool(nil), f.raw...)
	f.mu.Unlock()
	if want := []bool{true, false, true, false}; !slicesEqualBool(raw, want) {
		t.Errorf("raw mode went %v, want %v", raw, want)
	}
}

func slicesEqualBool(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestWithTerminalEndsTheInput checks that io.EOF from the Terminal ends the
// input (D79).
func TestWithTerminalEndsTheInput(t *testing.T) {
	f := newFakeTerminal()
	p := newHostSession(t, f)
	done := make(chan error, 1)
	go func() {
		_, err := p.ReadLine("$ ")
		done <- err
	}()
	f.typed(t, "ab")
	_ = f.keys.Close()
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Errorf("the end of the input gave %v, want io.EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the end of the input did not end the read")
	}
}

// TestWithTerminalFollowsTheSize checks that a change of size reaches a read
// that waits for a key, as SIGWINCH does, and that Size gives the size of the
// Terminal (D79).
func TestWithTerminalFollowsTheSize(t *testing.T) {
	f := newFakeTerminal()
	p := newHostSession(t, f)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = p.ReadLine("$ ")
	}()
	f.typed(t, "abc")
	drawn := len(f.output())
	f.mu.Lock()
	f.cols, f.rows = 40, 10
	f.mu.Unlock()
	f.resized <- struct{}{}
	// The read waits for a key while the size changes, so the change must
	// wake it: the line is drawn again before the next key arrives.
	deadline := time.Now().Add(5 * time.Second)
	for len(f.output()) == drawn {
		if time.Now().After(deadline) {
			t.Fatal("nothing was drawn after the size changed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cols, rows := p.Size(); cols != 40 || rows != 10 {
		t.Errorf("Size gives %d by %d, want 40 by 10", cols, rows)
	}
	f.typed(t, "\r")
	<-done
}

// paletteTerminal is a fakeTerminal that says which colors it shows.
type paletteTerminal struct {
	*fakeTerminal

	palette ansi.Palette
}

func (p paletteTerminal) Palette() ansi.Palette { return p.palette }

// TestWithTerminalTakesItsColors checks that the colors come from the
// Terminal, and are 256 when it does not say (D79).
func TestWithTerminalTakesItsColors(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("NO_COLOR", "")
	for _, tc := range []struct {
		term Terminal
		want ansi.Palette
	}{
		{newFakeTerminal(), ansi.PaletteANSI256},
		{paletteTerminal{newFakeTerminal(), ansi.PaletteANSI16}, ansi.PaletteANSI16},
		{paletteTerminal{newFakeTerminal(), ansi.PaletteTrueColor}, ansi.PaletteTrueColor},
	} {
		p, err := New(WithTerminal(tc.term))
		if err != nil {
			t.Fatal(err)
		}
		if got := p.env.term.palette; got != tc.want {
			t.Errorf("the palette is %v, want %v", got, tc.want)
		}
		_ = p.Close()
	}
}
