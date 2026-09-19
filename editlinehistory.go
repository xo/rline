package rline

// Walking and searching the history from inside the edit loop.
//
// Not ported yet. These are the three entry points that the key dispatch
// calls, declared so that the dispatch is complete and the seam is in one
// place. isocline/src/editline_history.c holds what goes here: walking back
// and forward through the history, and the incremental search that Ctrl-R
// opens, which draws its own prompt and reads its own keys.
//
// To port from isocline/src/editline_history.c.

// historyPrev replaces the line with the previous history entry.
//
//nolint:unused // called by the key dispatch once the public API lands
func (ev *env) historyPrev(e *editor) {
	ev.refresh(e)
}

// historyNext replaces the line with the next history entry.
//
//nolint:unused // called by the key dispatch once the public API lands
func (ev *env) historyNext(e *editor) {
	ev.refresh(e)
}

// historySearchWithCurrentWord opens the incremental history search, starting
// from the word the cursor is in.
//
//nolint:unused // called by the key dispatch once the public API lands
func (ev *env) historySearchWithCurrentWord(e *editor) {
	ev.refresh(e)
}
