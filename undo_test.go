package rline

import "testing"

// TestEditStackIsAStack checks that the most recent saved line comes back
// first, which is what stepping back through edits means.
func TestEditStackIsAStack(t *testing.T) {
	t.Parallel()
	var s editStack
	if got := s.count(); got != 0 {
		t.Errorf("a new stack holds %d, want 0", got)
	}
	s.capture("a", 1)
	s.capture("ab", 2)
	if got := s.count(); got != 2 {
		t.Errorf("the stack holds %d, want 2", got)
	}
	for _, want := range []editState{{"ab", 2}, {"a", 1}} {
		input, pos, ok := s.restore()
		if !ok {
			t.Fatalf("nothing came back where %q was saved", want.input)
		}
		if input != want.input || pos != want.pos {
			t.Errorf("restored %q at %d, want %q at %d", input, pos, want.input, want.pos)
		}
	}
	if _, _, ok := s.restore(); ok {
		t.Error("an empty stack gave something back")
	}
	if got := s.count(); got != 0 {
		t.Errorf("the stack holds %d after being emptied, want 0", got)
	}
}

// TestEditStackReset checks that throwing the saved lines away leaves the
// stack usable. The edit loop does this when it starts a new line.
func TestEditStackReset(t *testing.T) {
	t.Parallel()
	var s editStack
	for i := range 5 {
		s.capture("line", i)
	}
	s.reset()
	if got := s.count(); got != 0 {
		t.Errorf("after reset the stack holds %d, want 0", got)
	}
	if _, _, ok := s.restore(); ok {
		t.Error("after reset something came back")
	}
	s.capture("again", 7)
	input, pos, ok := s.restore()
	if !ok || input != "again" || pos != 7 {
		t.Errorf("after reset the stack gave %q at %d, want %q at %d", input, pos, "again", 7)
	}
}
