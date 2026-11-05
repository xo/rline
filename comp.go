// Completion: what the user could type instead of what they have typed, how a
// completer is asked for it, and the menu that shows the answers.
//
// Ported from isocline/src/completions.c, completers.c and
// editline_completion.c.

package rline

import (
	"bytes"
	"slices"

	"github.com/xo/rline/internal/editor"
	"github.com/xo/rline/internal/text"
	"github.com/xo/rline/key"
)

// --------------------------------------------------------------------------
// completions.go

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

	// suffix goes after a completion that is the only one.
	suffix string
}

// addFunc adds one completion. Word completion wraps this to adjust how much
// of the line the completion takes away.
type addFunc func(cand Candidate) bool

// Completer offers completions for the word at the cursor.
//
// It is handed the prefix to complete and calls Add for each answer. Returning
// without adding anything means there is nothing to offer.
//
// This is an interface rather than a function type so that a completer can
// grow a second method without breaking everyone who has one. Wrap a plain
// function in CompleterFunc.
type Completer interface {
	Complete(c *Completion, prefix string)
}

// CompleterFunc makes a Completer out of an ordinary function.
type CompleterFunc func(c *Completion, prefix string)

// Complete satisfies Completer.
func (f CompleterFunc) Complete(c *Completion, prefix string) { f(c, prefix) }

// Candidate is one completion, said in full.
//
// Every count here is in bytes, as every position in this package is, because
// that is what indexes a Go string. The one place characters are counted is
// LineStyle.StyleRunes, which says so in its name.
type Candidate struct {
	// Replacement is what goes into the line.
	Replacement string

	// Display is what the menu shows. An empty Display shows the
	// Replacement itself.
	Display string

	// Help is a line shown below the menu.
	Help string

	// DeleteBefore and DeleteAfter are how many bytes on each side of the
	// cursor this completion takes away.
	DeleteBefore int
	DeleteAfter  int

	// Suffix goes after the Replacement when it is the only completion and
	// the cursor is at the end of the line, as readline adds a space after a
	// word and a slash after a directory (D46). An empty Suffix adds nothing.
	Suffix string
}

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

	// replace is how many bytes before the cursor Add takes away. It is the
	// length of the prefix that the completer was handed, less what a
	// wrapper of add already takes away for it.
	replace int
}

// Add offers one completion, which replaces the prefix that was handed to the
// completer. When it is the only completion and the cursor is at the end of
// the line, a space follows it, as readline adds one (D56). It reports whether
// more are wanted: a completer that is walking a large directory should stop
// when it answers false.
func (c *Completion) Add(replacement string) bool {
	if c == nil {
		return false
	}
	return c.AddCandidate(Candidate{Replacement: replacement, DeleteBefore: c.replace, Suffix: " "})
}

// AddCandidate offers one completion, said in full. It reports whether more are
// wanted, as Add does.
func (c *Completion) AddCandidate(cand Candidate) bool {
	if c == nil || c.add == nil {
		return false
	}
	return c.add(cand)
}

// Text returns the whole line the completer was called on, not only the part
// before the cursor.
//
// A completer is handed the prefix to complete, which is usually all it
// needs. This is for the times it is not — deciding whether the cursor sits
// inside a word, for one, which is what stops a completion being offered in
// the middle of a word that is already there.
func (c *Completion) Text() string {
	if c == nil {
		return ""
	}
	return c.input
}

// Cursor returns where the cursor sits in Text, as a byte offset.
func (c *Completion) Cursor() int {
	if c == nil {
		return 0
	}
	return c.cursor
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
func (c *completions) add(cand Candidate) bool {
	if c.completerMax <= 0 {
		return false
	}
	c.completerMax--
	if !c.contains(cand.Replacement) {
		c.items = append(c.items, completion{
			replacement:  cand.Replacement,
			display:      cand.Display,
			help:         cand.Help,
			deleteBefore: cand.DeleteBefore,
			deleteAfter:  cand.DeleteAfter,
			suffix:       cand.Suffix,
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
	if hint == "" || text.IsCont(hint[0]) {
		return "", "", false
	}
	return hint, cm.help, true
}

// apply puts the completion at index into buf, and returns where the cursor
// should end up. It returns applyFail when there is no such completion, and
// applyNoop when the line already held it.
func (c *completions) apply(index int, buf *text.Buffer, pos int) int {
	cm, ok := c.get(index)
	if !ok {
		return applyFail
	}
	return applyCompletion(cm, buf, pos)
}

// applyCompletion puts one completion into buf.
func applyCompletion(cm *completion, buf *text.Buffer, pos int) int {
	start := max(pos-cm.deleteBefore, 0)
	n := cm.deleteBefore + cm.deleteAfter
	if len(cm.replacement) == n && start >= 0 && start+n <= buf.Length() &&
		string(buf.Bytes()[start:start+n]) == cm.replacement {
		// The line already reads this way.
		if cm.deleteAfter > 0 {
			// The completion happened inside a word, so the cursor still has
			// to move to the end of it.
			return start + n
		}
		return applyNoop
	}
	buf.DeleteFromTo(start, pos+cm.deleteAfter)
	return buf.InsertAt(cm.replacement, start)
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
		return text.CompareFold(a.replacement, b.replacement)
	})
}

// applyLongestPrefix puts in as much as every completion agrees on, so that
// pressing Tab with several matches fills in the part they share.
func (c *completions) applyLongestPrefix(buf *text.Buffer, pos int) int {
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
		deleteAfter:  text.CountEndOverlap(prefix, buf.StringFrom(pos)),
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
	cenv := &Completion{input: input, cursor: pos, arg: c.completerArg, replace: pos}
	cenv.add = c.add
	c.completerMax = maxOffers
	c.completer.Complete(cenv, input[:pos])
	return c.count()
}

// addCompletions offers every one of completions that starts with prefix,
// ignoring case. It stops early once no more are accepted.
func addCompletions(cenv *Completion, prefix string, completions []string) bool {
	for _, completion := range completions {
		if text.HasPrefixFold(completion, prefix) {
			if !cenv.add(Candidate{Replacement: completion}) {
				return false
			}
		}
	}
	return true
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

// --------------------------------------------------------------------------
// completers.go

// Completing a word rather than the whole line.
//
// A completer usually wants the last word of the line, not everything the
// user has typed. These narrow the prefix down to that word, hand it over,
// and then put back what they took off, by telling the completion how much of
// the line it has to take away when it goes in.
//
// The quoted form also understands quotes and escapes, so that completing a
// file name with a space in it works: the word given to the completer has its
// quoting taken off, and what comes back has it put on again.
//
// Ported from isocline/src/completers.c.

// defaultQuoteChars are the quotes the quoted form looks for when the caller
// names none.
const defaultQuoteChars = "'\""

// defaultEscapeChar is the escape the quoted form uses when the caller names
// none.
const defaultEscapeChar = '\\'

// completeWord narrows prefix to the word at its end and calls fun with just
// that word.
//
// isWordChar says what a word is made of. Passing nil means anything that is
// not a separator.
func completeWord(cenv *Completion, prefix string, fun Completer, isWordChar text.CharClass) {
	if isWordChar == nil {
		isWordChar = text.CharIsNonSeparator
	}
	p := []byte(prefix)
	pos := len(p)
	for pos > 0 {
		ofs, _ := text.PrevOfs(p, pos)
		if ofs <= 0 {
			break
		}
		if !isWordChar(p[pos-ofs : pos]) {
			break
		}
		pos -= ofs
	}
	withWordPrefix(cenv, len(prefix)-pos, func(replacement string) string {
		return replacement
	}, fun, prefix[pos:])
}

// withWordPrefix calls fun with word, with cenv set up so that every
// completion it offers is passed through fix and then told how much of the
// line to take away.
//
// deleteBeforeAdjust is how much of the line the word itself covers, which
// the completer knows nothing about. The amount to take away after the cursor
// is whatever end of the replacement is already typed there, so that
// completing in the middle of a word does not leave its tail behind.
func withWordPrefix(cenv *Completion, deleteBeforeAdjust int,
	fix func(string) string, fun Completer, word string,
) {
	postfix := ""
	if cenv.cursor >= 0 && cenv.cursor <= len(cenv.input) {
		postfix = cenv.input[cenv.cursor:]
	}
	prev, prevReplace := cenv.add, cenv.replace
	cenv.add = func(cand Candidate) bool {
		cand.Replacement = fix(cand.Replacement)
		cand.DeleteBefore += deleteBeforeAdjust
		cand.DeleteAfter += text.CountEndOverlap(cand.Replacement, postfix)
		return prev(cand)
	}
	// The wrapper takes the word away itself, so Add takes nothing more.
	cenv.replace = 0
	fun.Complete(cenv, word)
	cenv.add, cenv.replace = prev, prevReplace
}

// completeQWord is completeWord for a word that may be quoted, with the usual
// backslash escape and single or double quotes.
func completeQWord(cenv *Completion, prefix string, fun Completer, isWordChar text.CharClass) {
	completeQWordEx(cenv, prefix, fun, isWordChar, defaultEscapeChar, defaultQuoteChars)
}

// completeQWordEx is completeQWord with the escape character and the quotes
// named by the caller. An empty quoteChars means the default pair.
func completeQWordEx(cenv *Completion, prefix string, fun Completer,
	isWordChar text.CharClass, escape byte, quoteChars string,
) {
	if isWordChar == nil {
		isWordChar = text.CharIsNonSeparator
	}
	if quoteChars == "" {
		quoteChars = defaultQuoteChars
	}
	p := []byte(prefix)
	quote, pos, quoteLen := findQuotedWord(p, isWordChar, escape, quoteChars)

	if quote == 0 {
		// No quote, so the word is the run of word characters at the end,
		// stepping over anything that is escaped.
		pos = len(p)
		for pos > 0 {
			ofs, _ := text.PrevOfs(p, pos)
			if ofs <= 0 {
				break
			}
			if !isWordChar(p[pos-ofs : pos]) {
				// A separator ends the word unless it is escaped.
				if pos <= ofs || p[pos-ofs-1] != escape {
					break
				}
				pos-- // step over the escape as well
			}
			pos -= ofs
		}
	}

	word := p[pos:]
	if quote != 0 {
		word = word[:min(quoteLen, len(word))]
	}
	if quote == 0 {
		word = unescapeWord(word, isWordChar, escape)
	}

	deleteBeforeAdjust := len(p) - pos
	withWordPrefix(cenv, deleteBeforeAdjust, func(replacement string) string {
		return requoteReplacement(replacement, quote, isWordChar, escape)
	}, fun, string(word))
}

// findQuotedWord looks for a quoted word at the end of p. It returns the
// quote character, where the word starts just after it, and how long the word
// is. A quote of zero means there is no quoted word.
//
// It counts the quotes from the front rather than looking backwards, because
// only an odd count, or a closing quote with nothing but word characters
// after it, means the cursor is inside a quoted word.
func findQuotedWord(p []byte, isWordChar text.CharClass, escape byte, quoteChars string) (byte, int, int) {
	var quote byte
	openAt, closeAt, count := -1, -1, 0
	for pos := 0; pos < len(p); {
		switch {
		case p[pos] == escape && text.ByteAt(p, pos+1) != 0 && !isWordChar(p[pos+1:pos+2]):
			pos++ // step over the escape and whatever it escapes
		case count%2 == 0 && text.CharSetHas(quoteChars, p[pos]):
			openAt, quote = pos, p[pos]
			count++
		case count%2 == 1 && p[pos] == quote:
			closeAt = pos
			count++
		case !isWordChar(p[pos : pos+1]):
			// A separator outside a quote means any quote that closed before
			// it belongs to an earlier word.
			closeAt = -1
		}
		ofs, _ := text.NextOfs(p, pos)
		if ofs <= 0 {
			break
		}
		pos += ofs
	}
	// An odd count means a quote is still open. An even count with a closing
	// quote that only word characters follow means the cursor is still in
	// that word.
	if (count%2 == 0 && closeAt >= 0) || count%2 == 1 {
		return quote, openAt + 1, len(p) - openAt - 1
	}
	return 0, 0, 0
}

// unescapeWord takes the escapes off a word, so that the completer sees the
// name the user meant rather than the way it was typed.
//
// The C code shifts the rest of the string down over each escape it removes
// while keeping its idea of the length, so the walk carries on over what is
// left. This keeps a buffer of that fixed length with a terminating zero and
// does the same, then takes the string up to that zero.
func unescapeWord(word []byte, isWordChar text.CharClass, escape byte) []byte {
	wlen := len(word)
	buf := make([]byte, wlen+1)
	copy(buf, word)
	wpos := 0
	for wpos < wlen {
		ofs, _ := text.NextOfs(buf[:wlen], wpos)
		if ofs <= 0 {
			break
		}
		if buf[wpos] == escape && text.ByteAt(buf, wpos+1) != 0 &&
			!isWordChar(buf[wpos+1:min(wpos+1+ofs, len(buf))]) {
			copy(buf[wpos:], buf[wpos+1:])
		}
		wpos += ofs
	}
	if before, _, ok := bytes.Cut(buf, []byte{0}); ok {
		return before
	}
	return buf[:wlen]
}

// requoteReplacement puts the quoting back on a completion, so that what goes
// into the line reads the way the user typed the rest of it.
//
// Inside a quote it only needs the closing quote. Outside one, every
// character that is not part of a word gets the escape in front of it.
func requoteReplacement(replacement string, quote byte, isWordChar text.CharClass, escape byte) string {
	var buf text.Buffer
	buf.Replace(replacement)
	if quote != 0 {
		buf.AppendByte(quote)
		return buf.String()
	}
	pos := 0
	for {
		next, _ := buf.NextOfs(pos)
		if next <= 0 {
			break
		}
		if !isWordChar(buf.Bytes()[pos : pos+next]) {
			buf.InsertByteAt(escape, pos)
			pos++
		}
		pos += next
	}
	return buf.String()
}

// --------------------------------------------------------------------------
// editlinecompletion.go

// Offering completions from inside the edit loop: asking the completer,
// applying the only answer when there is only one, which is how a hint is
// taken, and opening the menu when there are more (D51). The readline
// commands that complete are in internal/readline, and bind.go draws the list
// they ask for.
//
// Ported from isocline/src/editline_completion.c.

// completionCommit settles the undo stack after a completion was applied, and
// reports whether the line or the cursor actually moved.
func completionCommit(e *editor.Editor, newPos int) bool {
	switch newPos {
	case applyFail:
		e.UndoRestore(false)
		return false
	case applyNoop:
		e.UndoForget()
		return false
	}
	e.Pos = newPos
	return true
}

// complete applies the completion at index and draws the result, reporting
// whether anything changed.
func (ev *env) complete(e *editor.Editor, index int) bool {
	e.StartModify()
	newPos := ev.completions.apply(index, &e.Input, e.Pos)
	changed := completionCommit(e, newPos)
	switch {
	case changed:
		ev.refresh(e)
	case newPos == applyNoop && ev.completions.count() > 1:
		// Nothing moved, but the menu still has to show which entry is
		// selected, so it is drawn again anyway.
		ev.refresh(e)
	}
	return changed
}

// completeLongestPrefix puts in as much as every completion agrees on.
func (ev *env) completeLongestPrefix(e *editor.Editor) {
	e.StartModify()
	completionCommit(e, ev.completions.applyLongestPrefix(&e.Input, e.Pos))
}

// generateCompletions offers completions for the word at the cursor, as
// isocline's Tab does, and is the command rline-menu-complete (D51). When
// autoTab is set the caller asked for this rather than the user, so nothing
// to complete passes quietly instead of beeping.
func (ev *env) generateCompletions(e *editor.Editor, autoTab bool) {
	if e.Pos < 0 {
		return
	}
	count := ev.completions.generate(e.Input.String(), e.Pos, maxCompletionsToTry)
	// The completer was asked to stop once it had this many, so reaching the
	// limit means there are probably others it never offered.
	moreAvailable := count >= maxCompletionsToTry
	switch {
	case count <= 0:
		if !autoTab {
			ev.term.bell()
		}
	case count == 1:
		if ev.complete(e, 0) && ev.completeAutoTab {
			ev.pushAutoTab()
		}
	default:
		if !moreAvailable {
			// Every completion is known, so as much of them as agrees can go
			// in before the menu is drawn.
			ev.completeLongestPrefix(e)
		}
		ev.completions.sort()
		ev.openMenu(e, moreAvailable)
	}
}

// pushAutoTab asks the edit loop to complete again, as the menu does after a
// completion, which can open the menu again.
func (ev *env) pushAutoTab() {
	ev.menuAutoTab = true
	ev.keys.pushCode(key.EventAutoTab)
}
