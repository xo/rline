// Command bash is a prompt that reads lines as bash does and runs none of
// them.
//
// It shows highlighting for bash, a history that lasts between runs, and the
// keys of GNU readline, which bash uses: emacs keys by default, vi keys with
// -vi, and the user's inputrc file, as bash reads it. Each line is printed
// back exactly as it was typed and is kept in the history. Nothing is
// executed, so it is safe to type anything.
//
// A line that ends with a backslash, or that leaves a quote open, carries on
// on the next row, as it does in bash.
//
// Run it from the root of the repository with:
//
//	go run ./_examples/bash
//	go run ./_examples/bash -vi
//	go run ./_examples/bash -inputrc my.inputrc
//
// exit or Ctrl-D leaves.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/xo/rline"
)

// historyFile is where the example keeps its history between runs, in the
// working directory, as the SQL example does. Its name is in .gitignore.
const historyFile = "rline_example_bash_history"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// run reads lines until the input ends or exit is typed.
func run() error {
	logPath := flag.String("log", "", "record the session to this file")
	vi := flag.Bool("vi", false, "edit with vi keys, as set -o vi does in bash")
	inputrcFile := flag.String("inputrc", "", "read this inputrc file in place of the user's")
	flag.Parse()

	opts := []rline.Option{
		rline.WithPrompt("$ ", "> "),
		// bash reads the user's inputrc, and readline knows it as Bash, which
		// is what $if Bash in an inputrc file tests.
		rline.WithUserInputrc(true),
		rline.WithAppName("Bash"),
		rline.WithViMode(*vi),
		rline.WithHighlighter(rline.HighlighterFunc(highlight)),
		rline.WithContinue(incomplete),
		rline.WithHistoryFile(historyFile),
		rline.WithHistoryLimit(rline.DefaultHistoryEntries),
	}
	if *inputrcFile != "" {
		opts = append(opts, rline.WithInputrcFile(*inputrcFile))
	}
	if *logPath != "" {
		f, err := os.Create(*logPath)
		if err != nil {
			return fmt.Errorf("opening the log: %w", err)
		}
		defer f.Close()
		// Every record, down to each byte read and written, which rline logs
		// at debug level.
		opts = append(opts, rline.WithLogger(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}
	p, err := rline.New(opts...)
	if err != nil {
		return fmt.Errorf("starting the reader: %w", err)
	}
	defer func() { _ = p.Close() }()

	for name, spec := range styles {
		p.DefineStyle(name, spec)
	}
	out := p.Markup()
	_, _ = fmt.Fprintln(out, "[b]rline[/b] bash example.")
	_, _ = fmt.Fprintln(out, "[rline-info]Nothing runs. Each line is printed back and kept in the history.[/]")
	_, _ = fmt.Fprintln(out, "[rline-info]The keys are readline's, and the user's inputrc changes them. -vi gives vi keys.[/]")
	_, _ = fmt.Fprintln(out, "[rline-info]exit or Ctrl-D leaves.[/]")
	// What in the inputrc file rline does not support, so that a binding
	// that does nothing says why.
	for _, n := range p.Notices() {
		_, _ = fmt.Fprintf(p.Stderr(), "inputrc: %s\n", n)
	}

	for {
		line, err := p.ReadLine("")
		switch {
		case errors.Is(err, io.EOF):
			_, _ = fmt.Fprintln(out, "")
			return nil
		case errors.Is(err, rline.ErrInterrupted):
			// Ctrl-C gives up on the line, as it does in bash.
			continue
		case err != nil:
			return fmt.Errorf("reading a line: %w", err)
		}
		if !p.Interactive() && line != "" {
			// With no terminal, ReadLine does not keep the history, so the
			// example keeps it, and every line is in it either way.
			p.AddHistory(line)
			if err := p.SaveHistory(); err != nil {
				return fmt.Errorf("saving the history: %w", err)
			}
		}
		// Plain, not markup: the line may hold a bracket, which the markup
		// writer would read as a tag.
		_, _ = fmt.Fprintln(p, line)
		if strings.TrimSpace(line) == "exit" {
			return nil
		}
	}
}

// styles are the styles that the highlighter marks with, in the bright ANSI
// colors, so that the highlighting shows on any terminal and in any theme.
var styles = map[string]string{
	"bash-keyword":  "bold ansi-fuchsia",
	"bash-builtin":  "bold ansi-yellow",
	"bash-command":  "bold ansi-blue",
	"bash-option":   "ansi-aqua",
	"bash-string":   "ansi-lime",
	"bash-variable": "bold ansi-red",
	"bash-operator": "ansi-teal",
	"bash-comment":  "ansi-gray",
}

// highlight marks the line with the styles that tokenize finds.
func highlight(l *rline.LineStyle) {
	for _, t := range tokenize(l.Text()) {
		l.Style(t.pos, t.n, t.style)
	}
}

// token is a stretch of a line, in bytes, and the style it is marked with.
type token struct {
	pos, n int
	style  string
}

// keywords are bash's reserved words. Each maps to whether the next word is
// a command, as it is after "then" and is not after "for".
var keywords = map[string]bool{
	"if": true, "then": true, "elif": true, "else": true, "fi": false,
	"for": false, "select": false, "in": false, "while": true, "until": true,
	"do": true, "done": false, "case": false, "esac": false,
	"function": false, "time": true, "!": true, "{": true, "}": false,
	"[[": false, "]]": false,
}

// builtins are the commands that bash runs itself.
var builtins = map[string]bool{
	".": true, ":": true, "alias": true, "bg": true, "bind": true,
	"break": true, "builtin": true, "cd": true, "command": true,
	"continue": true, "declare": true, "echo": true, "eval": true,
	"exec": true, "exit": true, "export": true, "false": true, "fg": true,
	"hash": true, "history": true, "jobs": true, "kill": true, "let": true,
	"local": true, "printf": true, "pwd": true, "read": true,
	"readonly": true, "return": true, "set": true, "shift": true,
	"source": true, "test": true, "trap": true, "true": true, "type": true,
	"typeset": true, "ulimit": true, "umask": true, "unalias": true,
	"unset": true, "wait": true, "[": true,
}

// tokenize finds the stretches of a bash line to mark. It reads one pass
// from the front. It is a highlighter, not a parser, so text that it does
// not understand is left unmarked.
func tokenize(text string) []token {
	var toks []token
	add := func(pos, n int, style string) {
		toks = append(toks, token{pos: pos, n: n, style: style})
	}
	// command is true where the next word names a command.
	command := true
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '\n':
			command = true
			i++
		case c == '#' && (i == 0 || isBreak(text[i-1])):
			n := strings.IndexByte(text[i:], '\n')
			if n < 0 {
				n = len(text) - i
			}
			add(i, n, "bash-comment")
			i += n
		case c == '\'':
			n := singleQuoted(text[i:])
			add(i, n, "bash-string")
			command = false
			i += n
		case c == '"':
			n := doubleQuoted(text[i:])
			add(i, n, "bash-string")
			// A variable inside double quotes is still expanded, so it is
			// marked over the string.
			for j := i + 1; j < i+n; j++ {
				switch text[j] {
				case '\\':
					j++
				case '$':
					if m := variable(text[j : i+n]); m > 1 {
						add(j, m, "bash-variable")
						j += m - 1
					}
				}
			}
			command = false
			i += n
		case strings.HasPrefix(text[i:], "$("):
			// A command substitution holds a command of its own.
			add(i, 2, "bash-operator")
			command = true
			i += 2
		case c == '$':
			n := variable(text[i:])
			if n > 1 {
				add(i, n, "bash-variable")
			}
			command = false
			i += n
		case isOperator(c):
			n := 1
			for i+n < len(text) && isOperator(text[i+n]) {
				n++
			}
			add(i, n, "bash-operator")
			op := text[i : i+n]
			// After a redirection comes a file name, and after anything else
			// comes a command.
			command = !strings.ContainsAny(op, "<>")
			i += n
		default:
			n := word(text[i:])
			w := text[i : i+n]
			switch {
			case command && isAssignment(w):
				// NAME=value before a command leaves the command to come.
				eq := strings.IndexByte(w, '=')
				add(i, eq, "bash-variable")
			case command && isKeyword(w):
				add(i, n, "bash-keyword")
				command = keywords[w]
			case command && builtins[w]:
				add(i, n, "bash-builtin")
				command = false
			case command:
				add(i, n, "bash-command")
				command = false
			case strings.HasPrefix(w, "-"):
				add(i, n, "bash-option")
			}
			i += n
		}
	}
	return toks
}

// isKeyword reports whether w is a reserved word.
func isKeyword(w string) bool {
	_, ok := keywords[w]
	return ok
}

// isOperator reports whether c is one of the bytes that make up bash's
// control and redirection operators.
func isOperator(c byte) bool {
	return strings.IndexByte(";&|<>()", c) >= 0
}

// isBreak reports whether c ends a word, so that a # after it starts a
// comment.
func isBreak(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || isOperator(c)
}

// word returns the length of the word at the front of s. A word ends at a
// blank, an operator, a quote or a $. A backslash keeps the byte after it in
// the word.
func word(s string) int {
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '\\' {
			i += 2
			continue
		}
		if isBreak(c) || c == '\'' || c == '"' || c == '$' {
			break
		}
		i++
	}
	if i == 0 {
		// A byte that starts no token, such as a lone backslash at the end.
		i = 1
	}
	return min(i, len(s))
}

// isAssignment reports whether w starts with NAME=.
func isAssignment(w string) bool {
	eq := strings.IndexByte(w, '=')
	if eq <= 0 {
		return false
	}
	for i := range eq {
		if !isNameByte(w[i]) || (i == 0 && isDigit(w[i])) {
			return false
		}
	}
	return true
}

// singleQuoted returns the length of the single quoted string at the front of
// s, with its quotes. A string with no closing quote runs to the end.
func singleQuoted(s string) int {
	if n := strings.IndexByte(s[1:], '\''); n >= 0 {
		return n + 2
	}
	return len(s)
}

// doubleQuoted returns the length of the double quoted string at the front of
// s, with its quotes. A backslash escapes the byte after it. A string with no
// closing quote runs to the end.
func doubleQuoted(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(s)
}

// variable returns the length of the expansion at the front of s, which
// starts with $. It answers 1 when the $ starts no expansion.
func variable(s string) int {
	if len(s) < 2 {
		return 1
	}
	switch c := s[1]; {
	case c == '{':
		if n := strings.IndexByte(s, '}'); n >= 0 {
			return n + 1
		}
		return len(s)
	case strings.IndexByte("?#$!@*-0123456789", c) >= 0:
		return 2
	case isNameByte(c):
		n := 1
		for n < len(s) && isNameByte(s[n]) {
			n++
		}
		return n
	}
	return 1
}

// isNameByte reports whether c can be part of a variable name.
func isNameByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || isDigit(c)
}

// isDigit reports whether c is a decimal digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// incomplete reports whether bash would read another row before it ran the
// text: a quote is still open, or the last row ends with a backslash.
func incomplete(text string) bool {
	var quote byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
		case c == '\\':
			if i == len(text)-1 {
				return true
			}
			i++
		case quote == '"':
			if c == '"' {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == 0 || isBreak(text[i-1])):
			// A comment hides quotes and backslashes to the end of its row.
			n := strings.IndexByte(text[i:], '\n')
			if n < 0 {
				return false
			}
			i += n
		}
	}
	return quote != 0
}
