package readline

import (
	"bytes"
	"os"
	"os/user"
	"strings"

	"github.com/xo/rline/key"
)

// The commands of this file: searching for a character, keyboard macros,
// tilde expansion, the region, copying to the kill ring, overwrite mode,
// moving by bytes, and searching the history for the text before the cursor.
func init() {
	for name, cmd := range map[string]command{
		"character-search":          func(l *Line, c call) { l.charSearch(c.count) },
		"character-search-backward": func(l *Line, c call) { l.charSearch(-c.count) },

		"start-kbd-macro": func(l *Line, _ call) {
			l.recording, l.macro = true, nil
		},
		"end-kbd-macro": endKbdMacro,
		"call-last-kbd-macro": func(l *Line, c call) {
			keys := l.lastMacro
			for i := 0; i < max(c.count, 1); i++ {
				for _, k := range keys {
					l.Key(k)
				}
			}
		},

		"tilde-expand": tildeExpand,

		"kill-region": func(l *Line, _ call) {
			start, end := l.region()
			l.kill(start, end)
		},
		"copy-region-as-kill": func(l *Line, _ call) {
			start, end := l.region()
			l.copy(start, end)
		},
		"copy-backward-word": func(l *Line, c call) {
			l.copy(l.wordsFrom(l.point, -max(c.count, 1)), l.point)
		},
		"copy-forward-word": func(l *Line, c call) {
			l.copy(l.point, l.wordsFrom(l.point, max(c.count, 1)))
		},
		"forward-backward-delete-char": func(l *Line, c call) {
			if l.point >= len(l.buf) && l.point > 0 {
				backwardDeleteChar(l, c)
				return
			}
			deleteChar(l, c)
		},
		"unix-filename-rubout": unixFilenameRubout,

		"overwrite-mode": func(l *Line, c call) {
			l.overwrite = !l.overwrite
			if c.explicit {
				l.overwrite = c.count > 0
			}
		},

		"forward-byte":  func(l *Line, c call) { l.point = min(max(l.point+c.count, 0), len(l.buf)) },
		"backward-byte": func(l *Line, c call) { l.point = min(max(l.point-c.count, 0), len(l.buf)) },

		"history-search-backward":           func(l *Line, _ call) { l.historySearch(-1, false) },
		"history-search-forward":            func(l *Line, _ call) { l.historySearch(1, false) },
		"history-substring-search-backward": func(l *Line, _ call) { l.historySearch(-1, true) },
		"history-substring-search-forward":  func(l *Line, _ call) { l.historySearch(1, true) },

		"clear-display": func(*Line, call) {},
	} {
		commands[name] = cmd
	}
}

// charSearch moves to the next character that is the one the next key types,
// n times, or back to the previous one when n is negative. It rings the bell
// and stays where it is when there is no such character.
func (l *Line) charSearch(n int) {
	l.readNext = func(k key.Code) {
		target := []byte(string(rune(k &^ key.ModMask)))
		pos := l.point
		for i := 0; i < max(n, -n); i++ {
			var at int
			if n > 0 {
				at = bytes.Index(l.buf[l.nextChar(pos):], target)
				if at >= 0 {
					at += l.nextChar(pos)
				}
			} else {
				at = bytes.LastIndex(l.buf[:pos], target)
			}
			if at < 0 {
				l.ding()
				return
			}
			pos = at
		}
		l.point = pos
	}
}

// endKbdMacro stops recording a keyboard macro. The keys that ended it are
// recorded too, and are taken off again.
func endKbdMacro(l *Line, _ call) {
	if !l.recording {
		l.ding()
		return
	}
	l.recording = false
	keys := l.macro
	// The sequence that ran this command is the last thing recorded, and it
	// is not part of the macro. A replay that ran it would ring the bell,
	// which the recordings cannot see, so nothing but this comment holds it.
	if n := len(keys) - l.seqLen; n >= 0 {
		keys = keys[:n]
	}
	l.lastMacro = keys
	l.macro = nil
}

// region returns the text between the cursor and the mark, in order.
func (l *Line) region() (int, int) {
	mark := min(l.mark, len(l.buf))
	return min(l.point, mark), max(l.point, mark)
}

// copy puts the bytes from start to end on the kill ring as an entry of its
// own, and changes nothing in the line. Copies do not join the kill before
// them.
func (l *Line) copy(start, end int) {
	if start >= end {
		return
	}
	l.kills.add(append([]byte(nil), l.buf[start:end]...), false, false)
}

// unixFilenameRubout kills the word before the cursor, where spaces and
// slashes separate words. Spaces and slashes just before the cursor go with
// it.
func unixFilenameRubout(l *Line, c call) {
	sep := func(r rune) bool { return isBlank(r) || r == '/' }
	start := l.point
	for i := 0; i < max(c.count, 1); i++ {
		for start > 0 && sep(l.runeBefore(start)) {
			start = l.prevChar(start)
		}
		for start > 0 && !sep(l.runeBefore(start)) {
			start = l.prevChar(start)
		}
	}
	if start == l.point {
		l.ding()
		return
	}
	l.kill(start, l.point)
}

// tildeExpand replaces a word of the form ~ or ~name, or one that starts with
// ~/ or ~name/, with the home directory it names. The word is the one around
// the cursor, between spaces, and the cursor goes to its end.
func tildeExpand(l *Line, _ call) {
	start, end := l.point, l.point
	for start > 0 && !isBlank(l.runeBefore(start)) {
		start = l.prevChar(start)
	}
	for end < len(l.buf) && !isBlank(l.runeAt(end)) {
		end = l.nextChar(end)
	}
	word := string(l.buf[start:end])
	if !strings.HasPrefix(word, "~") {
		l.point = end
		return
	}
	name, rest, _ := strings.Cut(word[1:], "/")
	home, ok := l.home(name)
	if !ok {
		l.ding()
		return
	}
	text := home
	if strings.Contains(word, "/") {
		text += "/" + rest
	}
	l.replace(start, end, []byte(text))
	l.point = start + len(text)
}

// homeDir returns the home directory of the user called name, or of the
// current user, from HOME, when name is empty.
func homeDir(name string) (string, bool) {
	if name == "" {
		if home := os.Getenv("HOME"); home != "" {
			return home, true
		}
		u, err := user.Current()
		if err != nil {
			return "", false
		}
		return u.HomeDir, true
	}
	u, err := user.Lookup(name)
	if err != nil {
		return "", false
	}
	return u.HomeDir, true
}

// historySearch moves to the next history line, in the direction dir, that
// starts with the text before the cursor, or that holds it anywhere when
// substring is set. The text is kept while the searches follow one another.
// A prefix search leaves the cursor after the text and a substring search
// leaves it where the text starts. Both put the mark at the end of the line.
func (l *Line) historySearch(dir int, substring bool) {
	same := strings.HasPrefix(l.last, "history-search-") || strings.HasPrefix(l.last, "history-substring-search-")
	if !same {
		l.search = string(l.buf[:l.point])
	}
	h := l.history
	for pos := h.pos + dir; pos >= 0 && pos < len(h.entries); pos += dir {
		line := h.line(pos)
		at := strings.Index(line, l.search)
		if line == string(l.buf) || at < 0 || !substring && at != 0 {
			continue
		}
		l.moveTo(pos)
		l.point = len(l.search)
		if substring {
			l.point = at
		}
		l.mark = len(l.buf)
		return
	}
	l.ding()
}
