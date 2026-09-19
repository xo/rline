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
		rline.WithHighlighter(highlight),
		rline.WithCompleter(complete),
		rline.WithContinue(incomplete),
		rline.WithHistory("", 0),
	}
	if *logPath != "" {
		f, err := os.Create(*logPath)
		if err != nil {
			return fmt.Errorf("opening the log: %w", err)
		}
		defer func() { _ = f.Close() }()
		opts = append(opts, rline.WithLog(f))
	}
	r, err := rline.New(opts...)
	if err != nil {
		return fmt.Errorf("starting the reader: %w", err)
	}
	defer func() { _ = r.Close() }()

	// The built in styles are the muted ones a code editor uses. This example
	// defines its own, in the bright ANSI colors, so that the highlighting is
	// obvious on any terminal and in any theme.
	r.DefineStyle("sql-keyword", "bold ansi-red")
	r.DefineStyle("sql-type", "bold ansi-yellow")
	r.DefineStyle("sql-const", "bold ansi-fuchsia")
	r.DefineStyle("sql-number", "bold ansi-aqua")
	r.DefineStyle("sql-string", "bold ansi-lime")
	r.DefineStyle("sql-comment", "ansi-gray")

	r.Println("[b]rline[/b] SQL example.")
	r.Println("[ic-info]A statement ends at a semicolon. \\q or exit leaves.[/]")
	r.Println("[ic-info]Enter carries on until then, so up and down move between the rows.[/]")
	r.Println(`[ic-info]\pass reads a password without showing it, then echoes it back.[/]`)

	// When there is no terminal to edit on, ReadLine hands back one line at a
	// time and WithContinue never runs, so the statement is joined here
	// instead. A tool wants the same answer either way.
	var pending strings.Builder
	for {
		line, err := r.ReadLine("")
		if errors.Is(err, io.EOF) {
			// The input ended rather than the user asking to leave, which is
			// what happens when the input is a file or a pipe that ran out.
			r.Println("")
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading a line: %w", err)
		}
		if !r.Interactive() {
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
		if i := strings.LastIndexByte(text, '\n'); i >= 0 {
			// A command is whatever is on the last row, so it works after a
			// statement has been started.
			text = strings.TrimSpace(text[i+1:])
		}
		if text == `\pass` {
			// Reading something that must not be shown, the way usql asks for
			// a database password.
			pw, err := r.Password("password: ")
			switch {
			case errors.Is(err, rline.ErrInterrupted):
				r.Println("[ic-info]cancelled[/]")
				continue
			case errors.Is(err, io.EOF):
				return nil
			case err != nil:
				return fmt.Errorf("reading the password: %w", err)
			}
			// Echoed on purpose, so that what Password collected can be
			// checked against what was typed. A real program would not.
			_, _ = fmt.Fprintf(r, "password was %q (%d bytes)\n", pw, len(pw))
			continue
		}
		if text == "" || isQuit(text) {
			if isQuit(text) {
				return nil
			}
			continue
		}
		// Plain, not markup: the statement came from the user and may hold a
		// bracket, which Print would read as a tag.
		_, _ = fmt.Fprintf(r, "ran %d line(s): %s\n",
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
			h.Style(i, n, "sql-comment")
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
			h.Style(i, n, "sql-string")
			i += n
		case isDigit(input[i]):
			n := 0
			for i+n < len(input) && (isDigit(input[i+n]) || input[i+n] == '.') {
				n++
			}
			h.Style(i, n, "sql-number")
			i += n
		case isWordByte(input[i]):
			n := 0
			for i+n < len(input) && isWordByte(input[i+n]) {
				n++
			}
			word := strings.ToLower(input[i : i+n])
			switch {
			case sqlKeywords[word]:
				h.Style(i, n, "sql-keyword")
			case sqlTypes[word]:
				h.Style(i, n, "sql-type")
			case sqlConstants[word]:
				h.Style(i, n, "sql-const")
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
		if !c.AddFull(w, "", "", len(word), 0) {
			return
		}
	}
}
