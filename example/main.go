// Command example is a small SQL prompt built on rline.
//
// It shows the two things that are hard to see from the package on its own:
// input that runs over more than one line, and highlighting that changes as
// the line is typed.
//
// A statement ends at a semicolon. Until then Enter starts another line, so a
// statement can be written over as many lines as it takes.
//
// Run it with:
//
//	go run ./example
//
// Do not build it with "go build -o example ./example". The argument names a
// directory that already exists, so the binary lands at example/example rather
// than replacing anything in the current directory, and an older binary of the
// same name goes on running. Use a different name:
//
//	go build -o rlex ./example && ./rlex
//
// Pass -log to record everything read and written, which is the way to see
// what was drawn without watching it happen.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/xo/rline"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// run reads statements until the input ends.
func run() error {
	logPath := flag.String("log", "", "record the session to this file")
	flag.Parse()

	opts := []rline.Option{
		rline.WithPrompt("sql> ", "...> "),
		rline.WithHighlighter(rline.HighlighterFunc(highlight)),
		rline.WithCompleter(rline.CompleterFunc(complete)),
		rline.WithContinue(incomplete),
		rline.WithHistoryLimit(rline.DefaultHistoryEntries), // kept in memory, default size
	}
	if *logPath != "" {
		f, err := os.Create(*logPath)
		if err != nil {
			return fmt.Errorf("opening the log: %w", err)
		}
		defer func() { _ = f.Close() }()
		opts = append(opts, rline.WithLog(f))
	}
	p, err := rline.New(opts...)
	if err != nil {
		return fmt.Errorf("starting the reader: %w", err)
	}
	defer func() { _ = p.Close() }()

	// The built in styles are the muted ones a code editor uses. This example
	// defines its own, in the bright ANSI colors, so that the highlighting is
	// obvious on any terminal and in any theme.
	out := p.Markup()
	p.DefineStyle("sql-keyword", "bold ansi-red")
	p.DefineStyle("sql-type", "bold ansi-yellow")
	p.DefineStyle("sql-const", "bold ansi-fuchsia")
	p.DefineStyle("sql-number", "bold ansi-aqua")
	p.DefineStyle("sql-string", "bold ansi-lime")
	p.DefineStyle("sql-comment", "ansi-gray")

	_, _ = fmt.Fprintln(out, "[b]rline[/b] SQL example.")
	_, _ = fmt.Fprintln(out, "[ic-info]A statement ends at a semicolon. \\q or exit leaves.[/]")
	_, _ = fmt.Fprintln(out, "[ic-info]Enter carries on until then, so up and down move between the rows.[/]")
	_, _ = fmt.Fprintln(out, `[ic-info]\pass reads a password without showing it, then echoes it back.[/]`)

	// When there is no terminal to edit on, ReadLine hands back one line at a
	// time and WithContinue never runs, so the statement is joined here
	// instead. A tool wants the same answer either way.
	var pending strings.Builder
	for {
		line, err := p.ReadLine("")
		if errors.Is(err, io.EOF) {
			// The input ended rather than the user asking to leave, which is
			// what happens when the input is a file or a pipe that ran out.
			_, _ = fmt.Fprintln(out, "")
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading a line: %w", err)
		}
		if !p.Interactive() {
			if pending.Len() > 0 {
				pending.WriteByte('\n')
			}
			pending.WriteString(line)
			line = pending.String()
			if incomplete(line) {
				continue
			}
			pending.Reset()
		}
		text := strings.TrimSpace(line)
		// A command is whatever is on the last row, so it works after a
		// statement has been started. The statement itself is still the whole
		// of what was typed.
		last := text
		if i := strings.LastIndexByte(last, '\n'); i >= 0 {
			last = strings.TrimSpace(last[i+1:])
		}
		if last == `\pass` {
			// Reading something that must not be shown, the way usql asks for
			// a database password.
			pw, err := p.Password("password: ")
			switch {
			case errors.Is(err, rline.ErrInterrupted):
				_, _ = fmt.Fprintln(out, "[ic-info]cancelled[/]")
				continue
			case errors.Is(err, io.EOF):
				return nil
			case err != nil:
				return fmt.Errorf("reading the password: %w", err)
			}
			// Echoed on purpose, so that what Password collected can be
			// checked against what was typed. A real program would not.
			_, _ = fmt.Fprintf(p, "password was %q (%d bytes)\n", pw, len(pw))
			continue
		}
		if text == "" || isQuit(last) {
			if isQuit(last) {
				return nil
			}
			continue
		}
		// Plain, not markup: the statement came from the user and may hold a
		// bracket, which Print would read as a tag.
		_, _ = fmt.Fprintf(p, "ran %d line(s): %s\n",
			strings.Count(text, "\n")+1, strings.ReplaceAll(text, "\n", " "))
	}
}

// isQuit reports whether a line asks to leave.
//
// The last row is what counts, because the whole statement is one line now and
// a person types the word on the row they are on.
func isQuit(text string) bool {
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		text = text[i+1:]
	}
	switch strings.TrimSpace(text) {
	case `\q`, "exit", "quit":
		return true
	}
	return false
}

// incomplete reports whether a statement is unfinished, so that Enter starts
// another row rather than running it.
//
// A statement is finished at a semicolon. A backslash command is finished at
// the end of its row, and so is a person asking to leave, or either would be
// swallowed into a statement that never ends.
func incomplete(line string) bool {
	text := strings.TrimSpace(line)
	if text == "" || isCommand(text) {
		return false
	}
	return !strings.HasSuffix(text, ";")
}

// isCommand reports whether the last row is a backslash command rather than
// part of a statement.
func isCommand(text string) bool {
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		text = text[i+1:]
	}
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, `\`) || isQuit(text)
}

// sqlKeywords are the words the highlighter colors as keywords.
var sqlKeywords = map[string]bool{
	"select": true, "from": true, "where": true, "insert": true, "into": true,
	"values": true, "update": true, "set": true, "delete": true, "join": true,
	"left": true, "right": true, "inner": true, "outer": true, "on": true,
	"group": true, "order": true, "by": true, "having": true, "limit": true,
	"offset": true, "as": true, "distinct": true, "union": true, "create": true,
	"table": true, "drop": true, "alter": true, "index": true,
	// Enough words starting with the same letter that one keystroke fills the
	// completion menu, which is how the column layout can be seen at all.
	"case": true, "cast": true, "coalesce": true, "column": true,
	"constraint": true, "cross": true, "count": true, "check": true,
	"current_date": true, "current_time": true, "current_timestamp": true,
	"cascade": true, "collate": true, "commit": true,
}

// sqlTypes are colored as types.
var sqlTypes = map[string]bool{
	"int": true, "integer": true, "text": true, "varchar": true, "boolean": true,
	"timestamp": true, "date": true, "numeric": true, "serial": true,
}

// sqlConstants are colored as constants.
var sqlConstants = map[string]bool{
	"null": true, "true": true, "false": true, "and": true, "or": true, "not": true,
}

// highlight colors the parts of a SQL statement as it is typed.
//
// It walks the line once, marking each stretch it recognises. Everything it
// does not mark keeps the color of the terminal.
func highlight(h *rline.Highlight, input string) {
	for i := 0; i < len(input); {
		switch {
		case strings.HasPrefix(input[i:], "--"):
			// A comment runs to the end of the line.
			n := strings.IndexByte(input[i:], '\n')
			if n < 0 {
				n = len(input) - i
			}
			h.StyleBytes(i, n, "sql-comment")
			i += n
		case input[i] == '\'':
			// A string runs to the closing quote, or to the end if there is
			// none yet, which is what a half typed string looks like.
			n := strings.IndexByte(input[i+1:], '\'')
			if n < 0 {
				n = len(input) - i
			} else {
				n += 2
			}
			h.StyleBytes(i, n, "sql-string")
			i += n
		case isDigit(input[i]):
			n := 0
			for i+n < len(input) && (isDigit(input[i+n]) || input[i+n] == '.') {
				n++
			}
			h.StyleBytes(i, n, "sql-number")
			i += n
		case isWordByte(input[i]):
			n := 0
			for i+n < len(input) && isWordByte(input[i+n]) {
				n++
			}
			word := strings.ToLower(input[i : i+n])
			switch {
			case sqlKeywords[word]:
				h.StyleBytes(i, n, "sql-keyword")
			case sqlTypes[word]:
				h.StyleBytes(i, n, "sql-type")
			case sqlConstants[word]:
				h.StyleBytes(i, n, "sql-const")
			}
			i += n
		default:
			i++
		}
	}
}

// isDigit reports whether c is a decimal digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isWordByte reports whether c can appear in an identifier.
func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || isDigit(c)
}

// complete offers the SQL words that start with what has been typed.
//
// A completer is handed the prefix to complete and calls Add for each answer.
// This one walks its three word lists, which is enough to show how completion
// is wired up.
func complete(c *rline.Completion, prefix string) {
	// The prefix is only what is in front of the cursor, so completing while
	// the cursor sits inside a word would offer the rest of a word that is
	// already there: with the cursor after "wh" in "where", the answer is
	// "where" and taking it gives "whereere". The whole line says so.
	in := c.Input()
	if in.Cursor < len(in.Text) && isWordByte(in.Text[in.Cursor]) {
		return
	}
	word := prefix
	// Complete the last word rather than the whole line.
	if i := strings.LastIndexAny(word, " \t\n(,"); i >= 0 {
		word = word[i+1:]
	}
	if word == "" {
		return
	}
	lower := strings.ToLower(word)
	var found []string
	for _, set := range []map[string]bool{sqlKeywords, sqlTypes, sqlConstants} {
		for w := range set {
			if strings.HasPrefix(w, lower) && w != lower {
				found = append(found, w)
			}
		}
	}
	sort.Strings(found)
	for _, w := range found {
		// The replacement takes the place of the word already typed, so the
		// part already there is deleted first.
		if !c.AddEntry(rline.Entry{Replacement: w, DeleteBefore: len(word)}) {
			return
		}
	}
}
