package readline

import (
	"slices"
	"strings"

	rtext "github.com/xo/rline/internal/text"
	"github.com/xo/rline/key"
)

// reader reads a line of text in a prompt of its own, as the non-incremental
// searches and execute-named-command read it. readline edits the text in the
// line itself, so the line shows the text while it is read, and comes back
// when the reading ends.
type reader struct {
	prompt string

	// The line, the cursor and the mark before the reading.
	text        []byte
	point, mark int

	// quote types the next key as it is, after Ctrl-V, and commands makes
	// Tab complete the name of a command.
	quote    bool
	commands bool

	// done gets the text when Enter ends the reading, after the line came
	// back.
	done func(text string)
}

// startReading reads text after prompt, and hands it to done.
func (l *Line) startReading(prompt string, commands bool, done func(text string)) {
	l.reading = &reader{
		prompt:   prompt,
		text:     l.buf,
		point:    l.point,
		mark:     l.mark,
		commands: commands,
		done:     done,
	}
	l.buf = nil
	l.point = 0
}

// stopReading ends the reading and puts the line back as it was.
func (l *Line) stopReading() {
	r := l.reading
	l.reading = nil
	l.buf, l.point, l.mark = r.text, r.point, r.mark
}

// readKey reads one key of the text. Enter ends it, Ctrl-G gives it up, and
// Rubout on no text gives it up too. Ctrl-U and Ctrl-W rub out the text and
// its last word, Ctrl-V types the next key as it is, and in the name of a
// command Tab completes it. Any other key, Esc and the control keys too, is
// typed into the text.
func (l *Line) readKey(c key.Code) {
	r := l.reading
	if r.quote {
		r.quote = false
		l.readInsert(c)
		return
	}
	switch {
	case c == key.Enter || c == key.Linefeed:
		text := string(l.buf)
		l.stopReading()
		r.done(text)
	case c == key.Bell:
		l.stopReading()
		l.ding()
	case c == key.Rubout || c == key.Backspace:
		if len(l.buf) == 0 {
			l.stopReading()
			return
		}
		l.buf = l.buf[:l.prevChar(len(l.buf))]
		l.point = len(l.buf)
	case c == key.CtrlU:
		l.buf = l.buf[:0]
		l.point = 0
	case c == key.CtrlW:
		start := l.blankWordStart(l.point)
		l.buf = l.buf[:start]
		l.point, l.mark = start, start
	case c == key.CtrlV:
		r.quote = true
	case c == key.Tab && r.commands:
		l.completeCommandName()
	default:
		l.readInsert(c)
	}
}

// readInsert types c at the end of the text, when it is a character. A key
// such as an arrow, which is no character, rings the bell.
func (l *Line) readInsert(c key.Code) {
	if c&key.ModMask != 0 || c > key.UnicodeMax {
		l.ding()
		return
	}
	l.buf = rtext.AppendRune(l.buf, rune(c))
	l.point = len(l.buf)
}

// commandNames returns the names of every command, this package's and the
// program's, sorted.
func (l *Line) commandNames() []string {
	names := make([]string, 0, len(commands)+len(l.extra))
	for name := range commands {
		names = append(names, name)
	}
	names = append(names, l.extra...)
	slices.Sort(names)
	return slices.Compact(names)
}

// completeCommandName completes the name being read to what every command
// that starts with it shares. It rings the bell when that adds nothing.
func (l *Line) completeCommandName() {
	typed := string(l.buf)
	var shared string
	found := false
	for _, name := range l.commandNames() {
		if !strings.HasPrefix(name, typed) {
			continue
		}
		if !found {
			shared, found = name, true
			continue
		}
		n := 0
		for n < len(shared) && n < len(name) && shared[n] == name[n] {
			n++
		}
		shared = shared[:n]
	}
	if !found || shared == typed {
		l.ding()
		return
	}
	l.buf = []byte(shared)
	l.point = len(l.buf)
}

// executeNamedCommand reads the name of a command and runs it, with the
// numeric argument that it was given.
func executeNamedCommand(l *Line, c call) {
	arg := c.arg
	l.startReading("!", true, func(name string) {
		switch {
		case name == "":
		case !slices.Contains(l.commandNames(), name):
			l.ding()
		default:
			l.arg = arg
			l.run(name, 0)
		}
	})
}

// nonIncrementalSearch reads the text to search for, after prompt, and
// searches the history for it in the direction dir: -1 for older lines and
// 1 for newer ones. Empty text searches again for the last text.
func nonIncrementalSearch(l *Line, prompt string, dir int) {
	l.startReading(prompt, false, func(text string) {
		if text == "" {
			text = l.readSearch
		}
		if text == "" {
			l.ding()
			return
		}
		l.readSearch = text
		l.searchHistory(text, dir)
	})
}

// searchAgain searches the history again for the text of the last
// non-incremental search, in the direction dir.
func (l *Line) searchAgain(dir int) {
	if l.readSearch == "" {
		l.ding()
		return
	}
	l.searchHistory(l.readSearch, dir)
}

// searchHistory moves to the first history line in the direction dir that
// holds text anywhere, with the cursor at its start. emacs puts the mark at
// the length of the text, and vi at the end of the line. When no line holds
// it, the line stays and the bell rings.
func (l *Line) searchHistory(text string, dir int) {
	h := l.history
	for pos := h.pos + dir; pos >= 0 && pos < len(h.entries); pos += dir {
		if !strings.Contains(h.line(pos), text) {
			continue
		}
		l.moveTo(pos)
		l.point, l.mark = 0, len(text)
		if l.mode != modeEmacs {
			l.mark = len(l.buf)
		}
		return
	}
	l.ding()
}

// The commands of this file.
func init() {
	for name, cmd := range map[string]command{
		"execute-named-command": executeNamedCommand,
		"non-incremental-reverse-search-history": func(l *Line, _ call) {
			nonIncrementalSearch(l, ":", -1)
		},
		"non-incremental-forward-search-history": func(l *Line, _ call) {
			nonIncrementalSearch(l, ":", 1)
		},
		"non-incremental-reverse-search-history-again": func(l *Line, _ call) { l.searchAgain(-1) },
		"non-incremental-forward-search-history-again": func(l *Line, _ call) { l.searchAgain(1) },
		// / searches older lines and ? newer ones. n searches older lines
		// again and N newer ones, whichever of the two the search was.
		"vi-search": func(l *Line, c call) {
			if c.key == '?' {
				nonIncrementalSearch(l, "?", 1)
				return
			}
			nonIncrementalSearch(l, "/", -1)
		},
		"vi-search-again": func(l *Line, c call) {
			if c.key == 'N' {
				l.searchAgain(1)
				return
			}
			l.searchAgain(-1)
		},
	} {
		commands[name] = cmd
	}
}
