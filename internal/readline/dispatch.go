package readline

import (
	"unicode"

	"github.com/xo/rline/key"
)

// argument is the numeric argument being typed: Esc and a digit start it,
// and plain digits then add to it. universal-argument starts it at four, and
// multiplies it by four each time it runs again.
type argument struct {
	active bool
	digits bool // a digit was given, so n holds the value
	neg    bool
	n      int
	times4 bool // universal-argument started it, so n holds a power of four
	quiet  bool // universal-argument ran last, and nothing is shown yet
}

// count returns the argument for a command, and whether one was given.
// Without digits it is 1, so that Esc - alone means -1, or the power of four
// that universal-argument gave.
func (a argument) count() (int, bool) {
	if !a.active {
		return 1, false
	}
	n := a.n
	if !a.digits && (!a.times4 || a.neg) {
		n = 1
	}
	if a.neg {
		n = -n
	}
	return n, true
}

// shown reports whether readline shows the argument in place of the prompt.
// It does not right after universal-argument, until a digit or a minus is
// typed.
func (a argument) shown() bool {
	return a.active && !a.quiet
}

// add puts the key c into the argument, and reports whether it was a digit
// or a leading minus that belongs to it. The first digit after
// universal-argument replaces the four.
func (a *argument) add(c key.Code) bool {
	switch {
	case c >= '0' && c <= '9':
		if !a.digits {
			a.n = 0
		}
		a.active, a.digits, a.quiet = true, true, false
		a.n = a.n*10 + int(c-'0')
	case c == '-' && !a.digits:
		a.quiet = false
		a.active, a.neg = true, true
	default:
		return false
	}
	return true
}

// Key reads one key and runs what it is bound to.
func (l *Line) Key(c key.Code) {
	if l.done {
		return
	}
	if l.recording {
		l.macro = append(l.macro, c)
	}
	top := l.depth == 0
	l.depth++
	defer func() {
		l.depth--
		if top {
			l.viKeyDone()
		}
	}()
	// vi's redo repeats the keys of the last change, so the keys of each
	// command in command mode are kept, and the keys of an insert that a
	// command started.
	if top && !l.viReplaying && (l.mode == modeViCommand || l.viRecording) {
		if !l.viRecording {
			l.viRec, l.viChange, l.viRecording = l.viRec[:0], false, true
		}
		l.viRec = append(l.viRec, c)
	}
	// A command that reads the next key itself, such as quoted-insert, takes
	// this one.
	if fn := l.readNext; fn != nil {
		l.readNext = nil
		fn(c)
		l.viClamp()
		return
	}
	if l.viOp != nil {
		l.viOpKey(c)
		return
	}
	if l.searching != nil {
		l.searchKey(c)
		return
	}
	if l.reading != nil {
		l.readKey(c)
		return
	}
	// The end of the input is Ctrl-D on an empty line. readline checks for it
	// before the keymap, as the terminal's end of file character.
	if len(l.pending) == 0 && c == key.CtrlD && len(l.buf) == 0 {
		l.done, l.eof = true, true
		return
	}
	// While a numeric argument is being typed, plain digits add to it, and
	// after universal-argument a minus does too.
	if l.arg.active && len(l.pending) == 0 &&
		(c >= '0' && c <= '9' || c == '-' && l.arg.times4 && !l.arg.digits && !l.arg.neg) {
		l.arg.add(c)
		return
	}
	// vi's replace mode reads its keys from a keymap of its own, which an
	// inputrc file cannot change: a character replaces the one at the
	// cursor, and Rubout puts it back.
	if l.mode == modeViInsert && l.overwrite && len(l.pending) == 0 {
		switch {
		case c == key.Rubout || c == key.Backspace:
			l.run("vi-overstrike-delete", c)
			return
		case c >= ' ' && c <= key.UnicodeMax && c != key.Rubout && !unicode.IsControl(rune(c)):
			l.run("vi-overstrike", c)
			return
		}
	}
	seq := append(append([]key.Code(nil), l.pending...), c)
	n, prefix := l.keymap.find(seq)
	if prefix && l.mode == modeViInsert && len(seq) == 1 && c == key.Esc {
		// In vi insert mode a lone Esc leaves insert mode at once. readline
		// waits keyseq-timeout for more of a sequence, and the decoder has
		// already waited for that.
		if k, b := l.keymap.boundPrefix(append(seq, 0)); k == 1 {
			l.runNode(b, c)
			return
		}
	}
	if prefix {
		l.pending = seq
		return
	}
	l.pending = nil
	var name string
	if n == nil && len(seq) > 1 {
		// The keys bound no sequence, but a start of them is bound: readline
		// runs that, and reads the rest again, in the keymap that it leaves.
		// So in vi insert mode Esc and h run vi-movement-mode, then h.
		if k, b := l.keymap.boundPrefix(seq); k > 0 {
			l.runNode(b, seq[k-1])
			for _, r := range seq[k:] {
				l.Key(r)
			}
			return
		}
	}
	if n != nil {
		if n.macro != nil {
			// A macro runs its keys as though they were typed, as many
			// times as the argument says.
			count, _ := l.arg.count()
			l.arg = argument{}
			for range max(count, 1) {
				for _, k := range n.macro {
					l.Key(k)
				}
			}
			return
		}
		name = n.name
	}
	if name == "" {
		// A character the keymap does not name, such as one outside ASCII,
		// is typed into the line. That includes a code point of the raw
		// plane, which stands for a byte that was not UTF-8. Any other key
		// does nothing.
		if len(seq) == 1 && c >= 0x80 && c <= key.UnicodeMax && !unicode.IsControl(rune(c)) {
			name = "self-insert"
		} else {
			l.arg = argument{}
			l.ding()
			return
		}
	}
	l.seqLen = len(seq)
	l.run(name, seq[len(seq)-1])
}

// runNode runs what the keymap node b is bound to, for the key c.
func (l *Line) runNode(b *node, c key.Code) {
	if b.macro != nil {
		for _, k := range b.macro {
			l.Key(k)
		}
		return
	}
	l.seqLen = 1
	l.run(b.name, c)
}

// viKeyDone ends the keys of a command in vi command mode once the command
// is complete, and keeps them for redo when it changed the line.
func (l *Line) viKeyDone() {
	if !l.viRecording || l.mode != modeViCommand || l.viOp != nil || l.readNext != nil ||
		len(l.pending) > 0 || l.arg.active || l.searching != nil || l.reading != nil {
		return
	}
	if l.viChange {
		l.viRedo = append(l.viRedo[:0:0], l.viRec...)
	}
	l.viRecording = false
}

// run runs the command called name for the key c, with the numeric argument
// typed before it.
func (l *Line) run(name string, c key.Code) {
	cmd := commands[name]
	if cmd == nil && l.handler == nil {
		// A command that this package does not have, and no program to ask.
		l.arg = argument{}
		l.ding()
		return
	}
	arg := l.arg
	l.arg = argument{}
	count, explicit := arg.count()
	l.this, l.thisKill = name, false
	// A command can run another one, as call-last-kbd-macro does, so the
	// running command is put back afterwards.
	prevCmd, prevCall := l.cmd, l.call
	l.cmd, l.call = cmd, call{count: count, explicit: explicit, key: c, arg: arg}
	l.undo.begin()
	switch {
	case l.handler != nil && l.handler(name, c):
	case cmd != nil:
		cmd(l, l.call)
	default:
		l.ding()
	}
	l.undo.end()
	l.cmd, l.call = prevCmd, prevCall
	l.last, l.lastKill = l.this, l.thisKill
	if l.mode == modeViCommand && (name == "previous-history" || name == "next-history") {
		// vi puts the cursor at the start of the line it brings back, and
		// the mark at its end.
		l.point, l.mark = 0, len(l.buf)
	}
	l.viClamp()
}

// call is what a command is given: the numeric argument, whether one was
// typed, and the key that ran it.
type call struct {
	count    int
	explicit bool
	key      key.Code
	arg      argument // the argument as it was typed, for digit-argument
}
