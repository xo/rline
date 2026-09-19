package capture

import "testing"

// TestSessionsAreWellFormed guards the session table, because a duplicate name
// silently overwrites a golden file.
func TestSessionsAreWellFormed(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool, len(Sessions))
	for i, s := range Sessions {
		switch {
		case s.Name == "":
			t.Errorf("session %d has no name", i)
		case s.About == "":
			t.Errorf("session %q says nothing about what it exercises", s.Name)
		case seen[s.Name]:
			t.Errorf("session %q appears twice", s.Name)
		case len(s.Steps) == 0:
			t.Errorf("session %q has no steps", s.Name)
		}
		seen[s.Name] = true
	}
}
