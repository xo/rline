package rline

import "slices"

// The list of things the user could type instead of what they have typed so
// far, and how to put one of them into the line.
//
// A completion does not simply get appended. It says how much of the line to
// take away on each side of the cursor first, so that completing in the
// middle of a word replaces the word rather than doubling it.
//
// Ported from isocline/src/completions.c.

// How many completions to gather. The menu shows at most the first, and the
// completer is asked to stop once it has offered a quarter of that, so that a
// completer over a large directory does not run for ever.
const (
	maxCompletionsToShow = 1000
	maxCompletionsToTry  = maxCompletionsToShow / 4
)

// What applying a completion can report in place of a new cursor position.
const (
	// applyFail says there was no completion to apply.
	applyFail = -1

	// applyNoop says the line already held the completion, so neither the
	// line nor the cursor moved.
	applyNoop = -2
)

// maxCompletionPrefix is the longest shared start that is looked for. The C
// code copies it into a buffer of this size, and a longer one is cut short,
// which can cut a character in half.
const maxCompletionPrefix = 256

// completion is one thing the user could type.
type completion struct {
	// replacement is what goes into the line.
	replacement string

	// display is what the menu shows. An empty one shows the replacement.
	display string

	// help is the line shown under the menu for this entry.
	help string

	// deleteBefore and deleteAfter say how much of the line to take away on
	// each side of the cursor before the replacement goes in.
	deleteBefore int
	deleteAfter  int
}

// addFunc adds one completion. Word completion wraps this to adjust how much
// of the line the completion takes away.
type addFunc func(replacement, display, help string, deleteBefore, deleteAfter int) bool

// Completer offers completions for the word at the cursor.
//
// It is handed the prefix to complete and calls Add for each answer. Returning
// without adding anything means there is nothing to offer.
type Completer func(c *Completion, prefix string)

// Completion is what a completer is given: the whole line, where the cursor
// sits in it, and the way to offer an answer.
type Completion struct {
	// input is the whole line and cursor where the cursor sits in it. The
	// prefix a completer is handed is the line up to the cursor, or a part of
	// it once word completion has narrowed it down.
	input  string
	cursor int

	// arg is whatever the program passed when it set its completer.
	arg any

	// add offers one completion.
	add addFunc
}

// Add offers one completion, which replaces the prefix that was handed to the
// completer. It reports whether more are wanted: a completer that is walking a
// large directory should stop when it answers false.
func (c *Completion) Add(replacement string) bool {
	return c.AddFull(replacement, "", "", 0, 0)
}

// AddFull offers one completion, saying how it should be shown and how much of
// the line it replaces.
//
// display is what the menu shows, and an empty display shows the replacement
// itself. help is a line shown below the menu. deleteBefore and deleteAfter
// say how many bytes on each side of the cursor the completion takes away.
func (c *Completion) AddFull(replacement, display, help string, deleteBefore, deleteAfter int) bool {
	if c == nil || c.add == nil {
		return false
	}
	return c.add(replacement, display, help, deleteBefore, deleteAfter)
}

// Input returns the whole line and where the cursor sits in it, which a
// completer needs when the word alone is not enough to decide.
func (c *Completion) Input() (string, int) {
	return c.input, c.cursor
}

// completions holds what the completer offered, and the completer itself.
type completions struct {
	items []completion

	completer    Completer
	completerArg any

	// completerMax is how many more completions will be accepted. It counts
	// down, and every offer past zero is refused, which is how a completer
	// over a large directory is stopped.
	completerMax int
}

// count returns how many completions are held.
func (c *completions) count() int {
	return len(c.items)
}

// clear throws the completions away. It leaves the completer in place.
func (c *completions) clear() {
	c.items = nil
}

// setCompleter sets the function that offers completions.
func (c *completions) setCompleter(completer Completer, arg any) {
	c.completer = completer
	c.completerArg = arg
}

// contains reports whether a completion with this replacement is already
// held. The comparison is exact, so two that differ only in case are both
// kept.
func (c *completions) contains(replacement string) bool {
	for i := range c.items {
		if c.items[i].replacement == replacement {
			return true
		}
	}
	return false
}

// add offers one completion. It reports false once no more will be accepted,
// which is how a completer is told to stop.
//
// A replacement that is already held is counted but not kept.
func (c *completions) add(replacement, display, help string, deleteBefore, deleteAfter int) bool {
	if c.completerMax <= 0 {
		return false
	}
	c.completerMax--
	if !c.contains(replacement) {
		c.items = append(c.items, completion{
			replacement:  replacement,
			display:      display,
			help:         help,
			deleteBefore: deleteBefore,
			deleteAfter:  deleteAfter,
		})
	}
	return true
}

// get returns the completion at index.
func (c *completions) get(index int) (*completion, bool) {
	if index < 0 || index >= len(c.items) {
		return nil, false
	}
	return &c.items[index], true
}

// displayAt returns what the menu shows for a completion, and its help.
//
// An empty display shows the replacement instead. The C code keeps an absent
// display apart from an empty one and shows the empty one, but a Go caller
// cannot pass the absence of a string, so the two are the same here.
func (c *completions) displayAt(index int) (string, string, bool) {
	cm, ok := c.get(index)
	if !ok {
		return "", "", false
	}
	if cm.display == "" {
		return cm.replacement, cm.help, true
	}
	return cm.display, cm.help, true
}

// hintAt returns the part of a completion that is not already typed, which is
// shown in grey after the cursor, and its help.
//
// There is no hint when the part already typed is longer than the
// replacement, when nothing is left, or when what is left starts inside a
// character rather than at one.
func (c *completions) hintAt(index int) (string, string, bool) {
	cm, ok := c.get(index)
	if !ok {
		return "", "", false
	}
	// A negative count would step back off the front of the replacement. The
	// C code does step back, and reads whatever is in front of it, which is
	// not defined. This answers that there is no hint.
	if cm.deleteBefore < 0 || len(cm.replacement) < cm.deleteBefore {
		return "", "", false
	}
	hint := cm.replacement[cm.deleteBefore:]
	if hint == "" || isCont(hint[0]) {
		return "", "", false
	}
	return hint, cm.help, true
}

// apply puts the completion at index into buf, and returns where the cursor
// should end up. It returns applyFail when there is no such completion, and
// applyNoop when the line already held it.
func (c *completions) apply(index int, buf *buffer, pos int) int {
	cm, ok := c.get(index)
	if !ok {
		return applyFail
	}
	return applyCompletion(cm, buf, pos)
}

// applyCompletion puts one completion into buf.
func applyCompletion(cm *completion, buf *buffer, pos int) int {
	start := max(pos-cm.deleteBefore, 0)
	n := cm.deleteBefore + cm.deleteAfter
	if len(cm.replacement) == n && start >= 0 && start+n <= buf.length() &&
		string(buf.bytes()[start:start+n]) == cm.replacement {
		// The line already reads this way.
		if cm.deleteAfter > 0 {
			// The completion happened inside a word, so the cursor still has
			// to move to the end of it.
			return start + n
		}
		return applyNoop
	}
	buf.deleteFromTo(start, pos+cm.deleteAfter)
	return buf.insertAt(cm.replacement, start)
}

// sort puts the completions in the order the menu shows them, which is by
// length first and then by folded bytes, because that is what the C
// comparison does.
//
// Entries that compare the same keep the order they were added in. The C code
// leaves them wherever its sort puts them, which is not defined, so nothing
// depends on it either way.
func (c *completions) sort() {
	slices.SortStableFunc(c.items, func(a, b completion) int {
		return compareFold(a.replacement, b.replacement)
	})
}

// applyLongestPrefix puts in as much as every completion agrees on, so that
// pressing Tab with several matches fills in the part they share.
func (c *completions) applyLongestPrefix(buf *buffer, pos int) int {
	if len(c.items) <= 1 {
		return c.apply(0, buf, pos)
	}
	first, ok := c.get(0)
	if !ok {
		return applyFail
	}
	deleteBefore := first.deleteBefore
	prefix := first.replacement
	if len(prefix) > maxCompletionPrefix {
		// The C code copies into a buffer of this size, which can cut a
		// character in half.
		prefix = prefix[:maxCompletionPrefix]
	}
	for i := 1; i < len(c.items); i++ {
		cm := &c.items[i]
		if cm.deleteBefore != deleteBefore {
			// They do not take away the same amount, so there is nothing
			// safe to fill in.
			prefix = ""
			break
		}
		j := 0
		for j < len(prefix) && j < len(cm.replacement) && prefix[j] == cm.replacement[j] {
			j++
		}
		prefix = prefix[:j]
		if j <= 0 {
			break
		}
	}
	if len(prefix) == 0 || len(prefix) < deleteBefore {
		return applyNoop
	}
	shared := completion{
		replacement:  prefix,
		deleteBefore: deleteBefore,
		deleteAfter:  countEndOverlap(prefix, buf.stringFrom(pos)),
	}
	newPos := applyCompletion(&shared, buf, pos)
	if newPos < 0 {
		return newPos
	}
	// What is now in the line is part of every completion, so none of them
	// has to take it away again.
	for i := range c.items {
		c.items[i].deleteBefore = len(prefix)
	}
	return newPos
}

// generate asks the completer what could follow the line up to pos, and
// returns how many it offered. max is how many will be accepted.
func (c *completions) generate(input string, pos, maxOffers int) int {
	c.clear()
	if c.completer == nil || pos < 0 || len(input) < pos {
		return 0
	}
	cenv := &Completion{input: input, cursor: pos, arg: c.completerArg}
	cenv.add = func(replacement, display, help string, deleteBefore, deleteAfter int) bool {
		return c.add(replacement, display, help, deleteBefore, deleteAfter)
	}
	c.completerMax = maxOffers
	c.completer(cenv, input[:pos])
	return c.count()
}

// addCompletions offers every one of completions that starts with prefix,
// ignoring case. It stops early once no more are accepted.
func addCompletions(cenv *Completion, prefix string, completions []string) bool {
	for _, completion := range completions {
		if hasPrefixFold(completion, prefix) {
			if !cenv.add(completion, "", "", 0, 0) {
				return false
			}
		}
	}
	return true
}

// stringFrom returns the contents of b from pos onwards, or nothing when pos
// is outside it.
func (b *buffer) stringFrom(pos int) string {
	if pos < 0 || pos > len(b.buf) {
		return ""
	}
	return string(b.buf[pos:])
}

// hasCompletions reports whether anything has been offered yet.
func (c *completions) hasCompletions() bool {
	return len(c.items) > 0
}

// stopCompleting reports whether no more completions will be accepted, so a
// completer can give up rather than keep looking.
func (c *completions) stopCompleting() bool {
	return c.completerMax <= 0
}
