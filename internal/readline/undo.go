package readline

// undoEntry is one change to the line: text put in at start, or text taken
// out from start.
type undoEntry struct {
	insert bool
	start  int
	n      int    // how many bytes an insert put in
	text   []byte // what a delete took out
}

// undoGroup is every change one command made. Undo takes back a whole group.
type undoGroup struct {
	entries []undoEntry
	// typed says the group holds one run of typed characters, which the next
	// typed character can join.
	typed bool
}

// undoList is the changes made to the line, oldest first.
type undoList struct {
	groups []undoGroup
	open   *undoGroup

	// join puts the changes of every command into one group, as vi does
	// for what is typed in one insert, and joined says that the group was
	// started.
	join, joined bool
}

// typedLimit is how many bytes of typed characters readline joins into one
// undo, measured as 20 for one byte and for two byte characters alike.
const typedLimit = 20

// begin starts the group of the command about to run.
func (u *undoList) begin() {
	u.open = &undoGroup{}
}

// end closes the group of the command that ran, and keeps it when it changed
// anything.
func (u *undoList) end() {
	if u.open != nil && len(u.open.entries) > 0 {
		if u.join && u.joined && len(u.groups) > 0 {
			last := &u.groups[len(u.groups)-1]
			last.entries = append(last.entries, u.open.entries...)
			last.typed = false
		} else {
			u.groups = append(u.groups, *u.open)
		}
		u.joined = u.joined || u.join
	}
	u.open = nil
}

// recordInsert records n bytes put in at start. A typed character joins the
// run of typed characters before it, when it continues that run and the run
// is shorter than typedLimit.
func (u *undoList) recordInsert(start, n int, typed bool) {
	if u.open == nil {
		return
	}
	if typed && len(u.open.entries) == 0 && len(u.groups) > 0 {
		last := &u.groups[len(u.groups)-1]
		if last.typed && len(last.entries) == 1 {
			e := &last.entries[0]
			if e.insert && e.start+e.n == start && e.n < typedLimit {
				e.n += n
				return
			}
		}
	}
	u.open.entries = append(u.open.entries, undoEntry{insert: true, start: start, n: n})
	u.open.typed = typed && len(u.open.entries) == 1
}

// recordDelete records text taken out at start.
func (u *undoList) recordDelete(start int, text []byte) {
	if u.open == nil {
		return
	}
	u.open.entries = append(u.open.entries, undoEntry{start: start, text: text})
	u.open.typed = false
}

// pop takes the newest group off the list.
func (u *undoList) pop() (undoGroup, bool) {
	if len(u.groups) == 0 {
		return undoGroup{}, false
	}
	g := u.groups[len(u.groups)-1]
	u.groups = u.groups[:len(u.groups)-1]
	return g, true
}

// undoOne takes back the newest group of changes, in reverse order. Taking
// back an insert leaves the cursor where the insert started, and taking back
// a delete leaves it after the restored text. The mark does not move.
func (l *Line) undoOne() bool {
	g, ok := l.undo.pop()
	if !ok {
		return false
	}
	for i := len(g.entries) - 1; i >= 0; i-- {
		e := g.entries[i]
		if e.insert {
			l.buf = append(l.buf[:e.start], l.buf[e.start+e.n:]...)
			l.point = e.start
			continue
		}
		l.buf = append(l.buf[:e.start], append(append([]byte(nil), e.text...), l.buf[e.start:]...)...)
		l.point = e.start + len(e.text)
	}
	return true
}
