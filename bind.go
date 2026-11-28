// Key bindings: the keymaps that turn the keys a terminal sends into the
// commands of GNU readline, and the commands that rline runs itself (D41,
// D44, D46).
//
// isocline binds its keys in a switch over key codes. These keymaps replace
// it, so this file is rline's own and is ported from no C.

package rline

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/user"
	"slices"
	"strconv"
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
	case "bracketed-paste-begin":
		// The decoder reads the paste as bytes, so that what is in it stays
		// text.
		l.Paste(ev.keys.readPaste())
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
		ev.runCommand(e, l, cmd, c)
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
//
// A change to the line, the mark or the kill ring happens at once, and undo
// takes back everything that the command changed as one step. Run and
// Complete wait until the command returns.
type Line struct {
	rl    *readline.Line
	ev    *env
	e     *editor.Editor
	count int
	given bool
	key   key.Code

	// What the command asked for, which runCommand does after it returns.
	bell   bool
	accept bool
}

// pendingRun is a command that a Command asked to run after it returns, and
// the completer to run it with, when it is a completion of its own.
type pendingRun struct {
	name      string
	completer Completer
	complete  bool
}

// Text returns the line.
func (l *Line) Text() string { return l.rl.Text() }

// Cursor returns where the cursor is in Text.
func (l *Line) Cursor() int { return l.rl.Point() }

// Count returns the numeric argument typed before the key, as readline
// passes it to a command. It is 1 when none was typed, and can be negative.
func (l *Line) Count() int { return l.count }

// CountGiven reports whether a numeric argument was typed before the key, as
// bash's READLINE_ARGUMENT is set only then (D82).
func (l *Line) CountGiven() bool { return l.given }

// Key returns the key that ran the command.
func (l *Line) Key() key.Code { return l.key }

// Mark returns the mark, as bash's READLINE_MARK gives it (D82).
func (l *Line) Mark() int { return l.rl.Mark() }

// SetMark puts the mark at pos, within the line (D82).
func (l *Line) SetMark(pos int) { l.rl.SetMark(pos) }

// Replace makes the line text, with the cursor at cursor. Undo takes the
// change back as one step, with the rest of the command.
func (l *Line) Replace(text string, cursor int) {
	l.rl.Edit(text, min(max(cursor, 0), len(text)))
}

// Insert puts s in at the cursor and leaves the cursor after it.
func (l *Line) Insert(s string) {
	text, cursor := l.Text(), l.Cursor()
	l.Replace(text[:cursor]+s+text[cursor:], cursor+len(s))
}

// Kill takes the text from start to end out of the line and puts it on the
// kill ring, as readline's kill commands do, so that yank brings it back. A
// kill by the command that ran just before joins it, as a second kill-word
// does. The cursor and the mark go to where the text was, and the two ends
// can come in either order (D83).
func (l *Line) Kill(start, end int) { l.rl.Kill(start, end) }

// Bell rings the bell, as a readline command does when it can do nothing.
func (l *Line) Bell() { l.bell = true }

// AcceptLine finishes the line after the command returns, as accept-line
// does, so that ReadLine hands it back.
func (l *Line) AcceptLine() { l.accept = true }

// Run runs the readline or rline command called name after this command
// returns, as though its key were pressed then, with no numeric argument.
// Undo takes it back as a step of its own. A name that no command has rings
// the bell (D80).
func (l *Line) Run(name string) {
	l.ev.pending = append(l.ev.pending, pendingRun{name: name})
}

// Complete runs a command that completes, such as complete,
// possible-completions or insert-completions, after this command returns,
// with completer in place of the Session's, as bash's complete-filename
// completes file names whatever the program's completer offers. The
// Session's completer is back in place afterwards (D80).
func (l *Line) Complete(name string, completer Completer) {
	l.ev.pending = append(l.ev.pending, pendingRun{name: name, completer: completer, complete: true})
}

// Release gives the terminal to fn while the command runs, as bash does for
// bind -x: the line is taken off the screen, the terminal leaves raw mode,
// fn reads and writes the terminal through rw, and then the prompt and the
// line are drawn again below what fn wrote. It returns the error of fn, or
// the error of putting the terminal back into raw mode (D81).
func (l *Line) Release(fn func(rw io.ReadWriter) error) error {
	return l.ev.release(l.e, l.rl, fn)
}

// runCommand runs a command of the program's own, and does what it asked for
// once it returns.
func (ev *env) runCommand(e *editor.Editor, rl *readline.Line, cmd Command, c key.Code) {
	n, given := rl.Count()
	l := &Line{rl: rl, ev: ev, e: e, count: n, given: given, key: c}
	cmd.Run(l)
	if l.bell {
		rl.Ding()
	}
	if l.accept {
		rl.Run("accept-line", c)
	}
}

// runPending runs the commands that a Command asked for with Run and
// Complete, each as though its key were pressed after the key that ran the
// Command (D80).
func (ev *env) runPending(l *readline.Line, c key.Code) {
	for len(ev.pending) > 0 {
		p := ev.pending[0]
		ev.pending = ev.pending[1:]
		if !p.complete {
			l.Run(p.name, c)
			continue
		}
		prev, arg := ev.completions.completer, ev.completions.completerArg
		ev.completions.setCompleter(p.completer, nil)
		l.Run(p.name, c)
		ev.completions.setCompleter(prev, arg)
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
func (ev *env) candidates(line string, point int) ([]readline.Candidate, bool) {
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
	return out, ev.completions.keepOrder
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
	ev.writePromptLines()
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
	ev.writePromptLines()
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

// Commands returns the name of every command that a key can be bound to,
// readline's and rline's, sorted, as bash's bind -l lists them. A command of
// the program's own is not among them (D76).
func Commands() []string {
	names := append(readline.Names(), rlineOnly()...)
	slices.Sort(names)
	return slices.Compact(names)
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
// front of the prompt (D53). The two bell variables are the terminal's, and
// so is bracketed paste mode (D66).
var rlineVars = map[string]bool{
	"bell-style":             true,
	"enable-bracketed-paste": true,
	"prefer-visible-bell":    true,
	"keyseq-timeout":         true,
	"show-mode-in-prompt":    true,
	"emacs-mode-string":      true,
	"vi-ins-mode-string":     true,
	"vi-cmd-mode-string":     true,
}

// readInputrc reads the inputrc file that c names, or the user's, and builds
// the keymap and the variables from it, over readline's defaults. What in the
// file rline does not support goes into the notices, and a file that cannot
// be read is one notice.
func (ev *env) readInputrc(c *config) {
	ev.inputrc = c
	ev.notices = nil
	ev.appName, ev.beep = c.appName, c.beep
	// An empty configuration, so that what the file sets can be told from the
	// defaults. It reads files for UserDefault and for $include.
	cfg := inputrc.NewConfig()
	cfg.ReadFileFunc = os.ReadFile
	var err error
	if c.inputrcFile != "" {
		err = inputrc.ParseFile(c.inputrcFile, cfg, ev.parseOptions()...)
	} else {
		err = inputrc.UserDefault(homeUser(), cfg, ev.parseOptions()...)
	}
	if err != nil {
		ev.notices = append(ev.notices, fmt.Sprintf("reading the inputrc file: %v", err))
	}
	ev.vars = nil
	ev.keymap, ev.viInsert, ev.viCommand = ev.defaultKeymaps()
	ev.apply(cfg)
}

// parseOptions are what the parser of an inputrc file is told: the name of
// the program and of the terminal, which $if tests, and the editing mode.
func (ev *env) parseOptions() []inputrc.Option {
	mode := "emacs"
	if ev.viMode {
		mode = "vi"
	}
	return []inputrc.Option{
		inputrc.WithApp(ev.appName),
		inputrc.WithTerm(os.Getenv("TERM")),
		inputrc.WithMode(mode),
	}
}

// apply puts what cfg holds over the keymaps and the variables that are in
// place. What rline does not support goes into the notices.
func (ev *env) apply(cfg *inputrc.Config) {
	ev.ensureKeymaps()
	// The file's bindings go over what is there, after it, because two
	// sequences can decode to one key: \e[A and \eOA are both Up.
	binds := map[string]map[string]inputrc.Bind{
		"emacs":      {},
		"vi-insert":  {},
		"vi-command": {},
	}
	for _, km := range slices.Sorted(maps.Keys(cfg.Binds)) {
		// The meta and Ctrl-X keymaps are the keys after Esc and after
		// Ctrl-X, and vi and vi-move are other names of vi-command.
		name, prefix := keymapAlias(km)
		if binds[name] == nil {
			ev.notices = append(ev.notices, fmt.Sprintf("the keymap %s is not supported", km))
			continue
		}
		for seq, b := range cfg.Binds[km] {
			if !b.Macro && !ev.hasCommand(b.Action) {
				ev.notices = append(ev.notices, fmt.Sprintf("the command %s is not supported", b.Action))
			}
			binds[name][prefix+seq] = b
		}
	}
	bindAll(ev.keymap, binds["emacs"])
	bindAll(ev.viInsert, binds["vi-insert"])
	bindAll(ev.viCommand, binds["vi-command"])

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
	ev.setVars(cfg.Vars)
	slices.Sort(ev.notices)
	ev.notices = slices.Compact(ev.notices)
}

// setVars puts vars over the variables that are set, and does what each of
// them asks of the terminal and of the editing mode.
func (ev *env) setVars(vars map[string]any) {
	if len(vars) == 0 {
		return
	}
	if ev.vars == nil {
		ev.vars = make(map[string]any, len(vars))
	}
	maps.Copy(ev.vars, vars)
	style := bellStyleOf(ev.vars)
	ev.term.beep = ev.beep && style != bellNone
	ev.term.visible = style == bellVisible
	// Only a variable that was set now changes the mode, because
	// vi-editing-mode and emacs-editing-mode change it too, and leave the
	// variable as it was.
	if s, ok := vars["editing-mode"].(string); ok {
		ev.viMode = s == "vi"
	}
}

// keymapAlias returns the keymap that rline keeps for the keymap that an
// inputrc file calls name, and the keys in front of each of its sequences:
// Esc for emacs-meta and Ctrl-X for emacs-ctlx. vi and vi-move are other
// names of vi-command. A name that readline does not have comes back as it
// is.
func keymapAlias(name string) (string, string) {
	switch name {
	case "emacs-standard":
		return "emacs", ""
	case "emacs-meta":
		return "emacs", "\x1b"
	case "emacs-ctlx":
		return "emacs", "\x18"
	case "vi", "vi-move":
		return "vi-command", ""
	}
	return name, ""
}

// keymapNamed returns the keymap that an inputrc file calls name, and the
// keys in front of each of its sequences. An empty name is the keymap of the
// editing mode, as bash's bind uses when it is given none.
func (ev *env) keymapNamed(name string) (*readline.Keymap, string, error) {
	ev.ensureKeymaps()
	if name == "" {
		name = "emacs"
		if ev.viMode {
			name = "vi-insert"
		}
	}
	alias, prefix := keymapAlias(name)
	switch alias {
	case "emacs":
		return ev.keymap, prefix, nil
	case "vi-insert":
		return ev.viInsert, prefix, nil
	case "vi-command":
		return ev.viCommand, prefix, nil
	}
	return nil, "", fmt.Errorf("finding the keymap %s: %w", name, ErrUnknownKeymap)
}

// ensureKeymaps makes readline's default keymaps where there are none yet.
func (ev *env) ensureKeymaps() {
	if ev.keymap != nil && ev.viInsert != nil {
		return
	}
	km, vi, vc := ev.defaultKeymaps()
	if ev.keymap == nil {
		ev.keymap = km
	}
	if ev.viInsert == nil {
		ev.viInsert, ev.viCommand = vi, vc
	}
}

// hasCommand reports whether readline, rline or the program has a command
// called name.
func (ev *env) hasCommand(name string) bool {
	return hasCommand(name) || ev.commands[name] != nil
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

// bind binds seq in keymap to the command called name, or to a macro when
// name is in double quotes, as Session.Bind does (D76).
func (ev *env) bind(keymap, seq, name string) error {
	km, prefix, err := ev.keymapNamed(keymap)
	if err != nil {
		return fmt.Errorf("binding %s: %w", seq, err)
	}
	b := inputrc.Bind{Action: name}
	switch {
	case len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"':
		b = inputrc.Bind{Action: inputrc.Unescape(name[1 : len(name)-1]), Macro: true}
		if _, ok := decodeSeq(b.Action); !ok {
			return fmt.Errorf("binding %s to the macro %s: %w", seq, name, ErrInvalidSequence)
		}
	case !ev.hasCommand(name):
		return fmt.Errorf("binding %s to %s: %w", seq, name, ErrUnknownCommand)
	}
	keys := prefix + inputrc.Unescape(seq)
	if _, ok := decodeSeq(keys); !ok {
		return fmt.Errorf("binding %s: %w", seq, ErrInvalidSequence)
	}
	bindAll(km, map[string]inputrc.Bind{keys: b})
	return nil
}

// unbind takes away what seq is bound to in keymap, as Session.Unbind does
// (D76).
func (ev *env) unbind(keymap, seq string) error {
	km, prefix, err := ev.keymapNamed(keymap)
	if err != nil {
		return fmt.Errorf("unbinding %s: %w", seq, err)
	}
	keys, ok := decodeSeq(prefix + inputrc.Unescape(seq))
	if !ok {
		return fmt.Errorf("unbinding %s: %w", seq, ErrInvalidSequence)
	}
	km.Unbind(keys)
	return nil
}

// bindings returns what keymap binds, as Session.Bindings does (D76).
func (ev *env) bindings(keymap string) ([]Binding, error) {
	km, prefix, err := ev.keymapNamed(keymap)
	if err != nil {
		return nil, fmt.Errorf("listing the bindings: %w", err)
	}
	var out []Binding
	for _, b := range km.Bindings() {
		seq, ok := strings.CutPrefix(b.Seq, prefix)
		if !ok || seq == "" {
			continue
		}
		binding := Binding{Keys: readline.EscapeSeq(seq), Command: b.Name}
		if b.Name == "" {
			binding.Macro = readline.EscapeMacro(b.Macro)
		}
		out = append(out, binding)
	}
	return out, nil
}

// allVars returns every variable of readline with the value it has now:
// readline's default, under what the terminal's terminfo entry sets, under
// what an inputrc file or SetVariable set. The editing mode is the one in
// use.
func (ev *env) allVars() map[string]any {
	vars := inputrc.DefaultVars()
	maps.Copy(vars, ev.termVars)
	maps.Copy(vars, ev.vars)
	vars["editing-mode"] = "emacs"
	if ev.viMode {
		vars["editing-mode"] = "vi"
	}
	return vars
}

// setVariable sets one variable, as a line of an inputrc file sets it, as
// Session.SetVariable does (D76).
func (ev *env) setVariable(name, value string) error {
	if _, ok := inputrc.DefaultVars()[name]; !ok {
		return fmt.Errorf("setting %s: %w", name, ErrUnknownVariable)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("setting %s: %w", name, ErrInvalidValue)
	}
	if err := ev.parseAndBind("set " + name + " " + value); err != nil {
		return fmt.Errorf("setting %s: %w", name, err)
	}
	return nil
}

// parseAndBind reads one line of an inputrc file and applies it, as
// Session.ParseAndBind does (D77). Nothing of the line applies when any of
// it is at fault.
func (ev *env) parseAndBind(line string) error {
	if strings.ContainsAny(line, "\r\n") {
		return ErrInvalidValue
	}
	// The variables in place tell the parser the type of each one, so that
	// a value is read as on or off, or as a number, as readline reads it.
	cfg := inputrc.NewConfig()
	cfg.ReadFileFunc = os.ReadFile
	cfg.Vars = ev.allVars()
	rec := &setRecorder{Config: cfg, set: make(map[string]any)}
	// A binding goes to the keymap of the editing mode unless the line names
	// one, as bash's bind uses it.
	text := line + "\n"
	if ev.viMode {
		text = "set keymap vi-insert\n" + text
	}
	opts := append(ev.parseOptions(), inputrc.WithHaltOnErr(true), inputrc.WithStrict(true))
	if err := inputrc.ParseBytes([]byte(text), rec, opts...); err != nil {
		return fmt.Errorf("parsing %q: %w", line, err)
	}
	for name := range rec.set {
		if _, ok := inputrc.DefaultVars()[name]; !ok {
			return fmt.Errorf("setting %s: %w", name, ErrUnknownVariable)
		}
	}
	for km, binds := range cfg.Binds {
		_, prefix := keymapAlias(km)
		for seq, b := range binds {
			if !b.Macro && !ev.hasCommand(b.Action) {
				return fmt.Errorf("binding %s to %s: %w", readline.EscapeSeq(seq), b.Action, ErrUnknownCommand)
			}
			if _, ok := decodeSeq(prefix + seq); !ok {
				return fmt.Errorf("binding %s: %w", readline.EscapeSeq(seq), ErrInvalidSequence)
			}
			if _, ok := decodeSeq(b.Action); b.Macro && !ok {
				return fmt.Errorf("binding %s to a macro: %w", readline.EscapeSeq(seq), ErrInvalidSequence)
			}
		}
	}
	// Only what the line set goes over the variables, not the ones that
	// were handed to the parser for their types.
	ev.apply(&inputrc.Config{Vars: rec.set, Binds: cfg.Binds})
	return nil
}

// setRecorder is an inputrc configuration that also keeps what was set, so
// that only that goes over the variables of the Session.
type setRecorder struct {
	*inputrc.Config

	set map[string]any
}

// Set satisfies inputrc.Handler.
func (r *setRecorder) Set(name string, value any) error {
	r.set[name] = value
	if err := r.Config.Set(name, value); err != nil {
		return fmt.Errorf("setting %s: %w", name, err)
	}
	return nil
}

// readInputrcFile reads the inputrc file called name over the bindings and
// the variables in place, as Session.ReadInputrc does (D76).
func (ev *env) readInputrcFile(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return fmt.Errorf("reading the inputrc file: %w", err)
	}
	defer f.Close()
	cfg := inputrc.NewConfig()
	cfg.ReadFileFunc = os.ReadFile
	// The parser reads on past a line it cannot parse, as readline does, and
	// keeps the faults.
	p := inputrc.New(append(ev.parseOptions(), inputrc.WithName(name))...)
	err = p.Parse(f, cfg)
	ev.apply(cfg)
	if err = errors.Join(append([]error{err}, p.Errs()...)...); err != nil {
		return fmt.Errorf("reading the inputrc file %s: %w", name, err)
	}
	return nil
}

// varString writes the value of a variable as readline prints it: on or off,
// a number, or the text.
func varString(v any) string {
	switch v := v.(type) {
	case bool:
		if v {
			return "on"
		}
		return "off"
	case int:
		return strconv.Itoa(v)
	case string:
		return v
	}
	return fmt.Sprint(v)
}
