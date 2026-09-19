package rline

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// The public interface.
//
// This is the one part of the port that is not a translation. The C keeps a
// single environment in a process global and every public function reaches for
// it, so a program can only have one line reader and cannot say which terminal
// it is on. A Go package should not work that way, so the state lives in a
// Reader that the caller makes, and the settings are options passed when it is
// made rather than global switches flipped afterwards.
//
// Ported in spirit, not in shape, from isocline/src/isocline.c.

// Default settings.
const (
	// DefaultPromptMarker is written after the prompt text.
	DefaultPromptMarker = "> "

	// DefaultMatchBraces are the pairs whose partner is highlighted.
	DefaultMatchBraces = "()[]{}"

	// DefaultAutoBraces are the pairs that close themselves when typed.
	DefaultAutoBraces = `()[]{}""''`

	// DefaultHintDelay is how long to wait before showing a hint.
	DefaultHintDelay = 400 * time.Millisecond

	// DefaultMultilineEOL ends a line that carries on below.
	DefaultMultilineEOL = '\\'
)

// defaultStyles are the styles that markup can use without defining them. The
// names starting with "ic-" are the editor's own.
var defaultStyles = [][2]string{
	{"ic-prompt", "ansi-green"},
	{"ic-info", "ansi-darkgray"},
	{"ic-diminish", "ansi-lightgray"},
	{"ic-emphasis", "#ffffd7"},
	{"ic-hint", "ansi-darkgray"},
	{"ic-error", "#d70000"},
	{"ic-bracematch", "ansi-white"},
	{"keyword", "#569cd6"},
	{"control", "#c586c0"},
	{"number", "#b5cea8"},
	{"string", "#ce9178"},
	{"comment", "#6A9955"},
	{"type", "darkcyan"},
	{"constant", "#569cd6"},
}

// Reader reads lines from a terminal, with editing, history and completion.
//
// A Reader is not safe for use from more than one goroutine at a time.
type Reader struct {
	// env holds the terminal, the keyboard and everything the editor reads.
	env *env

	// plain reads lines when there is no terminal to edit on, which is what
	// happens when the input is a pipe or a file.
	plain *bufio.Reader

	// noEdit says there is no terminal to edit on.
	noEdit bool

	// closed stops a second Close from touching the terminal again.
	closed bool
}

// config carries what New needs before it builds a Reader.
type config struct {
	// Where the reader reads and writes. A negative fd means standard input.
	inFd int
	out  io.Writer

	// The prompt, and the one used for the lines after the first.
	promptMarker  string
	cpromptMarker string

	// Where the history is kept, and how much of it.
	historyFile    string
	historyEntries int

	// What marks up a line, and what completes a word.
	highlighter highlightFunc
	completer   completerFunc

	// Editing settings.
	opts editOptions

	// Settings that turn parts of the editor off.
	noColor           bool
	silent            bool
	noMultilineIndent bool
	noHighlight       bool
	noBraceMatch      bool
	noHint            bool
	singlelineOnly    bool
	completeAutoTab   bool

	hintDelay time.Duration
}

// Option changes a setting on a Reader being made.
type Option func(*config)

// WithOutput writes to w rather than to standard output.
func WithOutput(w io.Writer) Option {
	return func(c *config) { c.out = w }
}

// WithInputFd reads keys from the given file descriptor rather than from
// standard input.
func WithInputFd(fd int) Option {
	return func(c *config) { c.inFd = fd }
}

// WithPrompt sets the marker written after the prompt text, and the one used
// on the lines after the first. An empty continuation marker repeats the
// first.
func WithPrompt(marker, continuation string) Option {
	return func(c *config) {
		c.promptMarker = marker
		c.cpromptMarker = continuation
		if continuation == "" {
			c.cpromptMarker = marker
		}
	}
}

// WithHistory keeps the history in the named file, holding at most entries of
// them. An empty name keeps the history only while the program runs. Zero
// entries means the default of 200.
func WithHistory(fname string, entries int) Option {
	return func(c *config) {
		c.historyFile = fname
		c.historyEntries = entries
	}
}

// WithHighlighter marks up each line as it is typed.
func WithHighlighter(fn func(env *highlightEnv, input string)) Option {
	return func(c *config) { c.highlighter = fn }
}

// WithCompleter offers completions for the word at the cursor.
func WithCompleter(fn func(cenv *completionEnv, prefix string)) Option {
	return func(c *config) { c.completer = fn }
}

// WithoutColor writes no color, whatever the terminal supports.
func WithoutColor() Option {
	return func(c *config) { c.noColor = true }
}

// WithoutBeep stays quiet where the editor would beep.
func WithoutBeep() Option {
	return func(c *config) { c.silent = true }
}

// SingleLine refuses line breaks, so a line is always one line.
func SingleLine() Option {
	return func(c *config) { c.singlelineOnly = true }
}

// WithoutHighlighting turns off marking up the line.
func WithoutHighlighting() Option {
	return func(c *config) { c.noHighlight = true }
}

// WithoutBraceMatching turns off highlighting the partner of a brace.
func WithoutBraceMatching() Option {
	return func(c *config) { c.noBraceMatch = true }
}

// WithoutBraceInsertion turns off closing a brace automatically.
func WithoutBraceInsertion() Option {
	return func(c *config) { c.opts.NoAutoBrace = true }
}

// WithoutHints turns off showing the rest of the only completion that fits.
func WithoutHints() Option {
	return func(c *config) { c.noHint = true }
}

// WithoutMultilineIndent stops the lines after the first lining up under the
// prompt.
func WithoutMultilineIndent() Option {
	return func(c *config) { c.noMultilineIndent = true }
}

// WithAutoTab keeps completing while there is only one answer.
func WithAutoTab() Option {
	return func(c *config) { c.completeAutoTab = true }
}

// WithHintDelay waits d before showing a hint. Zero shows it at once.
func WithHintDelay(d time.Duration) Option {
	return func(c *config) { c.hintDelay = d }
}

// WithMatchBraces sets the pairs whose partner is highlighted, given in pairs
// such as "()[]{}".
func WithMatchBraces(pairs string) Option {
	return func(c *config) { c.opts.MatchBraces = pairs }
}

// WithAutoBraces sets the pairs that close themselves when typed.
func WithAutoBraces(pairs string) Option {
	return func(c *config) { c.opts.AutoBraces = pairs }
}

// New returns a Reader.
//
// It returns a Reader even when there is no terminal to edit on, such as when
// the input is a pipe. ReadLine then reads a plain line with no editing, which
// is what the C does and what a program reading a script expects.
func New(opts ...Option) (*Reader, error) {
	c := &config{
		inFd:           -1,
		out:            os.Stdout,
		promptMarker:   DefaultPromptMarker,
		cpromptMarker:  DefaultPromptMarker,
		historyEntries: 200,
		hintDelay:      DefaultHintDelay,
		opts: editOptions{
			MatchBraces:  DefaultMatchBraces,
			AutoBraces:   DefaultAutoBraces,
			MultilineEOL: DefaultMultilineEOL,
		},
	}
	for _, o := range opts {
		o(c)
	}

	r := &Reader{plain: bufio.NewReader(os.Stdin)}

	// A missing keyboard is a mode rather than a failure: a program whose
	// input is a pipe or a file still wants its lines, and gets them without
	// editing. The terminal is built either way, because the output is still
	// worth writing even when there is nothing to edit on. Losing it was the
	// bug this shape fixes.
	t, ttyErr := openTTY(c.inFd)
	isUTF8 := true
	if ttyErr == nil {
		isUTF8 = t.isUTF8
	}
	tm := newTerm(c.out, termOptions{
		// Color goes off when the output is not a terminal, so that a program
		// whose output is redirected writes plain text rather than escape
		// sequences into a file. The C makes the same check.
		NoColor: c.noColor || !writesToTerminal(c.out),
		Silent:  c.silent,
		IsUTF8:  isUTF8,
		Sizer:   outputSizer(c.out),
	})
	bb := newBBCode(tm)
	for _, s := range defaultStyles {
		bb.styleDef(s[0], s[1])
	}
	h := &history{}
	h.loadFrom(c.historyFile, c.historyEntries)
	cs := &completions{}
	if c.completer != nil {
		cs.setCompleter(c.completer, nil)
	}
	r.env = &env{
		term:              tm,
		tty:               t,
		bb:                bb,
		history:           h,
		completions:       cs,
		promptMarker:      c.promptMarker,
		cpromptMarker:     c.cpromptMarker,
		highlighter:       c.highlighter,
		opts:              c.opts,
		noMultilineIndent: c.noMultilineIndent,
		noHighlight:       c.noHighlight,
		noBraceMatch:      c.noBraceMatch,
		noHint:            c.noHint,
		singlelineOnly:    c.singlelineOnly,
		completeAutoTab:   c.completeAutoTab,
		hintDelay:         c.hintDelay,
	}
	r.noEdit = ttyErr != nil || !isInteractive()
	return r, nil //nolint:nilerr // a missing keyboard is a mode, not a failure
}

// writesToTerminal reports whether w is a terminal. Anything that is not a
// file is taken to be one, since a caller that passes its own writer has said
// where the output goes and is not redirecting it by accident.
func writesToTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return true
	}
	return isATTY(int(f.Fd()))
}

// outputSizer returns something that can report the size of the terminal
// being written to, or nil when the output is not a file at all.
func outputSizer(w io.Writer) sizer {
	f, ok := w.(*os.File)
	if !ok {
		return nil
	}
	return fileSizer{f: f}
}

// ReadLine reads one line.
//
// It returns io.EOF when the user ended the input, which is Ctrl-D on an empty
// line, and which is what the C answers with a null pointer.
func (r *Reader) ReadLine(prompt string) (string, error) {
	if r.closed {
		return "", ErrClosed
	}
	if r.noEdit {
		return r.readPlain(prompt)
	}
	line, ok, err := r.env.readLine(prompt)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", io.EOF
	}
	return line, nil
}

// readPlain reads a line with no editing, for when there is no terminal to
// edit on.
//
// The prompt is written only when there is a keyboard, which means the user is
// typing at a terminal that cannot be edited on. When the input is a pipe
// there is nobody to prompt and the prompt would only dirty the output, so it
// is left out. That is what the C does as well.
func (r *Reader) readPlain(prompt string) (string, error) {
	if r.env != nil && r.env.tty != nil {
		r.env.term.write(prompt)
		r.env.term.write(r.env.promptMarker)
		r.env.term.flush()
	}
	line, err := r.plain.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		if errors.Is(err, io.EOF) {
			return "", io.EOF
		}
		return "", fmt.Errorf("reading a line: %w", err)
	}
	return trimNewline(line), nil
}

// trimNewline drops the line ending that ReadString keeps.
func trimNewline(s string) string {
	s = trimSuffix(s, "\n")
	return trimSuffix(s, "\r")
}

// trimSuffix drops suffix from s when it is there.
func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}

// Close puts the terminal back as it was. A Reader cannot be used afterwards.
func (r *Reader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.env == nil {
		return nil
	}
	r.env.term.free()
	if r.env.tty == nil {
		return nil
	}
	if err := r.env.tty.close(); err != nil {
		return fmt.Errorf("closing the terminal: %w", err)
	}
	return nil
}

// Print writes markup such as "[red]text[/red]" to the terminal.
func (r *Reader) Print(s string) {
	if r.env == nil {
		return
	}
	r.env.bb.print(s)
	r.env.term.flush()
}

// Println writes markup and ends the line.
func (r *Reader) Println(s string) {
	if r.env == nil {
		return
	}
	r.env.bb.println(s)
	r.env.term.flush()
}

// Printf writes formatted markup.
func (r *Reader) Printf(format string, args ...any) {
	r.Print(fmt.Sprintf(format, args...))
}

// DefineStyle gives a name to a set of attributes, so that markup can use it.
// The spec is written the way the inside of a tag is, such as "bold color=red".
func (r *Reader) DefineStyle(name, spec string) {
	if r.env == nil {
		return
	}
	r.env.bb.styleDef(name, spec)
}

// AddHistory adds an entry to the history.
func (r *Reader) AddHistory(entry string) {
	if r.env == nil {
		return
	}
	r.env.history.push(entry)
}

// ClearHistory empties the history.
func (r *Reader) ClearHistory() {
	if r.env == nil {
		return
	}
	r.env.history.clear()
}

// SaveHistory writes the history to the file it was given, if it was given one.
func (r *Reader) SaveHistory() error {
	if r.env == nil {
		return nil
	}
	if err := r.env.history.save(); err != nil {
		return fmt.Errorf("saving the history: %w", err)
	}
	return nil
}

// Interactive reports whether there is a terminal to edit on. When there is
// not, ReadLine reads a plain line.
func (r *Reader) Interactive() bool {
	return !r.noEdit
}
