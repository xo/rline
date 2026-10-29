// The completion menu: what is drawn below the line when there is more than
// one answer, and what the keys do while it is open (D51).
//
// The menu is not a separate drawing surface. It writes into the editor's
// extra buffer and calls the same refresh the rest of the editor uses, so what
// appears below the line is the same mechanism as the hint help. It has no key
// loop of its own either. The C reads the decoder directly while the menu is
// open, and here the edit loop reads every key and hands it to the menu.
//
// Sharing the extra buffer with the hint help would be a trap if both could
// hold something at once: refresh draws the help in front of the extra and
// then takes that many bytes off the front of the extra on its way out, so a
// leftover help would eat the start of the menu. It cannot happen, because the
// edit loop clears the hint and its help before it dispatches the key that
// opens the menu, and the C clears it in the same place. The menu therefore
// says nothing about the hint, and that is deliberate rather than an
// oversight.
//
// The list itself, and the completers that fill it, are in comp.go.
//
// Ported from isocline/src/editline_completion.c.

package rline

import (
	"fmt"

	"github.com/xo/rline/internal/editor"
	"github.com/xo/rline/internal/text"
	"github.com/xo/rline/key"
)

// appendTagged writes content wrapped in a markup tag.
func appendTagged(b *text.Buffer, tag, content string) {
	b.Appendf("[%s]", tag)
	b.AppendString(content)
	b.AppendString("[/]")
}

// appendCompletion writes one entry of the menu into the extra buffer.
//
// A width above zero puts the entry in a column of that width, so that the
// entries line up. Below zero means a plain list, where each entry takes as
// much room as it needs.
func (ev *env) appendCompletion(e *editor.Editor, index, width int, numbered, selected bool) {
	display, help, ok := ev.completions.displayAt(index)
	if !ok {
		return
	}
	if numbered {
		marker := " "
		if selected {
			marker = "*"
			if ev.ttyIsUTF8() {
				marker = "→"
			}
		}
		e.Extra.Appendf("[rline-info]%s%d [/]", marker, 1+index)
		width -= 3
	}
	if width > 0 {
		e.Extra.Appendf("[width=\"%d;left; ;on\"]", width)
	}
	if selected {
		e.Extra.AppendString("[rline-emphasis]")
	}
	e.Extra.AppendString(display)
	if selected {
		e.Extra.AppendString("[/rline-emphasis]")
	}
	// An empty help is treated as no help. The C keeps an absent help apart
	// from an empty one and draws the empty one as two spaces and an empty
	// tag, but a Go caller cannot pass the absence of a string, the same way
	// it cannot for the display.
	if help != "" {
		e.Extra.AppendString("  ")
		appendTagged(&e.Extra, "rline-info", help)
	}
	if width > 0 {
		e.Extra.AppendString("[/width]")
	}
}

// appendCompletionRow writes one row of a two or three column menu.
func (ev *env) appendCompletionRow(e *editor.Editor, colWidth, selected int, indexes ...int) {
	for i, index := range indexes {
		if i > 0 {
			e.Extra.AppendString("  ")
		}
		ev.appendCompletion(e, index, colWidth, true, index == selected)
	}
}

// completionsMaxWidth is how much room the widest of the first count entries
// needs, help included.
func (ev *env) completionsMaxWidth(count int) int {
	maxWidth := 0
	for i := range count {
		display, help, ok := ev.completions.displayAt(i)
		if !ok {
			// The C measures a null display, which reads as nothing wide.
			continue
		}
		w := ev.bb.columnWidth(display)
		// An empty help adds nothing, where the C adds the two spaces it
		// would be drawn behind. Same conflation as in appendCompletion.
		if help != "" {
			w += 2 + ev.bb.columnWidth(help)
		}
		maxWidth = max(maxWidth, w)
	}
	return maxWidth
}

// menuAction is what a key does while the menu is open.
type menuAction int

const (
	// menuOther closes the menu, and the edit loop runs the key.
	menuOther menuAction = iota
	menuNext
	menuPrevious
	menuHelp
	menuCancel
	menuAccept
	menuShowAll
)

// menuKeys is the keymap of the menu. A digit picks an entry by its number,
// and is not in it. The keys are isocline's, and no inputrc file binds them,
// because readline has no menu (D51).
var menuKeys = map[key.Code]menuAction{
	key.Tab:      menuNext,
	key.Down:     menuNext,
	key.ShiftTab: menuPrevious,
	key.Up:       menuPrevious,
	key.F1:       menuHelp,
	key.Esc:      menuCancel,
	key.Enter:    menuAccept,
	key.Right:    menuAccept,
	key.End:      menuAccept,
	key.PageDown: menuShowAll,
	key.Linefeed: menuShowAll,
}

// completionMenu is the state of an open menu.
type completionMenu struct {
	// count is how many completions there are, and displayed how many of
	// them the layout chose to show. Every branch of the layout sets
	// displayed before anything reads it, which is why it starts at nothing
	// rather than at count as the C does: the C's initial value is dead, and
	// keeping it would have hidden that the one comparison which could have
	// read it never does. See the note in docs/PORT.md.
	count, displayed int

	// selected is the entry that is selected, or -1 for none.
	selected int

	// more says the completer stopped early because it had offered as many
	// as it was asked for, so there may be others it never reached.
	more bool
}

// openMenu opens the menu of the completions, and draws it below the line.
//
// The menu reads no keys of its own (D51). The edit loop reads them, and
// hands each one to menuKey while the menu is open.
func (ev *env) openMenu(e *editor.Editor, more bool) {
	m := &completionMenu{count: ev.completions.count(), more: more}
	// The first entry is selected up front only when there is no preview to
	// show; with preview on, nothing is selected until the user moves.
	m.selected = -1
	if !ev.completePreview {
		m.selected = 0
	}
	ev.menu = m
	ev.drawMenu(e)
}

// drawMenu draws the menu below the line, and the line with the selected
// entry put in when the preview is on.
func (ev *env) drawMenu(e *editor.Editor) {
	m := ev.menu
	e.Extra.Clear()
	// One less than the terminal is wide, so that a full row does not
	// wrap.
	twidth := ev.term.width - 1
	col3 := 3 + ev.completionsMaxWidth(9)
	col2 := 3 + ev.completionsMaxWidth(8)
	switch {
	case m.count > 3 && col3*3+2*2 < twidth:
		// Three columns, filled down each column rather than across.
		m.displayed = min(m.count, 9)
		perColumn := 3
		for rw := range perColumn {
			if rw > 0 {
				e.Extra.AppendString("\n")
			}
			ev.appendCompletionRow(e, col3, m.selected,
				rw, perColumn+rw, (2*perColumn)+rw)
		}
	case m.count > 4 && col2*2+2 < twidth:
		// Two columns, because something was too wide for three.
		m.displayed = min(m.count, 8)
		perColumn := 4
		if m.displayed <= 6 {
			perColumn = 3
		}
		for rw := range perColumn {
			if rw > 0 {
				e.Extra.AppendString("\n")
			}
			ev.appendCompletionRow(e, col2, m.selected, rw, perColumn+rw)
		}
	default:
		// One per row.
		m.displayed = min(m.count, 9)
		for i := range m.displayed {
			if i > 0 {
				e.Extra.AppendString("\n")
			}
			ev.appendCompletion(e, i, -1, true, m.selected == i)
		}
	}
	if m.count > m.displayed {
		if m.more {
			e.Extra.AppendString(
				"\n[rline-info](press page-down (or ctrl-j) to see all further completions)[/]")
		} else {
			e.Extra.Appendf(
				"\n[rline-info](press page-down (or ctrl-j) to see all %d completions)[/]", m.count)
		}
	}
	// The "or equal" is in the C, and reaches one past the last entry
	// shown. Applying that entry fails harmlessly, so it only means the
	// line is drawn again rather than previewed.
	if ev.completePreview && m.selected >= 0 && m.selected <= m.displayed {
		// Show what picking this entry would do by actually doing it, and
		// then take it straight back out again. The line on screen and
		// the line in the buffer are the same thing, so the undo stack is
		// what keeps the real line safe while the menu is open.
		if ev.complete(e, m.selected) {
			e.UndoRestore(false)
		}
	} else {
		ev.refresh(e)
	}
}

// menuKey takes one key while the menu is open. A key that moves the
// selection draws the menu again. Any other key closes it, and a key that the
// menu does not take is pushed back, so that the edit loop reads it next.
func (ev *env) menuKey(e *editor.Editor, c key.Code) {
	m := ev.menu
	e.Extra.Clear()

	action := menuKeys[c]
	// A digit picks that entry outright.
	if c >= '1' && c <= '9' {
		if i := int(c - '1'); i < m.count {
			m.selected = i
			c, action = key.Enter, menuAccept
		}
	}

	switch {
	case action == menuNext:
		m.selected++
		if m.selected >= m.displayed {
			m.selected = 0
		}
		ev.drawMenu(e)
		return
	case action == menuPrevious:
		m.selected--
		if m.selected < 0 {
			m.selected = m.displayed - 1
		}
		ev.drawMenu(e)
		return
	case action == menuHelp:
		ev.showHelp(e)
		ev.drawMenu(e)
		return

	case action == menuCancel:
		ev.completions.clear()
		ev.refresh(e)
		c = 0

	case m.selected >= 0 && action == menuAccept:
		c = 0
		if ev.complete(e, m.selected) && ev.completeAutoTab {
			// Try to complete again straight away.
			ev.pushAutoTab()
		}

	case ev.completePreview && !c.IsVirtKey():
		// The previewed entry is what the user was looking at, so typing
		// anything else takes it and leaves the menu, and the key that
		// was typed is handled by the edit loop afterwards.
		ev.complete(e, m.selected)

	case action == menuShowAll && m.count > 9:
		c = 0
		ev.showAllCompletions(e, &m.count, m.more)

	default:
		ev.refresh(e)
	}

	ev.menu = nil
	ev.completions.clear()
	if c != 0 {
		ev.keys.pushCode(c)
	}
}

// showAllCompletions prints every completion above the line, rather than the
// first nine below it.
func (ev *env) showAllCompletions(e *editor.Editor, count *int, moreAvailable bool) {
	if moreAvailable {
		// Ask for the rest of them before showing the list.
		*count = ev.completions.generate(e.Input.String(), e.Pos, maxCompletionsToShow)
	}
	_, rc := ev.rowCol(e)
	ev.clear(e)
	ev.writePrompt(e, 0, false)
	ev.term.writeln("")
	for i := range *count {
		if display, _, ok := ev.completions.displayAt(i); ok {
			ev.bb.println(display)
		}
	}
	if *count >= maxCompletionsToShow {
		ev.bb.println("[rline-info]... and more.[/]")
	} else {
		ev.bb.print(fmt.Sprintf("[rline-info](%d possible completions)[/]\n", *count))
	}
	for range rc.Row + 1 {
		ev.term.write(" \n")
	}
	e.CurRows = 0
	ev.refresh(e)
}
