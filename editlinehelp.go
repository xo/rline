package rline

// The help screen.
//
// Not ported yet. This is the one entry point the key dispatch calls,
// declared so that the dispatch is complete and the seam is in one place.
// isocline/src/editline_help.c holds what goes here: a table of the keys the
// editor understands, drawn below the line in two columns.
//
// To port from isocline/src/editline_help.c.

// showHelp draws the list of keys below the line.
//
//nolint:unused // called by the key dispatch once the public API lands
func (ev *env) showHelp(e *editor) {
	ev.refresh(e)
}
