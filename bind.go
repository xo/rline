// Key bindings: the keymaps that turn the keys a terminal sends into the
// commands of GNU readline, and the commands that rline runs itself (D41,
// D44, D46).
//
// isocline binds its keys in a switch over key codes. These keymaps replace
// it, so this file is rline's own and is ported from no C.

package rline

import (
	"fmt"
	"maps"
	"os"
	"os/user"
	"slices"
	"strings"
	"time"

	"github.com/xo/inputrc"

	"github.com/xo/rline/internal/editor"
	"github.com/xo/rline/internal/readline"
	"github.com/xo/rline/key"
)

// bytesReader is a byteReader over a fixed slice of bytes. Once the bytes run
// out it reports that nothing arrived.
type bytesReader struct {
	b   []byte
	pos int
}

func (r *bytesReader) readByte(time.Duration) (byte, bool) {
	if r.pos >= len(r.b) {
		return 0, false
	}
	r.pos++
	return r.b[r.pos-1], true
}

// decodeSeq returns the keys that seq decodes to. An inputrc file writes a key
// sequence as the bytes that a terminal sends, so the sequence is read as
// typing is read, and "\e[A" becomes the Up key. It reports false when a byte
// of seq decodes to no key.
func decodeSeq(seq string) ([]key.Code, bool) {
	d := newDecoder(&bytesReader{b: []byte(seq)})
	d.setEscDelay(0, 0)
	var keys []key.Code
	for {
		c, ok := d.readTimeout(0)
		if !ok {
			return keys, len(keys) > 0
		}
		if c == key.None {
			return nil, false
		}
		keys = append(keys, c)
	}
}

// newKeymap builds a keymap from bindings in the form that inputrc gives them:
// a key sequence and what it is bound to. A macro is bound to the keys that
// its text decodes to, which run as though they were typed.
func newKeymap(binds map[string]inputrc.Bind) *readline.Keymap {
	km := readline.NewKeymap()
	bindAll(km, binds)
	return km
}

// bindAll binds each of binds in km, over what km already binds.
func bindAll(km *readline.Keymap, binds map[string]inputrc.Bind) {
	for seq, b := range binds {
		keys, ok := decodeSeq(seq)
		if !ok {
			continue
		}
		if b.Macro {
			if text, ok := decodeSeq(b.Action); ok {
				km.BindMacroSeq(seq, keys, b.Action, text)
			}
			continue
		}
		km.BindSeq(seq, keys, b.Action)
	}
}

// emacsKeymap returns readline's default emacs keymap.
func emacsKeymap() *readline.Keymap {
	return newKeymap(inputrc.DefaultBinds()["emacs"])
}

// viKeymaps returns readline's default keymaps of vi's insert mode and
// command mode.
func viKeymaps() (*readline.Keymap, *readline.Keymap) {
	binds := inputrc.DefaultBinds()
	return newKeymap(binds["vi-insert"]), newKeymap(binds["vi-command"])
}

// defaultKeymaps returns readline's default keymaps of emacs, vi's insert
// mode and vi's command mode. With the completion menu on, Tab runs
// rline-menu-complete in emacs and in vi's insert mode (D51).
func (ev *env) defaultKeymaps() (emacs, viInsert, viCommand *readline.Keymap) {
	emacs = emacsKeymap()
	viInsert, viCommand = viKeymaps()
	if ev.completionMenu {
		emacs.BindSeq("\t", []key.Code{key.Tab}, "rline-menu-complete")
		viInsert.BindSeq("\t", []key.Code{key.Tab}, "rline-menu-complete")
	}
	return emacs, viInsert, viCommand
}

// readlineHistory returns the history for a line about to be read, oldest
// first. editLine pushes an empty entry that stands for the line itself, and
// that one is left out. No other entry is empty, because editLine keeps no
// line shorter than two bytes.
func (ev *env) readlineHistory() *readline.History {
	h := &readline.History{}
	all := ev.history.all()
	if len(all) > 0 && all[0] == "" {
		all = all[1:]
	}
	for _, entry := range slices.Backward(all) {
		h.Add(entry)
	}
	return h
}

// showLine copies the line and the cursor from l into e, and reports whether
// either changed.
func showLine(e *editor.Editor, l *readline.Line) bool {
	changed := false
	if text := l.Text(); text != e.Input.String() {
		e.Input.Replace(text)
		e.Modified = true
		changed = true
	}
	if e.Pos != l.Point() {
		e.Pos = l.Point()
		changed = true
	}
	return changed
}

// command runs a command that belongs to rline rather than to the line: one
// that draws, completes, searches, or uses a setting of the Session. It
// reports false for a command that the line runs itself. A command that
// changes the line here hands the change back to l with Edit, so that undo
// sees it.
func (ev *env) command(e *editor.Editor, l *readline.Line, name string, c key.Code) bool {
	showLine(e, l)
	switch name {
	case "accept-line":
		if !ev.multiline {
			return false
		}
		// A line continuation character at the end of a row turns into a
		// real line break rather than finishing the line.
		if e.Pos > 0 && e.Input.CharAt(e.Pos-1) == ev.opts.MultilineEOL && ev.posIsAtRowEnd(e) {
			e.MultilineEOL()
			l.Edit(e.Input.String(), e.Pos)
			return true
		}
		// The caller may say the line is not finished, which starts another
		// row instead of handing it back.
		if ev.cont != nil && ev.cont(e.Input.String()) {
			e.InsertChar('\n')
			l.Edit(e.Input.String(), e.Pos)
			return true
		}
		return false
	case "self-insert":
		// Brace insertion is rline's, and readline has none (D46).
		chr, ok := c.ASCIIChar()
		if n, _ := l.Count(); !ok || n != 1 || !ev.opts.AutoPair {
			return false
		}
		l.Default()
		showLine(e, l)
		e.AutoPair(chr)
		l.Edit(e.Input.String(), e.Pos)
		return true
	case "rline-accept-hint":
		ev.acceptHint(e)
		l.Edit(e.Input.String(), e.Pos)
		return true
	case "clear-screen", "clear-display":
		ev.clearScreen(e)
		return true
	case "redraw-current-line":
		ev.refresh(e)
		return true
	case "rline-help":
		ev.showHelp(e)
		return true
	case "rline-menu-complete":
		ev.generateCompletions(e, false)
		l.Edit(e.Input.String(), e.Pos)
		return true
	case "re-read-init-file":
		// The file is read again only when the program asked for one.
		if ev.inputrc == nil {
			return false
		}
		ev.readInputrc(ev.inputrc)
		l.SetKeymaps(ev.keymap, ev.viInsert, ev.viCommand)
		l.Configure(ev.lineVars())
		return true
	}
	if cmd := ev.commands[name]; cmd != nil {
		ev.runCommand(l, cmd, c)
		return true
	}
	return false
}

// A Command is a command of the program's own, which an inputrc file binds a
// key to by its name, as readline's rl_add_defun adds one (D58). WithCommand
// adds it.
//
// This is an interface rather than a function type so that a command can
// grow a second method without breaking everyone who has one. Wrap a plain
// function in CommandFunc.
type Command interface {
	Run(l *Line)
}

// CommandFunc makes a Command out of an ordinary function.
type CommandFunc func(l *Line)

// Run satisfies Command.
func (f CommandFunc) Run(l *Line) { f(l) }

// Line is the line being edited, as a Command sees it and changes it. Every
// position is a byte offset, as everywhere in this package.
type Line struct {
	text   string
	cursor int
	count  int
	key    key.Code

	// What the command asked for, which runCommand does after it returns.
	changed bool
	bell    bool
	accept  bool
}

// Text returns the line.
func (l *Line) Text() string { return l.text }

// Cursor returns where the cursor is in Text.
func (l *Line) Cursor() int { return l.cursor }

// Count returns the numeric argument typed before the key, as readline
// passes it to a command. It is 1 when none was typed, and can be negative.
func (l *Line) Count() int { return l.count }

// Key returns the key that ran the command.
func (l *Line) Key() key.Code { return l.key }

// Replace makes the line text, with the cursor at cursor. Undo takes the
// change back as one step, with the rest of the command.
func (l *Line) Replace(text string, cursor int) {
	l.text, l.cursor, l.changed = text, min(max(cursor, 0), len(text)), true
}

// Insert puts s in at the cursor and leaves the cursor after it.
func (l *Line) Insert(s string) {
	l.Replace(l.text[:l.cursor]+s+l.text[l.cursor:], l.cursor+len(s))
}

// Bell rings the bell, as a readline command does when it can do nothing.
func (l *Line) Bell() { l.bell = true }

// AcceptLine finishes the line after the command returns, as accept-line
// does, so that ReadLine hands it back.
func (l *Line) AcceptLine() { l.accept = true }

// runCommand runs a command of the program's own, and makes the changes that
// it asked for in the line.
func (ev *env) runCommand(rl *readline.Line, cmd Command, c key.Code) {
	n, _ := rl.Count()
	l := &Line{text: rl.Text(), cursor: rl.Point(), count: n, key: c}
	cmd.Run(l)
	if l.changed {
		rl.Edit(l.text, l.cursor)
	}
	if l.bell {
		rl.Ding()
	}
	if l.accept {
		rl.Run("accept-line", c)
	}
}

// acceptHint puts in the only completion, which is what a hint shows. With
// WithAutoTab it asks for the next one as well.
func (ev *env) acceptHint(e *editor.Editor) {
	if ev.completions.generate(e.Input.String(), e.Pos, 2) != 1 {
		return
	}
	if ev.complete(e, 0) && ev.completeAutoTab {
		ev.keys.pushCode(key.EventAutoTab)
	}
}

// candidates returns the completions of the program's completer for the
// readline commands that complete.
func (ev *env) candidates(line string, point int) []readline.Candidate {
	n := ev.completions.generate(line, point, maxCompletionsToShow)
	out := make([]readline.Candidate, 0, n)
	for i := range n {
		cm, ok := ev.completions.get(i)
		if !ok || cm.deleteBefore < 0 || cm.deleteAfter < 0 {
			continue
		}
		start, end := point-cm.deleteBefore, point+cm.deleteAfter
		if start < 0 || end > len(line) {
			continue
		}
		out = append(out, readline.Candidate{
			Start:   start,
			End:     end,
			Text:    cm.replacement,
			Display: cm.display,
			Suffix:  cm.suffix,
		})
	}
	return out
}

// showListing prints completions below the line, in columns that run down and
// then across, as readline lists them, and draws the line again below them.
// A list taller than the screen stops at each screenful with --More--, as
// readline's page-completions does.
func (ev *env) showListing(e *editor.Editor, cands []readline.Candidate) {
	items := make([]string, len(cands))
	width := 0
	for i, c := range cands {
		items[i] = c.Display
		if items[i] == "" {
			// The text is the program's and not markup, so a bracket in it
			// is shown as it is.
			items[i] = "[!pre]" + c.Text + "[/pre]"
		}
		width = max(width, ev.bb.columnWidth(items[i]))
	}
	width += 2
	cols := max(1, (e.TermW-1)/width)
	rows := (len(items) + cols - 1) / cols

	// Below the last row of the line.
	ev.term.down(e.CurRows - 1 - e.CurRow)
	ev.term.writeln("")
	page := max(ev.term.height-1, 1)
	shown := 0
	for r := range rows {
		if shown == page && !ev.more(&page) {
			break
		}
		var sb strings.Builder
		for c := range cols {
			i := r + c*rows
			if i >= len(items) {
				break
			}
			sb.WriteString(items[i])
			if c < cols-1 && i+rows < len(items) {
				sb.WriteString(strings.Repeat(" ", width-ev.bb.columnWidth(items[i])))
			}
		}
		ev.bb.println(sb.String())
		shown++
	}
	e.CurRows, e.CurRow = 1, 0
	ev.refresh(e)
}

// showOutput prints what a command such as dump-variables wrote below the
// line, and draws the line again below it.
func (ev *env) showOutput(e *editor.Editor, out string) {
	ev.term.down(e.CurRows - 1 - e.CurRow)
	ev.term.writeln("")
	for line := range strings.SplitSeq(strings.TrimSuffix(out, "\n"), "\n") {
		ev.term.write(line)
		ev.term.writeln("")
	}
	e.CurRows, e.CurRow = 1, 0
	ev.refresh(e)
}

// more asks whether to show more of a long list, and reads the answer as
// readline does: Space or y shows another screenful, Enter one more row, and
// q or n stops. It sets page to how many more rows to show.
func (ev *env) more(page *int) bool {
	ev.term.write("--More--")
	ev.term.flush()
	defer ev.term.clearLine()
	for {
		switch ev.keys.read() {
		case ' ', 'y', 'Y':
			*page += max(ev.term.height-1, 1)
			return true
		case key.Enter, key.Linefeed:
			*page++
			return true
		case 'q', 'Q', 'n', 'N', key.Rubout, key.Bell, key.CtrlC, key.None, key.EventStop:
			return false
		}
		ev.term.bell()
	}
}

// rlineCommands are the commands that rline runs itself, in ev.command.
var rlineCommands = map[string]bool{
	"accept-line": true, "self-insert": true, "rline-accept-hint": true,
	"clear-screen": true, "clear-display": true, "redraw-current-line": true,
	"rline-help": true, "rline-menu-complete": true,
}

// hasCommand reports whether readline or rline has a command called name.
func hasCommand(name string) bool {
	return readline.Has(name) || rlineCommands[name]
}

// rlineOnly returns the names of the commands that rline has and readline
// does not, for dump-functions and execute-named-command.
func rlineOnly() []string {
	var names []string
	for name := range rlineCommands {
		if !readline.Has(name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// rlineVars are the variables of readline that rline reads itself, rather
// than the line. keyseq-timeout is the wait of the decoder, which already
// waits for the rest of an escape sequence. The mode strings are drawn in
// front of the prompt (D53). The two bell variables are the terminal's.
var rlineVars = map[string]bool{
	"bell-style":          true,
	"prefer-visible-bell": true,
	"keyseq-timeout":      true,
	"show-mode-in-prompt": true,
	"emacs-mode-string":   true,
	"vi-ins-mode-string":  true,
	"vi-cmd-mode-string":  true,
}

// readInputrc reads the inputrc file that c names, or the user's, and builds
// the keymap and the variables from it, over readline's defaults. What in the
// file rline does not support goes into the notices, and a file that cannot
// be read is one notice.
func (ev *env) readInputrc(c *config) {
	ev.inputrc = c
	ev.notices = nil
	// An empty configuration, so that what the file sets can be told from the
	// defaults. It reads files for UserDefault and for $include.
	cfg := inputrc.NewConfig()
	cfg.ReadFileFunc = os.ReadFile
	opts := []inputrc.Option{
		inputrc.WithApp(c.appName),
		inputrc.WithTerm(os.Getenv("TERM")),
		inputrc.WithMode("emacs"),
	}
	var err error
	if c.inputrcFile != "" {
		err = inputrc.ParseFile(c.inputrcFile, cfg, opts...)
	} else {
		err = inputrc.UserDefault(homeUser(), cfg, opts...)
	}
	if err != nil {
		ev.notices = append(ev.notices, fmt.Sprintf("reading the inputrc file: %v", err))
	}

	// The file's bindings go over the defaults, after them, because two
	// sequences can decode to one key: \e[A and \eOA are both Up.
	binds := map[string]map[string]inputrc.Bind{
		"emacs":      {},
		"vi-insert":  {},
		"vi-command": {},
	}
	for _, km := range slices.Sorted(maps.Keys(cfg.Binds)) {
		// The meta and Ctrl-X keymaps are the keys after Esc and after
		// Ctrl-X, and vi and vi-move are other names of vi-command.
		name, prefix := km, ""
		switch km {
		case "emacs-standard":
			name = "emacs"
		case "emacs-meta":
			name, prefix = "emacs", "\x1b"
		case "emacs-ctlx":
			name, prefix = "emacs", "\x18"
		case "vi", "vi-move":
			name = "vi-command"
		}
		if binds[name] == nil {
			ev.notices = append(ev.notices, fmt.Sprintf("the keymap %s is not supported", km))
			continue
		}
		for seq, b := range cfg.Binds[km] {
			if !b.Macro && !readline.Has(b.Action) && !rlineCommands[b.Action] && ev.commands[b.Action] == nil {
				ev.notices = append(ev.notices, fmt.Sprintf("the command %s is not supported", b.Action))
			}
			binds[name][prefix+seq] = b
		}
	}
	ev.keymap, ev.viInsert, ev.viCommand = ev.defaultKeymaps()
	bindAll(ev.keymap, binds["emacs"])
	bindAll(ev.viInsert, binds["vi-insert"])
	bindAll(ev.viCommand, binds["vi-command"])

	ev.vars = cfg.Vars
	var unknown []string
	for name := range cfg.Vars {
		if !rlineVars[name] {
			unknown = append(unknown, name)
		}
	}
	vars := make(map[string]any, len(unknown))
	for _, name := range unknown {
		vars[name] = cfg.Vars[name]
	}
	for _, name := range readline.New(nil, nil).Configure(vars) {
		ev.notices = append(ev.notices, fmt.Sprintf("the variable %s is not supported", name))
	}
	style := bellStyleOf(cfg.Vars)
	ev.term.beep = c.beep && style != bellNone
	ev.term.visible = style == bellVisible
	if s, ok := cfg.Vars["editing-mode"].(string); ok {
		ev.viMode = s == "vi"
	}
	slices.Sort(ev.notices)
	ev.notices = slices.Compact(ev.notices)
}

// bellStyle is what readline's bell-style asks the bell to do.
type bellStyle int

// bellStyle values. The zero value is readline's default.
const (
	bellAudible bellStyle = iota
	bellNone
	bellVisible
)

// bellStyleOf returns the bell style that the variables of an inputrc file
// set. inputrc reads bell-style and prefer-visible-bell as readline 8.3
// does, and gives the style in bell-style as audible, visible or none.
func bellStyleOf(vars map[string]any) bellStyle {
	switch vars["bell-style"] {
	case "none":
		return bellNone
	case "visible":
		return bellVisible
	}
	return bellAudible
}

// homeUser returns the user whose home directory holds ~/.inputrc. readline
// takes the home directory from $HOME, and only a missing $HOME falls back
// to the password database.
func homeUser() *user.User {
	if home := os.Getenv("HOME"); home != "" {
		return &user.User{HomeDir: home}
	}
	u, err := user.Current()
	if err != nil {
		return nil
	}
	return u
}
