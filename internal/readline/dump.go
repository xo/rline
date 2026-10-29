package readline

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	rtext "github.com/xo/rline/internal/text"
	"github.com/xo/rline/key"
)

// The commands that print: dump-functions, dump-macros, dump-variables,
// print-last-kbd-macro and export-completions. What they print waits in the
// line until the program takes it with Output, and the program shows it
// below the line, as readline writes it to its output stream.

// Output returns what the commands that print wrote, and forgets it. It is
// empty when they wrote nothing.
func (l *Line) Output() string {
	out := string(l.output)
	l.output = nil
	return out
}

// SetCommandNames names the commands that the program runs in its handler and
// that this package does not have, so that dump-functions and
// execute-named-command know them.
func (l *Line) SetCommandNames(names []string) { l.extra = slices.Clone(names) }

// printf writes to the output.
func (l *Line) printf(format string, args ...any) {
	l.output = fmt.Appendf(l.output, format, args...)
}

// escapeSeq writes s as an inputrc file writes a key sequence or the text of
// a macro, as readline prints them: Esc is \e, a control character is \C-
// and its letter in lower case, DEL is \C-?, a backslash or a double quote
// has a backslash in front, and a byte from 0x80 to 0xff, which inputrc reads
// as the character of that number, is three octal digits after a backslash.
func escapeSeq(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case r == 0x1b:
			sb.WriteString(`\e`)
		case r == 0x7f:
			sb.WriteString(`\C-?`)
		case r == 0x1c:
			sb.WriteString(`\C-\\`)
		case r < 0x20:
			c := byte(r) + 0x40
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			sb.WriteString(`\C-`)
			sb.WriteByte(c)
		case r >= 0x80 && r <= 0xff:
			fmt.Fprintf(&sb, "\\%03o", r)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// virtualSeqs are the sequences that the keys which are no character send,
// for print-last-kbd-macro, which prints the keys of a macro as text.
var virtualSeqs = map[key.Code]string{
	key.Up:       "\x1b[A",
	key.Down:     "\x1b[B",
	key.Right:    "\x1b[C",
	key.Left:     "\x1b[D",
	key.Home:     "\x1b[H",
	key.End:      "\x1b[F",
	key.Ins:      "\x1b[2~",
	key.Del:      "\x1b[3~",
	key.PageUp:   "\x1b[5~",
	key.PageDown: "\x1b[6~",
}

// keysText returns the text that typing keys sends.
func keysText(keys []key.Code) string {
	var b []byte
	for _, k := range keys {
		if seq, ok := virtualSeqs[k]; ok {
			b = append(b, seq...)
			continue
		}
		if k&key.ModAlt != 0 {
			b = append(b, 0x1b)
		}
		if c := k &^ key.ModMask; c <= key.UnicodeMax {
			b = rtext.AppendRune(b, rune(c))
		}
	}
	return string(b)
}

// nodeOf returns the node that seq ends at, whether it is bound or not.
func (k *Keymap) nodeOf(seq []key.Code) *node {
	n := &k.root
	for _, c := range seq {
		child, ok := n.next[c]
		if !ok {
			return nil
		}
		n = child
	}
	return n
}

// boundSeqs returns the sequences that are still bound to a command, by the
// name of the command, and those still bound to a macro, each sorted as
// readline sorts them, by their bytes. A sequence that a later binding of the
// same keys replaced is left out.
func (k *Keymap) boundSeqs() (byName map[string][]string, macros []string) {
	byName = make(map[string][]string)
	for seq, src := range k.sources {
		n := k.nodeOf(src.keys)
		switch {
		case n == nil:
		case src.macro == "" && n.macro == nil && n.name != "":
			byName[n.name] = append(byName[n.name], seq)
		case src.macro != "" && n.macro != nil:
			macros = append(macros, seq)
		}
	}
	for _, seqs := range byName {
		slices.Sort(seqs)
	}
	slices.Sort(macros)
	return byName, macros
}

// dumpFunctions prints every command with the sequences bound to it. With a
// numeric argument it prints them as the lines of an inputrc file.
func dumpFunctions(l *Line, c call) {
	byName, _ := l.keymap.boundSeqs()
	for _, name := range l.commandNames() {
		seqs := byName[name]
		switch {
		case c.explicit && len(seqs) == 0:
			l.printf("# %s (not bound)\n", name)
		case c.explicit:
			for _, seq := range seqs {
				l.printf("\"%s\": %s\n", escapeSeq(seq), name)
			}
		case len(seqs) == 0:
			l.printf("%s is not bound to any keys\n", name)
		default:
			// readline names five sequences, and then says there are more,
			// with no full stop.
			quoted := make([]string, 0, 6)
			for _, seq := range seqs[:min(len(seqs), 5)] {
				quoted = append(quoted, `"`+escapeSeq(seq)+`"`)
			}
			end := "."
			if len(seqs) > 5 {
				quoted, end = append(quoted, "..."), ""
			}
			l.printf("%s can be found on %s%s\n", name, strings.Join(quoted, ", "), end)
		}
	}
}

// dumpMacros prints every sequence bound to a macro, with its text. With a
// numeric argument it prints them as the lines of an inputrc file.
func dumpMacros(l *Line, c call) {
	_, macros := l.keymap.boundSeqs()
	for _, seq := range macros {
		text := escapeSeq(l.keymap.sources[seq].macro)
		if c.explicit {
			l.printf("\"%s\": \"%s\"\n", escapeSeq(seq), text)
			continue
		}
		l.printf("%s outputs %s\n", escapeSeq(seq), text)
	}
}

// dumpVariables prints every variable of readline with its value: those that
// are on or off first, then the others, each sorted by name. With a numeric
// argument it prints them as the lines of an inputrc file.
func dumpVariables(l *Line, c call) {
	vars := maps.Clone(l.variables())
	switch l.mode {
	case modeEmacs:
		vars["editing-mode"], vars["keymap"] = "emacs", "emacs"
	case modeViInsert:
		vars["editing-mode"], vars["keymap"] = "vi", "vi-insert"
	default:
		vars["editing-mode"], vars["keymap"] = "vi", "vi-command"
	}
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		_, aBool := vars[a].(bool)
		_, bBool := vars[b].(bool)
		switch {
		case aBool && !bBool:
			return -1
		case bBool && !aBool:
			return 1
		}
		return strings.Compare(a, b)
	})
	for _, name := range names {
		var value string
		switch v := vars[name].(type) {
		case bool:
			value = "off"
			if v {
				value = "on"
			}
		case int:
			value = strconv.Itoa(v)
		case string:
			value = escapeSeq(v)
		default:
			value = fmt.Sprint(v)
		}
		if c.explicit {
			l.printf("set %s %s\n", name, value)
			continue
		}
		l.printf("%s is set to `%s'\n", name, value)
	}
}

// printLastKbdMacro prints the keys of the last keyboard macro, as the text
// of a macro in an inputrc file.
func printLastKbdMacro(l *Line, _ call) {
	if len(l.lastMacro) == 0 {
		l.ding()
		return
	}
	l.printf("%s\n", escapeSeq(keysText(l.lastMacro)))
}

// exportCompletions prints the completions of the word before the cursor
// for another program to read, one to a line: how many there are, the word,
// where the word starts and ends as start:end, and then the completions.
// With more than one, the first is what they all start with.
func exportCompletions(l *Line, _ call) {
	cands := l.candidates()
	start := l.point
	if len(cands) > 0 {
		start = cands[0].Start
	} else {
		for start > 0 && !strings.ContainsRune(completerWordBreaks, l.runeBefore(start)) {
			start = l.prevChar(start)
		}
	}
	matches := make([]string, 0, len(cands)+1)
	if len(cands) > 1 {
		prefix, _ := sharedPrefix(cands)
		matches = append(matches, prefix)
	}
	for _, cand := range cands {
		matches = append(matches, cand.Text)
	}
	l.printf("%d\n%s\n%d:%d\n", len(matches), l.buf[start:l.point], start, l.point)
	for _, m := range matches {
		l.printf("%s\n", m)
	}
}

// The commands of this file.
func init() {
	for name, cmd := range map[string]command{
		"dump-functions":       dumpFunctions,
		"dump-macros":          dumpMacros,
		"dump-variables":       dumpVariables,
		"print-last-kbd-macro": printLastKbdMacro,
		"export-completions":   exportCompletions,
	} {
		commands[name] = cmd
	}
}
