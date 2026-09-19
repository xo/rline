package rline

// Offering completions from inside the edit loop.
//
// Not ported yet. This is the one entry point the key dispatch calls,
// declared so that the dispatch is complete and the seam is in one place.
// isocline/src/editline_completion.c holds what goes here: asking the
// completer, applying the only answer when there is only one, and drawing the
// menu when there are several, which reads its own keys.
//
// The list itself, and the completers that fill it, are already ported in
// completions.go and completers.go.
//
// To port from isocline/src/editline_completion.c.

// generateCompletions offers completions for the word at the cursor. When
// autoTab is set the caller asked for this rather than the user, so a single
// answer is applied without showing anything.
//
//nolint:unused // called by the key dispatch once the public API lands
func (ev *env) generateCompletions(e *editor, autoTab bool) {
	_ = autoTab
	ev.refresh(e)
}
