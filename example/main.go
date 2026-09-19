// Command example is a small SQL prompt built on rline.
//
// It shows the two things that are hard to see from the package on its own:
// input that runs over more than one line, and highlighting that changes as
// the line is typed.
//
// A statement ends at a semicolon. Until then Enter starts another line, so a
// statement can be written over as many lines as it takes.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
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
	r, err := rline.New(
		rline.WithPrompt("sql> ", "...> "),
		rline.WithHighlighter(highlight),
		rline.WithHistory("", 0),
	)
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
	r.Println("[ic-info]A statement ends at a semicolon. Ctrl-D leaves.[/]")
	r.Println("[ic-info]Ctrl-J puts a line break inside one line.[/]")

	var stmt strings.Builder
	for {
		// The first line of a statement is asked for differently from the
		// ones that carry it on.
		if stmt.Len() == 0 {
			r.SetPrompt("sql> ", "...> ")
		} else {
			r.SetPrompt("...> ", "...> ")
		}
		line, err := r.ReadLine("")
		if errors.Is(err, io.EOF) {
			r.Println("")
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading a line: %w", err)
		}
		if stmt.Len() > 0 {
			stmt.WriteByte('\n')
		}
		stmt.WriteString(line)
		text := strings.TrimSpace(stmt.String())
		switch {
		case text == "exit" || text == "quit":
			return nil
		case text == "":
			stmt.Reset()
		case strings.HasSuffix(text, ";"):
			// Plain, not markup: the statement came from the user and may hold
			// a bracket, which Print would read as a tag.
			_, _ = fmt.Fprintf(r, "ran %d line(s): %s\n", strings.Count(text, "\n")+1,
				strings.ReplaceAll(text, "\n", " "))
			stmt.Reset()
		}
	}
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
