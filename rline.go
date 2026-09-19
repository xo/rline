// Package rline is a readline package written in pure Go. A readline package
// reads a line of text from a terminal, and gives the user editing, history,
// and completion.
//
// rline is a port of isocline, a readline replacement written in C by Daan
// Leijen. isocline carries the MIT license, and this copyright notice covers
// every part of this package that derives from it:
//
//	Copyright (c) 2021, Daan Leijen
//
// The port does not use cgo. It keeps the behavior of the C code, including
// the places where that behavior departs from a standard, because recorded
// sessions from the C build are the test corpus. Each such departure carries a
// comment where the code makes it.
// Package rline reads lines from a terminal, with editing, history,
// completion and syntax highlighting.
//
// This file holds the public interface — the Prompt a program builds, the
// Session inside it, the markup writer beside it and the options that make
// them — and the session log that records what passes through.
package rline

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// --------------------------------------------------------------------------
// rline.go

// Error values.
var (
	// ErrClosed is returned by a Session that has been closed.
	ErrClosed = errors.New("the reader is closed")

	// ErrInterrupted is returned when the user abandoned what was being read,
	// which is Ctrl-C or Ctrl-G, and which asks for the reading to be given
	// up rather than for the input to end.
	//
	// The C has no way to say this. It clears the line and hands back an
	// empty string, so a caller cannot tell an abandoned line from Enter on
	// an empty one. A shell has to: usql resets its statement buffer on an
	// interrupt and carries on, and would otherwise run whatever Ctrl-C left
	// behind. This is the one place where the port departs from the C over a
	// behaviour rather than a fault.
	ErrInterrupted = errors.New("interrupted")
)

// --------------------------------------------------------------------------
// api.go

// The public interface.
//
// This is the one part of the port that is not a translation. The C keeps a
// single environment in a process global and every public function reaches for
// it, so a program can only have one line reader and cannot say which terminal
// it is on. A Go package should not work that way, so the state lives in a
// Session that the caller makes, and the settings are options passed when it is
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

	// DefaultHistoryEntries is how many lines are remembered.
	DefaultHistoryEntries = 200
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

// Session reads lines from a terminal, with editing, history and completion.
//
// A Session is not safe for use from more than one goroutine at a time.
type Session struct {
	// env holds the terminal, the keyboard and everything the editor reads.
	env *env

	// plain reads lines when there is no terminal to edit on, which is what
	// happens when the input is a pipe or a file.
	plain *bufio.Reader

	// noEdit says there is no terminal to edit on.
	noEdit bool

	// closed stops a second Close from touching the terminal again.
	closed bool

	// log records the session, and may be nil.
	log *sessionLog
}

// config carries what New needs before it builds a Session.
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

	// What marks up a line, what completes a word, and what decides whether a
	// line is finished.
	highlighter  Highlighter
	completer    Completer
	isIncomplete func(string) bool

	// Editing settings.
	opts editOptions

	// Settings that turn parts of the editor off.
	noColor           bool
	silent            bool
	noMultilineIndent bool
	noHighlight       bool
	noBraceMatch      bool
	noHint            bool
	noHelp            bool
	singlelineOnly    bool
	completeAutoTab   bool

	hintDelay time.Duration

	// log records everything read and written, and may be nil.
	log io.Writer

	// in reads the keys when it is not a terminal, which is what a pipe or a
	// test gives.
	in io.Reader
}

// Option changes a setting on a Session being made.
type Option func(*config)

// WithOutput writes to w rather than to standard output.
func WithOutput(w io.Writer) Option {
	return func(c *config) { c.out = w }
}

// WithInput reads keys from r rather than from standard input.
//
// Editing needs a terminal, which is reached by file descriptor on Unix and
// by handle on Windows. So a reader that can say which descriptor it is — an
// *os.File, or anything else with an Fd method — is edited on, and anything
// else is read plainly with no editing, the same as a pipe.
//
// A *bufio.Reader cannot be edited on, whatever it wraps, because wrapping
// hides the descriptor and takes bytes out of the terminal that the editor
// then never sees. Pass the *os.File itself, or pass the reader here and the
// descriptor with WithInputFd.
func WithInput(r io.Reader) Option {
	return func(c *config) {
		c.in = r
		// An anonymous interface rather than a check for *os.File, so that a
		// wrapper which keeps the descriptor works too.
		if f, ok := r.(interface{ Fd() uintptr }); ok {
			c.inFd = int(f.Fd())
		}
	}
}

// WithInputFd reads keys from the given file descriptor.
//
// This is for a terminal that the caller holds as a descriptor rather than as
// a file, which is the one case WithInput cannot express. A negative
// descriptor means standard input.
func WithInputFd(fd int) Option {
	return func(c *config) { c.inFd = fd }
}

// WithPrompt sets the marker written after the prompt text, and the one used
// on the lines after the first. An empty continuation marker repeats the
// first.
//
// The marker is not the prompt. ReadLine is handed the prompt text for that
// one line, and the marker is what is drawn after it, so a prompt text of
// "sql" and a marker of "> " are shown as "sql> ".
func WithPrompt(marker, continuation string) Option {
	return func(c *config) {
		c.promptMarker = marker
		c.cpromptMarker = continuation
		if continuation == "" {
			c.cpromptMarker = marker
		}
	}
}

// WithHistoryFile keeps the history in the named file, so that it outlives
// the program. An empty name keeps it only while the program runs.
func WithHistoryFile(fname string) Option {
	return func(c *config) { c.historyFile = fname }
}

// WithHistoryLimit holds at most n entries. A limit of zero or less turns the
// history off, so nothing is remembered between lines and the arrow keys have
// nothing to walk through.
func WithHistoryLimit(n int) Option {
	return func(c *config) { c.historyEntries = max(n, 0) }
}

// WithHistory turns the history on or off, keeping whatever file and limit
// were set.
func WithHistory(enabled bool) Option {
	return func(c *config) {
		if enabled {
			if c.historyEntries <= 0 {
				c.historyEntries = DefaultHistoryEntries
			}
			return
		}
		c.historyEntries = 0
	}
}

// WithHighlighter marks up each line as it is typed.
func WithHighlighter(h Highlighter) Option {
	return func(c *config) { c.highlighter = h }
}

// WithCompleter offers completions for the word at the cursor.
func WithCompleter(completer Completer) Option {
	return func(c *config) { c.completer = completer }
}

// WithContinue decides whether Enter finishes the line or starts another row
// inside it.
//
// The function is handed everything typed so far, across every row, and
// answers true when the line is not finished. That is how a prompt keeps
// reading until a statement is closed, and it keeps the whole statement in one
// buffer, so the cursor can be moved between its rows and the whole thing is
// handed back at once.
//
// Without this, Enter always finishes the line.
func WithContinue(fn func(line string) bool) Option {
	return func(c *config) { c.isIncomplete = fn }
}

// WithColor writes color when the terminal supports it. Turning it off writes
// none, whatever the terminal supports.
func WithColor(enabled bool) Option {
	return func(c *config) { c.noColor = !enabled }
}

// WithBeep beeps where the editor would. Turning it off stays quiet.
func WithBeep(enabled bool) Option {
	return func(c *config) { c.silent = !enabled }
}

// WithMultiline allows line breaks inside one line. Turning it off refuses
// them, so a line is always one row.
func WithMultiline(enabled bool) Option {
	return func(c *config) { c.singlelineOnly = !enabled }
}

// WithHighlighting marks up the line. Turning it off draws it plainly, and
// the highlighter is not called.
func WithHighlighting(enabled bool) Option {
	return func(c *config) { c.noHighlight = !enabled }
}

// WithBraceMatching highlights the partner of the brace at the cursor.
func WithBraceMatching(enabled bool) Option {
	return func(c *config) { c.noBraceMatch = !enabled }
}

// WithBraceInsertion closes a brace automatically when one is typed.
func WithBraceInsertion(enabled bool) Option {
	return func(c *config) { c.opts.NoAutoBrace = !enabled }
}

// WithHints shows the rest of the only completion that fits, in grey after
// the cursor.
func WithHints(enabled bool) Option {
	return func(c *config) { c.noHint = !enabled }
}

// WithInlineHelp shows the short reminder below the line while searching the
// history.
func WithInlineHelp(enabled bool) Option {
	return func(c *config) { c.noHelp = !enabled }
}

// WithMultilineIndent lines the rows after the first up under the prompt.
func WithMultilineIndent(enabled bool) Option {
	return func(c *config) { c.noMultilineIndent = !enabled }
}

// WithAutoTab keeps completing while there is only one answer.
func WithAutoTab(enabled bool) Option {
	return func(c *config) { c.completeAutoTab = enabled }
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

// WithLog records everything the reader reads from the keyboard and writes to
// the terminal, so that a session can be read back afterwards by someone who
// was not watching it.
//
// Every line of the log says which direction it went: "<" for what was written
// to the terminal, ">" for a key that was read, and "=" for a finished line.
// The bytes are escaped so that the log can be read by eye, with the escape
// byte written as "\e".
func WithLog(w io.Writer) Option {
	return func(c *config) { c.log = w }
}

// New returns a Session.
//
// It returns a Session even when there is no terminal to edit on, such as when
// the input is a pipe. ReadLine then reads a plain line with no editing, which
// is what the C does and what a program reading a script expects.
func New(opts ...Option) (*Prompt, error) {
	c := &config{
		inFd:           -1,
		out:            os.Stdout,
		promptMarker:   DefaultPromptMarker,
		cpromptMarker:  DefaultPromptMarker,
		historyEntries: DefaultHistoryEntries,
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

	in := io.Reader(os.Stdin)
	if c.in != nil {
		in = c.in
	}
	r := &Session{plain: bufio.NewReader(in)}

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
	// The log sits between the reader and the terminal. The size and the
	// question of whether the output is a terminal are still asked of the real
	// output, not of the log.
	var slog *sessionLog
	out := c.out
	if c.log != nil {
		slog = &sessionLog{w: c.log}
		out = logWriter{w: c.out, log: slog}
		if ttyErr == nil {
			t.src = logReader{src: t.src, log: slog}
		}
	}
	tm := newTerm(out, termOptions{
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
		isIncomplete:      c.isIncomplete,
		opts:              c.opts,
		noMultilineIndent: c.noMultilineIndent,
		noHighlight:       c.noHighlight,
		noBraceMatch:      c.noBraceMatch,
		noHint:            c.noHint,
		noHelp:            c.noHelp,
		singlelineOnly:    c.singlelineOnly,
		completeAutoTab:   c.completeAutoTab,
		hintDelay:         c.hintDelay,
	}
	r.log = slog
	r.noEdit = ttyErr != nil || !isInteractive()
	return &Prompt{Session: r, markup: &MarkupWriter{env: r.env}}, nil //nolint:nilerr // a missing keyboard is a mode, not a failure
}

// writesToTerminal reports whether w is a terminal. Anything that is not a
// file is taken to be one, since a caller that passes its own writer has said
// where the output goes and is not redirecting it by accident.
//
// This asks fileIsTerminal rather than isATTY, because the two questions are
// not the same one. isATTY asks whether there is a keyboard, and on Windows
// it answers about the standard input whatever it is handed, since a console
// is reached there by handle rather than by descriptor. Asking it about the
// output gave the right answer on Unix and the wrong one on Windows, where a
// program with its output redirected wrote escape sequences into the file.
func writesToTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return true
	}
	return fileIsTerminal(f)
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
//
// It returns ErrInterrupted when the user abandoned the line, which is Ctrl-C
// or Ctrl-G. This is a deliberate departure: the C clears the line and hands
// back an empty string, so a caller cannot tell an abandoned line from Enter
// on an empty one, and a shell has to. See PLAN.md.
func (s *Session) ReadLine(prompt string) (string, error) {
	if s.closed {
		return "", ErrClosed
	}
	if s.noEdit {
		return s.readPlain(prompt)
	}
	line, ok, err := s.env.readLine(prompt)
	if err != nil {
		if errors.Is(err, ErrInterrupted) {
			s.log.note(logLine, "<interrupted>")
		}
		return "", err
	}
	if !ok {
		s.log.note(logLine, "<end of input>")
		return "", io.EOF
	}
	s.log.note(logLine, line)
	return line, nil
}

// readPlain reads a line with no editing, for when there is no terminal to
// edit on.
//
// The prompt is written only when there is a keyboard, which means the user is
// typing at a terminal that cannot be edited on. When the input is a pipe
// there is nobody to prompt and the prompt would only dirty the output, so it
// is left out. That is what the C does as well.
func (s *Session) readPlain(prompt string) (string, error) {
	if s.env != nil && s.env.tty != nil {
		s.env.term.write(prompt)
		s.env.term.write(s.env.promptMarker)
		s.env.term.flush()
	}
	line, err := s.plain.ReadString('\n')
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

// Close puts the terminal back as it was. A Session cannot be used afterwards.
func (s *Session) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.env == nil {
		return nil
	}
	s.env.term.free()
	if s.env.tty == nil {
		return nil
	}
	if err := s.env.tty.close(); err != nil {
		return fmt.Errorf("closing the terminal: %w", err)
	}
	return nil
}

// SetCompleter changes the function that offers completions.
//
// A program whose completions depend on something that changes while it runs
// sets a new completer rather than building a new reader: usql replaces its
// completer when the connection changes, so that the words offered come from
// the database that is actually open. A nil completer offers nothing.
func (s *Session) SetCompleter(completer Completer) {
	if s.env == nil || s.env.completions == nil {
		return
	}
	s.env.completions.setCompleter(completer, nil)
}

// SetPrompt changes the marker written after the prompt text, and the one used
// for the lines after the first.
//
// A program that reads a statement over several calls changes the marker
// between them, so that the first line is asked for differently from the ones
// that carry on. An empty continuation repeats the first.
func (s *Session) SetPrompt(marker, continuation string) {
	if s.env == nil {
		return
	}
	if continuation == "" {
		continuation = marker
	}
	s.env.promptMarker = marker
	s.env.cpromptMarker = continuation
}

// Write writes plain text to the terminal, with no markup in it.
//
// This is what to use for anything that came from the user or from a file,
// because Print would read a bracket in it as a tag: "a[b]c" printed as markup
// comes out as "ac". A Session is therefore an io.Writer, so fmt.Fprintf works
// on it.
//
// A program with its own output to write should write it here rather than to
// os.Stdout, because the terminal this goes through is the one that knows
// where the prompt is. An adapter that has to offer an io.Writer can return
// the Session itself.
func (s *Session) Write(p []byte) (int, error) {
	if s.env == nil {
		return len(p), nil
	}
	s.env.term.writeBytes(p)
	s.env.term.flush()
	return len(p), nil
}

// WriteString writes plain text without making a byte slice of it first,
// which is what io.StringWriter asks for and what the markup writer beside
// this one already does.
func (s *Session) WriteString(text string) (int, error) {
	if s.env == nil {
		return len(text), nil
	}
	s.env.term.write(text)
	s.env.term.flush()
	return len(text), nil
}

// MarkupWriter writes markup to a terminal, such as "[red]text[/red]".
//
// A Session writes plain text, because most of what a program writes came from
// a user or a file and a bracket in it is not a tag. This is the other half:
// everything written here is read as markup.
//
// A MarkupWriter with nowhere to write throws away what it is given rather than
// failing, which is what a program with no terminal gets. So a MarkupWriter is
// never nil and never has to be checked before it is used.
type MarkupWriter struct {
	// env holds the terminal and the styles. It is nil when there is
	// nowhere to write.
	env *env
}

// Write satisfies io.Writer, reading what is written as markup.
//
// Use fmt to do the formatting:
//
//	fmt.Fprintf(p.Markup(), "[ic-error]%s[/]\n", msg)
func (w *MarkupWriter) Write(p []byte) (int, error) {
	if w == nil || w.env == nil {
		return len(p), nil
	}
	w.writeMarkup(string(p))
	return len(p), nil
}

// WriteString writes markup without making a byte slice of it first.
func (w *MarkupWriter) WriteString(s string) (int, error) {
	if w == nil || w.env == nil {
		return len(s), nil
	}
	w.writeMarkup(s)
	return len(s), nil
}

// writeMarkup draws s, keeping a newline at the end of it out of whatever
// attributes the markup left open.
//
// The split is not tidiness. The drawing writes each run of text with the
// attributes that run carries and resets only once the text is done, so a
// newline inside the last run goes out while those attributes are still set.
// With a background colour left open that fills the rest of the row, which
// is the colour bleed a terminal shows at the end of a line. Println used to
// hand the text and the newline over separately, so the reset came first;
// fmt.Fprintln cannot, because it appends the newline to the string. So it
// is taken off again here.
//
// Only a newline at the end is moved. One in the middle of the string was
// written inside the run before this as well, and matching what was there is
// the point.
func (w *MarkupWriter) writeMarkup(s string) {
	if after, ok := strings.CutSuffix(s, "\n"); ok {
		w.env.bb.println(after)
	} else {
		w.env.bb.print(s)
	}
	w.env.term.flush()
}

// Prompt is a Session and the markup writer that goes with it.
//
// The reading methods are promoted, so a Prompt is used like a Session:
// ReadLine, Password and Close all work on it directly. Markup is reached
// through Markup, and a program that wants none simply never calls it.
//
// A Prompt is an io.Writer as well, through the Session it holds, and writes
// plain text that way. That is deliberate: fmt.Fprintln(p, s) writes what a
// program has to say through the terminal that knows where the prompt is,
// without reading a bracket in it as a tag.
type Prompt struct {
	*Session

	// markup writes styled output, and is never nil.
	markup *MarkupWriter
}

// Markup returns the writer that reads what is written to it as markup.
//
// It is never nil. When there is no terminal, or when colour is off, what is
// written to it is thrown away or written plainly, so a caller never has to
// ask whether markup is on before using it.
func (p *Prompt) Markup() *MarkupWriter {
	if p == nil || p.markup == nil {
		return &MarkupWriter{}
	}
	return p.markup
}

// DefineStyle gives a name to a set of attributes, so that markup can use it.
// The spec is written the way the inside of a tag is, such as "bold color=red".
func (s *Session) DefineStyle(name, spec string) {
	if s.env == nil {
		return
	}
	s.env.bb.styleDef(name, spec)
}

// SetHighlighter changes the function that marks up the line.
//
// A nil highlighter draws the line plainly.
func (s *Session) SetHighlighter(h Highlighter) {
	if s.env == nil {
		return
	}
	s.env.highlighter = h
}

// LoadHistory reads the history back from its file, throwing away what is
// held.
//
// New does this already. This is for a program that wants what another
// process has written since, or that has changed the file with
// SetHistoryFile.
//
// It takes no file name, and SaveHistory takes none either: which file the
// history lives in is one setting, not two arguments that could disagree.
func (s *Session) LoadHistory() error {
	if s.env == nil || s.env.history == nil {
		return nil
	}
	s.env.history.loadFrom(s.env.history.fname, s.env.history.max)
	return nil
}

// SetHistoryFile changes the file the history is kept in. It does not read
// the new file; call LoadHistory for that.
func (s *Session) SetHistoryFile(fname string) {
	if s.env == nil || s.env.history == nil {
		return
	}
	s.env.history.fname = fname
}

// History returns the entries, newest first.
//
// The slice is a fresh one each time and is the caller's to keep or change;
// changing it does not change the history.
//
// A program that wants to show the history, search it its own way, or write
// it somewhere else needs to be able to read it, and adding, saving and
// clearing were the only ways to touch it.
func (s *Session) History() []string {
	if s.env == nil || s.env.history == nil {
		return nil
	}
	return s.env.history.all()
}

// AddHistory adds an entry to the history.
func (s *Session) AddHistory(entry string) {
	if s.env == nil {
		return
	}
	s.env.history.push(entry)
}

// ClearHistory empties the history.
func (s *Session) ClearHistory() {
	if s.env == nil {
		return
	}
	s.env.history.clear()
}

// SaveHistory writes the history to the file it was given, if it was given one.
func (s *Session) SaveHistory() error {
	if s.env == nil {
		return nil
	}
	if err := s.env.history.save(); err != nil {
		return fmt.Errorf("saving the history: %w", err)
	}
	return nil
}

// Interactive reports whether there is a terminal to edit on. When there is
// not, ReadLine reads a plain line.
func (s *Session) Interactive() bool {
	return !s.noEdit
}

// --------------------------------------------------------------------------
// logging.go

// Recording a session.
//
// A log holds every byte the reader wrote to the terminal and every byte it
// read from the keyboard, in a form that can be read by eye and turned back
// into the original bytes. It is what lets a session be reconstructed
// afterwards by someone who was not watching it.

// Directions, written at the start of every line of a log.
const (
	// logWrite marks bytes the reader wrote to the terminal.
	logWrite = "<"

	// logRead marks bytes the reader read from the keyboard.
	logRead = ">"

	// logLine marks a finished line, as it was handed back to the caller.
	logLine = "="
)

// sessionLog writes what a reader read and wrote.
//
// It is safe for use from more than one goroutine, because the reading and the
// writing can happen at the same time.
type sessionLog struct {
	mu sync.Mutex
	w  io.Writer
}

// record writes one entry.
func (l *sessionLog) record(direction string, b []byte) {
	if l == nil || len(b) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.w, "%s %s\n", direction, escapeBytes(b))
}

// note writes one entry for text that is not raw bytes, such as a finished
// line.
func (l *sessionLog) note(direction, s string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.w, "%s %s\n", direction, escapeBytes([]byte(s)))
}

// escapeBytes renders bytes so that a person can read them and a program can
// turn them back.
//
// The escape byte becomes "\e", which is what makes a log of terminal output
// readable at all, and the other control bytes become "\xNN". Bytes above 0x7f
// pass through, so that text outside ASCII stays legible.
func escapeBytes(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/4)
	for _, c := range b {
		switch c {
		case 0x1b:
			sb.WriteString(`\e`)
		case '\\':
			sb.WriteString(`\\`)
		case '\r':
			sb.WriteString(`\r`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&sb, `\x%02x`, c)
				continue
			}
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// logWriter writes to a terminal and records what it wrote.
type logWriter struct {
	w   io.Writer
	log *sessionLog
}

// Write satisfies io.Writer.
func (t logWriter) Write(p []byte) (int, error) {
	t.log.record(logWrite, p)
	n, err := t.w.Write(p)
	if err != nil {
		return n, fmt.Errorf("writing to the terminal: %w", err)
	}
	return n, nil
}

// logReader reads keys from a terminal and records what it read.
type logReader struct {
	src byteReader
	log *sessionLog
}

// readByte satisfies byteReader.
func (t logReader) readByte(timeout time.Duration) (byte, bool) {
	c, ok := t.src.readByte(timeout)
	if ok {
		t.log.record(logRead, []byte{c})
	}
	return c, ok
}
