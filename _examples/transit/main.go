// Command transit is a prompt that highlights and completes code with
// transit, the pure Go port of tree-sitter.
//
// It is the test of the transit API for rline. After each key, the tree is
// told what changed and parsed again with the old tree, and the highlight
// query of the grammar marks the result. Tab offers what the parser can
// accept at the cursor. The offer comes from the parse state at the start of
// the word, and not from a list of words.
//
// The example is a module of its own, so that rline gets no new dependency
// (rline D7). transit has no tag yet, so go.mod takes rline and transit from
// the checkouts beside this one. Run it from this folder:
//
//	go run . -lang python
//
// The languages are bash, c, go, javascript, json, python, ruby and rust.
// Enter finishes the input when it parses with no error and no empty node,
// or when the last row is empty. Ctrl-J starts a new row. Ctrl-C gives up the input, and
// Ctrl-D on an empty row leaves.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xo/rline"
	"github.com/xo/transit"
	"github.com/xo/transit/grammars/bash"
	"github.com/xo/transit/grammars/c"
	golang "github.com/xo/transit/grammars/go"
	"github.com/xo/transit/grammars/javascript"
	"github.com/xo/transit/grammars/json"
	"github.com/xo/transit/grammars/python"
	"github.com/xo/transit/grammars/ruby"
	"github.com/xo/transit/grammars/rust"
)

// grammar is what the example takes from one grammar package.
type grammar struct {
	language func() *transit.Language
	queries  fs.FS
	keywords func() []string
}

// grammars are the languages that -lang chooses from. Every grammar package
// has the same three exports, so each one is a line here.
var grammars = map[string]grammar{
	"bash":       {bash.Language, bash.Queries, bash.Keywords},
	"c":          {c.Language, c.Queries, c.Keywords},
	"go":         {golang.Language, golang.Queries, golang.Keywords},
	"javascript": {javascript.Language, javascript.Queries, javascript.Keywords},
	"json":       {json.Language, json.Queries, json.Keywords},
	"python":     {python.Language, python.Queries, python.Keywords},
	"ruby":       {ruby.Language, ruby.Queries, ruby.Keywords},
	"rust":       {rust.Language, rust.Queries, rust.Keywords},
}

// styles are the rline styles that the example defines, in the bright ANSI
// colors, so that each kind of capture is plain on any terminal. transit
// has no shared colors yet (transit D65), so the example chooses them.
var styles = map[string]string{
	"ts-keyword":   "bold ansi-red",
	"ts-type":      "ansi-yellow",
	"ts-function":  "ansi-aqua",
	"ts-string":    "ansi-lime",
	"ts-escape":    "bold ansi-lime",
	"ts-constant":  "bold ansi-fuchsia",
	"ts-comment":   "ansi-gray",
	"ts-operator":  "ansi-silver",
	"ts-property":  "ansi-teal",
	"ts-parameter": "ansi-olive",
}

// captureStyles gives the style of a capture name. A name with no entry
// takes the entry of its first parts, so "function.method" takes the style
// of "function". A name that reaches no entry is not styled.
var captureStyles = map[string]string{
	"keyword":             "ts-keyword",
	"tag":                 "ts-keyword",
	"type":                "ts-type",
	"constructor":         "ts-type",
	"module":              "ts-type",
	"function":            "ts-function",
	"string":              "ts-string",
	"escape":              "ts-escape",
	"string.escape":       "ts-escape",
	"number":              "ts-constant",
	"boolean":             "ts-constant",
	"constant":            "ts-constant",
	"variable.builtin":    "ts-constant",
	"comment":             "ts-comment",
	"operator":            "ts-operator",
	"punctuation.special": "ts-operator",
	"property":            "ts-property",
	"attribute":           "ts-property",
	"label":               "ts-property",
	"variable.parameter":  "ts-parameter",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	names := slices.Sorted(func(yield func(string) bool) {
		for name := range grammars {
			if !yield(name) {
				return
			}
		}
	})
	lang := flag.String("lang", "python", "the language: "+strings.Join(names, ", "))
	showTree := flag.Bool("tree", true, "print the tree of each input")
	flag.Parse()
	g, ok := grammars[*lang]
	if !ok {
		return fmt.Errorf("no grammar %q: choose one of %s", *lang, strings.Join(names, ", "))
	}
	s, err := newSyntax(g)
	if err != nil {
		return err
	}
	p, err := rline.New(
		rline.WithPrompt("> ", "| "),
		rline.WithHighlighter(s),
		rline.WithCompleter(s),
		rline.WithContinue(s.unfinished),
	)
	if err != nil {
		return fmt.Errorf("starting the reader: %w", err)
	}
	defer func() { _ = p.Close() }()
	for name, spec := range styles {
		p.DefineStyle(name, spec)
	}
	out := p.Markup()
	_, _ = fmt.Fprintf(out, "[b]rline[/b] with transit, highlighting and completing [b]%s[/b].\n", *lang)
	_, _ = fmt.Fprintln(out, "[ic-info]Tab offers what the parser accepts at the cursor. Enter finishes input that parses, or an empty row.[/]")
	_, _ = fmt.Fprintln(out, "[ic-info]Ctrl-J starts a new row, Ctrl-C gives the input up, and Ctrl-D on an empty row leaves.[/]")
	// When there is no terminal to edit on, ReadLine hands back one row at a
	// time and never asks WithContinue, so the rows are joined here with the
	// same rule.
	var pending strings.Builder
	for {
		text, err := p.ReadLine(*lang)
		switch {
		case errors.Is(err, io.EOF):
			_, _ = fmt.Fprintln(p)
			return nil
		case errors.Is(err, rline.ErrInterrupted):
			pending.Reset()
			continue
		case err != nil:
			return fmt.Errorf("reading the input: %w", err)
		}
		if !p.Interactive() {
			if pending.Len() > 0 {
				pending.WriteByte('\n')
			}
			pending.WriteString(text)
			if text = pending.String(); s.unfinished(text) {
				continue
			}
			pending.Reset()
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		tree, stats, err := s.finish(text)
		if err != nil {
			return err
		}
		if *showTree {
			_, _ = fmt.Fprintln(p, tree)
		}
		_, _ = fmt.Fprintf(out, "[ic-info]%s[/]\n", stats)
	}
}

// syntax keeps the parse of the text being edited. It is the highlighter, the
// completer and the test of whether the input is finished, so that all three
// read the same tree.
//
// rline hands the highlighter the whole text, and not the edit that made it.
// So the edit is found by comparing the text with the text of the last parse.
type syntax struct {
	mu sync.Mutex

	language *transit.Language
	parser   *transit.Parser
	query    *transit.Query
	cursor   *transit.QueryCursor
	keywords map[string]bool

	src  []byte
	tree *transit.Tree

	// The time each edit took, from the edit of the tree to the last
	// capture, since the last input was read.
	edits   int
	total   time.Duration
	slowest time.Duration
}

// newSyntax makes a parser for the grammar and compiles its highlight query
// once, as transit asks.
func newSyntax(g grammar) (*syntax, error) {
	language := g.language()
	parser := transit.NewParser()
	if err := parser.SetLanguage(language); err != nil {
		return nil, fmt.Errorf("setting the language %s: %w", language.Name(), err)
	}
	source, err := fs.ReadFile(g.queries, "queries/highlights.scm")
	if err != nil {
		return nil, fmt.Errorf("reading the highlight query of %s: %w", language.Name(), err)
	}
	query, err := transit.NewQuery(language, string(source))
	if err != nil {
		return nil, fmt.Errorf("compiling the highlight query of %s: %w", language.Name(), err)
	}
	keywords := make(map[string]bool)
	for _, k := range g.keywords() {
		keywords[k] = true
	}
	return &syntax{
		language: language,
		parser:   parser,
		query:    query,
		cursor:   transit.NewQueryCursor(),
		keywords: keywords,
	}, nil
}

// update parses text, with the tree of the last parse told what changed. It
// does nothing when the text is the text of the last parse, which it is on
// every redraw that no key caused.
func (s *syntax) update(ctx context.Context, text string) error {
	if s.tree != nil && string(s.src) == text {
		return nil
	}
	src := []byte(text)
	if s.tree != nil {
		s.tree.Edit(editBetween(s.src, src))
	}
	tree, err := s.parser.Parse(ctx, src, s.tree)
	if err != nil {
		return fmt.Errorf("parsing: %w", err)
	}
	s.src, s.tree = src, tree
	return nil
}

// Highlight satisfies rline.Highlighter. It parses the text again from the
// last tree and marks each capture of the highlight query.
func (s *syntax) Highlight(l *rline.LineStyle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sp := range s.spans(context.Background(), l.Text()) {
		l.Style(sp.start, sp.end-sp.start, sp.style)
	}
}

// span is a stretch of the text and the style it is drawn in.
type span struct {
	start, end int
	style      string
}

// spans parses text and returns the styled stretches of it, in the order of
// the text. A later span is inside an earlier one or after it, so drawing
// them in order leaves the innermost style on top.
func (s *syntax) spans(ctx context.Context, text string) []span {
	start := time.Now()
	edited := s.tree == nil || string(s.src) != text
	if err := s.update(ctx, text); err != nil {
		return nil
	}
	// Two captures of one node are settled as the highlighter of upstream
	// settles them. The captures of a node come in the order of their
	// patterns, and the last one wins. A node that covers the same bytes as
	// the node before it, such as the only child of a node, keeps the style
	// of the first.
	names := s.query.CaptureNames()
	var out []span
	lastStart, lastEnd := -1, -1
	apply := func(c transit.QueryCapture) {
		start, end := c.Node.StartByte(), c.Node.EndByte()
		if start == lastStart && end == lastEnd {
			return
		}
		lastStart, lastEnd = start, end
		if style := styleFor(names[c.Index]); style != "" && end > start {
			out = append(out, span{start, end, style})
		}
	}
	s.cursor.SetByteRange(0, len(s.src))
	var held transit.QueryCapture
	var holding bool
	for m, i := range s.cursor.Captures(ctx, s.query, s.tree.RootNode(), s.src) {
		c := m.Captures[i]
		if holding && c.Node.Equal(held.Node) {
			held = c
			continue
		}
		if holding {
			apply(held)
		}
		held, holding = c, true
	}
	if holding {
		apply(held)
	}
	if edited {
		d := time.Since(start)
		s.edits++
		s.total += d
		s.slowest = max(s.slowest, d)
	}
	return out
}

// styleFor returns the style of a capture name, or "" for none. A name that
// starts with an underscore belongs to a predicate and is never a highlight.
func styleFor(name string) string {
	if strings.HasPrefix(name, "_") {
		return ""
	}
	for {
		if style, ok := captureStyles[name]; ok {
			return style
		}
		i := strings.LastIndexByte(name, '.')
		if i < 0 {
			return ""
		}
		name = name[:i]
	}
}

// Complete satisfies rline.Completer. It offers each symbol that the parser
// can accept at the start of the word before the cursor.
func (s *syntax) Complete(c *rline.Completion, _ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cand := range s.candidates(context.Background(), c.Text(), c.Cursor()) {
		if !c.AddCandidate(cand) {
			return
		}
	}
}

// candidates returns what the parser can accept at the start of the word
// that ends at cursor: the keywords first, then the other tokens, each
// sorted.
//
// A keyword or another token with fixed text goes into the line. A named
// symbol, such as an identifier, has no fixed text, so it is shown in angle
// brackets and inserts nothing. It is offered only when no word is typed,
// because it cannot match one. The help line below the menu names the nodes
// around the cursor.
func (s *syntax) candidates(ctx context.Context, text string, cursor int) []rline.Candidate {
	if err := s.update(ctx, text); err != nil {
		return nil
	}
	start := wordStart(text, cursor)
	word := text[start:cursor]
	// transit D70: to complete a word, ask for the states at its start.
	states, err := s.parser.StatesAt(ctx, s.src, start, s.tree)
	if err != nil {
		return nil
	}
	var keywords, tokens, nodes []string
	seen := make(map[string]bool)
	for _, state := range states {
		it, ok := s.language.LookaheadIterator(state)
		if !ok {
			continue
		}
		for sym := range it.Symbols() {
			name := s.language.SymbolName(sym)
			if seen[name] || !printable(name) {
				continue
			}
			seen[name] = true
			switch s.language.SymbolType(sym) {
			case transit.SymbolAnonymous:
				switch {
				case !strings.HasPrefix(name, word):
				case s.keywords[name]:
					keywords = append(keywords, name)
				default:
					tokens = append(tokens, name)
				}
			case transit.SymbolRegular:
				if word == "" && !strings.HasPrefix(name, "_") {
					nodes = append(nodes, name)
				}
			}
		}
	}
	where := s.context(start)
	var out []rline.Candidate
	add := func(names []string, kind string, fixed bool) {
		slices.Sort(names)
		for _, name := range names {
			cand := rline.Candidate{
				Replacement:  name,
				Help:         kind + where,
				DeleteBefore: len(word),
			}
			if !fixed {
				cand.Replacement = word
				cand.Display = "<" + name + ">"
			}
			out = append(out, cand)
		}
	}
	add(keywords, "keyword", true)
	add(tokens, "token", true)
	add(nodes, "node", false)
	return out
}

// context names the nodes around offset, from the outermost of the last
// three to the innermost, each with the field that holds the next one.
func (s *syntax) context(offset int) string {
	n, ok := s.tree.RootNode().DescendantForByteRange(offset, offset)
	if !ok {
		return ""
	}
	var parts []string
	for {
		parent, ok := n.Parent()
		if !ok {
			break
		}
		part := parent.Kind()
		if field := fieldOf(parent, n); field != "" {
			part += "." + field
		}
		parts = append(parts, part)
		n = parent
	}
	if len(parts) == 0 {
		return ""
	}
	parts = parts[:min(len(parts), 3)]
	slices.Reverse(parts)
	return ", in " + strings.Join(parts, " > ")
}

// fieldOf returns the name of the field of parent that holds child, or "" for
// a child that no field holds.
func fieldOf(parent, child transit.Node) string {
	for i := range parent.ChildCount() {
		if n, ok := parent.Child(i); ok && n.Equal(child) {
			return parent.FieldNameForChild(i)
		}
	}
	return ""
}

// unfinished says whether Enter starts a new row. It does while the input
// does not parse, or while it holds a named node with no text, as the body
// of "def f(x):" is in python, whose scanner closes the block at the end of
// the text. An empty last row always finishes the input, so that input with
// an error can still be read.
func (s *syntax) unfinished(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.update(context.Background(), text); err != nil {
		return false
	}
	last := text[strings.LastIndexByte(text, '\n')+1:]
	if strings.TrimSpace(last) == "" {
		return false
	}
	root := s.tree.RootNode()
	return root.HasError() || hasEmptyNode(root)
}

// hasEmptyNode says whether a named node below n covers no text.
func hasEmptyNode(n transit.Node) bool {
	for c := range n.NamedChildren() {
		if c.StartByte() == c.EndByte() || hasEmptyNode(c) {
			return true
		}
	}
	return false
}

// finish returns the tree of the input that was read, and what the edits
// that built it cost, and starts the count again for the next input.
func (s *syntax) finish(text string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.update(context.Background(), text); err != nil {
		return "", "", err
	}
	stats := "no edit was parsed"
	if s.edits > 0 {
		stats = fmt.Sprintf("%d edits parsed and highlighted, %s on average, %s at the most",
			s.edits, (s.total / time.Duration(s.edits)).Round(time.Microsecond),
			s.slowest.Round(time.Microsecond))
	}
	s.edits, s.total, s.slowest = 0, 0, 0
	return s.tree.RootNode().String(), stats, nil
}

// editBetween describes the edit that turns old into new, as the one range
// between the bytes that the two share at the start and at the end. Neither
// end is put inside a character, because the parser lexes from there.
func editBetween(old, new []byte) transit.InputEdit {
	start := 0
	for start < len(old) && start < len(new) && old[start] == new[start] {
		start++
	}
	for start > 0 && start < len(new) && !utf8.RuneStart(new[start]) {
		start--
	}
	end := 0
	for end < len(old)-start && end < len(new)-start && old[len(old)-1-end] == new[len(new)-1-end] {
		end++
	}
	for end > 0 && !utf8.RuneStart(new[len(new)-end]) {
		end--
	}
	e := transit.InputEdit{
		StartByte:  start,
		OldEndByte: len(old) - end,
		NewEndByte: len(new) - end,
	}
	e.StartPoint = pointAt(old, e.StartByte)
	e.OldEndPoint = pointAt(old, e.OldEndByte)
	e.NewEndPoint = pointAt(new, e.NewEndByte)
	return e
}

// pointAt returns the row and the column, in bytes, of offset in src.
func pointAt(src []byte, offset int) transit.Point {
	before := src[:offset]
	return transit.Point{
		Row:    bytes.Count(before, []byte{'\n'}),
		Column: offset - (bytes.LastIndexByte(before, '\n') + 1),
	}
}

// wordStart returns where the word that ends at cursor starts. A word is
// letters, digits and underscores.
func wordStart(text string, cursor int) int {
	for cursor > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:cursor])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		cursor -= size
	}
	return cursor
}

// printable says whether a symbol name can be offered: not empty, and with
// no control character, such as the newline token of bash.
func printable(name string) bool {
	return name != "" && !strings.ContainsFunc(name, unicode.IsControl)
}
