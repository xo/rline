package rline

// The undo stack: what the line looked like before each change.
//
// The edit loop saves the whole line rather than a description of the change,
// because a line is short and saving it is simpler than working out how to
// reverse an edit.
//
// Ported from isocline/src/undo.c.

// editState is a line and where the cursor was in it.
type editState struct {
	// input is the whole line, and pos the byte the cursor sat on.
	input string
	pos   int
}

// editStack holds the states that undo steps back through. The most recent
// is at the end. The zero value is ready to use.
type editStack struct {
	states []editState
}

// capture saves a line and a cursor position.
func (s *editStack) capture(input string, pos int) {
	s.states = append(s.states, editState{input: input, pos: pos})
}

// restore takes the most recent saved line off the stack. It reports false
// when there is nothing left to step back to.
func (s *editStack) restore() (string, int, bool) {
	n := len(s.states)
	if n == 0 {
		return "", 0, false
	}
	state := s.states[n-1]
	s.states = s.states[:n-1]
	return state.input, state.pos, true
}

// reset throws away every saved line.
func (s *editStack) reset() {
	s.states = nil
}

// count returns how many saved lines the stack holds.
func (s *editStack) count() int {
	return len(s.states)
}
