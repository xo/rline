// Completion: what the user could type instead of what they have typed, how a
// completer is asked for it, and the menu that shows the answers.
//
// Ported from isocline/src/completions.c, completers.c and
// editline_completion.c.

package rline

import (
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

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
}

// addFunc adds one completion. Word completion wraps this to adjust how much
// of the line the completion takes away.
type addFunc func(replacement, display, help string, deleteBefore, deleteAfter int) bool

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
}

// Add offers one completion, which replaces the prefix that was handed to the
// completer. It reports whether more are wanted: a completer that is walking a
// large directory should stop when it answers false.
func (c *Completion) Add(replacement string) bool {
	return c.AddCandidate(Candidate{Replacement: replacement})
}

// AddCandidate offers one completion, said in full. It reports whether more are
// wanted, as Add does.
func (c *Completion) AddCandidate(cand Candidate) bool {
	if c == nil || c.add == nil {
		return false
	}
	return c.add(cand.Replacement, cand.Display, cand.Help, cand.DeleteBefore, cand.DeleteAfter)
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
	cenv := &Completion{input: input, cursor: pos, arg: c.completerArg}
	cenv.add = func(replacement, display, help string, deleteBefore, deleteAfter int) bool {
		return c.add(replacement, display, help, deleteBefore, deleteAfter)
	}
	c.completerMax = maxOffers
	c.completer.Complete(cenv, input[:pos])
	return c.count()
}

// addCompletions offers every one of completions that starts with prefix,
// ignoring case. It stops early once no more are accepted.
func addCompletions(cenv *Completion, prefix string, completions []string) bool {
	for _, completion := range completions {
		if text.HasPrefixFold(completion, prefix) {
			if !cenv.add(completion, "", "", 0, 0) {
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
	prev := cenv.add
	cenv.add = func(replacement, display, help string, deleteBefore, deleteAfter int) bool {
		replacement = fix(replacement)
		return prev(replacement, display, help,
			deleteBeforeAdjust+deleteBefore,
			text.CountEndOverlap(replacement, postfix)+deleteAfter)
	}
	fun.Complete(cenv, word)
	cenv.add = prev
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
	if end := indexZero(buf); end >= 0 {
		return buf[:end]
	}
	return buf[:wlen]
}

// indexZero returns where the first zero byte is, or -1.
func indexZero(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
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
// filenames.go

// Completing a file name, and colouring what the menu shows for it.
//
// This is the first part of the port that reads the world rather than a
// string, so a corpus of recorded calls cannot be the whole test. The probe
// builds a fixed tree in a temporary directory and completes against it, and
// the Go test builds the same tree and does the same.
//
// Ported from isocline/src/completers.c.

// dirSeparator is what separates the parts of a path.
//
// Windows uses a backslash, and this will need a file per system when
// Windows is taken on. That is deferred until there is a Windows host to
// check it on, and PLAN.md says so.
const dirSeparator = '/'

// fileType says what kind of entry a name is, which decides its colour. The
// order is the one the BSD setting uses, because that setting is a string of
// two letters per type in exactly this order.
type fileType int

const (
	ftDefault fileType = iota
	ftDir
	ftSym
	ftSock
	ftPipe
	ftBlock
	ftChar
	ftSetuid
	ftSetgid
	ftDirOtherWritableSticky
	ftDirOtherWritable
	ftDirSticky
	ftExe
	ftLast
)

// lsColorNames are the keys the GNU setting uses for each file type, in the
// same order as the types.
var lsColorNames = []string{
	"no=", "di=", "ln=", "so=", "pi=", "bd=", "cd=", "su=", "sg=", "tw=",
	"ow=", "st=", "ex=",
}

// defaultBSDColors is the BSD setting used when the environment names none.
const defaultBSDColors = "exfxcxdxbxegedabagacad"

// lsColorSetting is how the environment says to colour a listing.
type lsColorSetting struct {
	// enabled is false when colouring is turned off, and then nothing else
	// matters.
	enabled bool

	// gnu is the GNU style setting, and hasGNU says it was given. An empty
	// one that was given still wins over the BSD style, and then nothing
	// matches.
	gnu    string
	hasGNU bool

	// bsd is the BSD style setting, which is used when the GNU one is absent.
	bsd string
}

// readLSColorSetting reads the colour settings from the environment.
//
// The C code reads these once and keeps them, so a program that changes them
// later keeps the first answer. This reads them each time, which is the same
// for any program that sets them before it starts and makes the behaviour
// testable.
func readLSColorSetting() lsColorSetting {
	s, ok := os.LookupEnv("CLICOLOR")
	if !ok || (s != "1" && s != "") {
		return lsColorSetting{}
	}
	setting := lsColorSetting{enabled: true, bsd: defaultBSDColors}
	if v, ok := os.LookupEnv("LS_COLORS"); ok {
		setting.gnu, setting.hasGNU = v, true
	}
	if v, ok := os.LookupEnv("LSCOLORS"); ok {
		setting.bsd = v
	}
	return setting
}

// colorFromKey appends the markup for one key of the GNU setting, and reports
// whether the key was there.
//
// The key is looked for anywhere in the setting rather than only at the start
// of an entry, so a key can match inside a longer one. The C code does the
// same.
func colorFromKey(b *strings.Builder, setting, key string) bool {
	if key == "" {
		return false
	}
	i := strings.Index(setting, key)
	if i < 0 {
		return false
	}
	rest := setting[i+len(key):]
	if key[len(key)-1] != '=' {
		// A file type key already ends with the equals sign. An extension
		// key does not, so one has to follow it.
		if rest == "" || rest[0] != '=' {
			return false
		}
		rest = rest[1:]
	}
	end := strings.IndexByte(rest, ':')
	if end < 0 {
		end = len(rest)
	}
	if end <= 0 {
		return false
	}
	b.WriteString(`[ansi-sgr="`)
	b.WriteString(rest[:end])
	b.WriteString(`"]`)
	return true
}

// colorFromChar reads one letter of the BSD setting as a colour number. The
// lower case letters are the first eight colours, the upper case letters the
// next eight, and anything else means the default.
func colorFromChar(c byte) int {
	switch {
	case c >= 'a' && c <= 'h':
		return int(c - 'a')
	case c >= 'A' && c <= 'H':
		return int(c-'A') + 8
	}
	return 256
}

// appendLSColor appends the markup that opens the colour for a file type, and
// reports whether it opened one. ext is the extension to look for, which only
// the GNU setting uses and only for an ordinary file.
func appendLSColor(b *strings.Builder, ft fileType, ext string, hasExt bool) bool {
	setting := readLSColorSetting()
	if !setting.enabled {
		return false
	}
	if setting.hasGNU {
		if ft == ftDefault && hasExt {
			if colorFromKey(b, setting.gnu, ext) {
				return true
			}
		}
		if ft >= ftDefault && ft < ftLast {
			if colorFromKey(b, setting.gnu, lsColorNames[ft]) {
				return true
			}
		}
		return false
	}
	// The BSD setting is two letters per file type, a foreground and a
	// background. A setting too short for this type leaves both at default.
	fg, bg := byte('x'), byte('x')
	if len(setting.bsd) > 2*int(ft)+1 {
		fg, bg = setting.bsd[2*ft], setting.bsd[2*ft+1]
	}
	fmt.Fprintf(b, "[ansi-color=%d ansi-bgcolor=%d]", colorFromChar(fg), colorFromChar(bg))
	return true
}

// colorizeEntry returns the markup the menu shows for one entry: the colour
// for its type, then the name marked so that the markup parser does not read
// the name itself as markup.
func colorizeEntry(noColor bool, ft fileType, name, ext string, hasExt bool, dirSep byte) string {
	var b strings.Builder
	opened := false
	if !noColor {
		opened = appendLSColor(&b, ft, ext, hasExt)
	}
	b.WriteString("[!pre]")
	b.WriteString(name)
	if dirSep != 0 {
		b.WriteByte(dirSep)
	}
	b.WriteString("[/pre]")
	if opened {
		b.WriteString("[/]")
	}
	return b.String()
}

// endsWith reports whether name ends with ending. An empty ending matches
// anything.
func endsWith(name, ending string) bool {
	if len(name) < len(ending) {
		return false
	}
	if ending == "" {
		return true
	}
	return name[len(name)-len(ending):] == ending
}

// matchExtension reports whether name ends with one of the extensions, which
// are separated by semicolons.
//
// An empty list matches everything. So does a list with an empty part in it,
// because an empty extension matches any name: ".txt;" matches everything,
// not only names ending in .txt.
func matchExtension(name, extensions string) bool {
	if extensions == "" {
		return true
	}
	for _, ext := range strings.Split(extensions, ";") {
		if endsWith(name, ext) {
			return true
		}
	}
	return false
}

// typeOf returns the file type of path, for colouring.
//
// It does not follow a symbolic link, so a link is a link whatever it points
// at. A path it cannot read is an ordinary file, which is what the C code
// ends up with when its stat fails and it reads the zeroed structure.
func typeOf(path string) fileType {
	info, err := os.Lstat(path)
	if err != nil {
		return ftDefault
	}
	mode := info.Mode()
	switch {
	case mode&fs.ModeSocket != 0:
		return ftSock
	case mode&fs.ModeSymlink != 0:
		return ftSym
	case mode&fs.ModeNamedPipe != 0:
		return ftPipe
	case mode&fs.ModeCharDevice != 0:
		return ftChar
	case mode&fs.ModeDevice != 0:
		return ftBlock
	case mode&fs.ModeDir != 0:
		// Only a directory is looked at for these. A file that is set-user
		// or sticky is still just a file, which is what the C code does
		// because it tests them inside its directory branch.
		switch {
		case mode&fs.ModeSetuid != 0:
			return ftSetuid
		case mode&fs.ModeSetgid != 0:
			return ftSetgid
		case mode.Perm()&0o020 != 0 && mode&fs.ModeSticky != 0:
			return ftDirOtherWritableSticky
		case mode.Perm()&0o020 != 0:
			return ftDirOtherWritable
		case mode&fs.ModeSticky != 0:
			return ftDirSticky
		}
		return ftDir
	}
	if mode.Perm()&0o100 != 0 {
		return ftExe
	}
	return ftDefault
}

// isDir reports whether path is a directory, following a symbolic link, so
// that a link to a directory completes with a separator after it.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isAbsolutePath reports whether path starts at the root.
func isAbsolutePath(path string) bool {
	return path != "" && path[0] == dirSeparator
}

// completeInDir offers every entry of dir that starts with base.
//
// dirPrefix is what goes in front of each name in the completion, which is
// the part of the path the user already typed. It reports false once no more
// completions are accepted.
func completeInDir(cenv *Completion, noColor bool, dir, dirPrefix, base string,
	dirSep byte, extensions string,
) bool {
	f, err := os.Open(dir)
	if err != nil {
		return true
	}
	defer func() { _ = f.Close() }()
	// Read in directory order rather than sorted, which is what the C sees.
	// Nothing depends on the order, because the menu sorts before it shows.
	entries, err := f.ReadDir(-1)
	if err != nil {
		return true
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." || !text.HasPrefixFold(name, base) {
			continue
		}
		full := dir + string(dirSeparator) + name
		ft := typeOf(full)
		directory := isDir(full)

		replacement := dirPrefix + name
		if directory && dirSep != 0 {
			replacement += string(dirSep)
		}
		if !directory && !matchExtension(name, extensions) {
			continue
		}
		shown := byte(0)
		if directory {
			shown = dirSep
		}
		// The file completer never passes an extension to look up, so the
		// extension colours of the GNU setting are never reached from here.
		// The C code is the same.
		display := colorizeEntry(noColor, ft, name, "", false, shown)
		if !cenv.add(replacement, display, "", 0, 0) {
			return false
		}
	}
	return true
}

// filenameCompleter offers the file names that could follow prefix.
func filenameCompleter(cenv *Completion, prefix string, noColor bool,
	dirSep byte, roots, extensions string,
) {
	// Split what was typed into the directory part and the start of a name.
	dirPrefix, base := "", prefix
	if i := strings.LastIndexByte(prefix, dirSeparator); i >= 0 {
		dirPrefix, base = prefix[:i+1], prefix[i+1:]
	}

	if isAbsolutePath(prefix) {
		// An absolute path is completed where it points rather than under
		// any of the roots.
		completeInDir(cenv, noColor, dirPrefix, dirPrefix, base, dirSep, extensions)
		return
	}
	// A relative path is completed under each root in turn. The C code does
	// not stop when no more completions are accepted, and neither does this.
	for _, root := range strings.Split(roots, ";") {
		dir := root + string(dirSeparator)
		if dirPrefix != "" {
			// Without its trailing separator, because one was just added.
			dir += dirPrefix[:len(dirPrefix)-1]
		}
		completeInDir(cenv, noColor, dir, dirPrefix, base, dirSep, extensions)
	}
}

// completeFilename offers file names for the word at the end of prefix.
//
// roots are the directories a relative name is looked for in, separated by
// semicolons, and extensions the endings a name must have, also separated by
// semicolons. An empty roots means the working directory, and an empty
// extensions means any name. A dirSep of zero means the one this system uses.
//
// The word is taken with completeQWordEx, so a name with a space in it can be
// completed whether the user quoted it or escaped the space.
func completeFilename(cenv *Completion, prefix string, noColor bool,
	dirSep byte, roots, extensions string,
) {
	if roots == "" {
		roots = "."
	}
	if dirSep == 0 {
		dirSep = dirSeparator
	}
	// The C code hands the settings to the completer through the argument
	// that belongs to the program, and leaves it pointing at a dead stack
	// value afterwards. A closure carries them here instead, so the
	// program's own argument is left alone.
	inner := CompleterFunc(func(cenv *Completion, word string) {
		filenameCompleter(cenv, word, noColor, dirSep, roots, extensions)
	})
	completeQWordEx(cenv, prefix, inner, text.CharIsFileNameLetter, defaultEscapeChar, defaultQuoteChars)
}

// --------------------------------------------------------------------------
// editlinecompletion.go

// Offering completions from inside the edit loop: asking the completer, and
// applying the only answer when there is only one.
//
// When there are several the menu draws them, which is in menu.go, because
// drawing below the line and reading its own keys is a different job from
// working out what the answers are.
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

// generateCompletions offers completions for the word at the cursor. When
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
			ev.term.beep()
		}
	case count == 1:
		if ev.complete(e, 0) && ev.completeAutoTab {
			ev.tty.pushCode(key.EventAutoTab)
		}
	default:
		if !moreAvailable {
			// Every completion is known, so as much of them as agrees can go
			// in before the menu is drawn.
			ev.completeLongestPrefix(e)
		}
		ev.completions.sort()
		ev.completionMenu(e, moreAvailable)
	}
}
