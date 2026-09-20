// The completion menu: what is drawn below the line when there is more than
// one answer, and the key loop that walks it.
//
// The menu is not a separate drawing surface. It writes into the editor's
// extra buffer and calls the same refresh the rest of the editor uses, so what
// appears below the line is the same mechanism as the hint help. What it does
// have of its own is a key loop: it reads the tty directly rather than going
// through readKey, which is what the C does.
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
		e.Extra.Appendf("[ic-info]%s%d [/]", marker, 1+index)
		width -= 3
	}
	if width > 0 {
		e.Extra.Appendf("[width=\"%d;left; ;on\"]", width)
	}
	if selected {
		e.Extra.AppendString("[ic-emphasis]")
	}
	e.Extra.AppendString(display)
	if selected {
		e.Extra.AppendString("[/ic-emphasis]")
	}
	// An empty help is treated as no help. The C keeps an absent help apart
	// from an empty one and draws the empty one as two spaces and an empty
	// tag, but a Go caller cannot pass the absence of a string, the same way
	// it cannot for the display.
	if help != "" {
		e.Extra.AppendString("  ")
		appendTagged(&e.Extra, "ic-info", help)
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

// completionMenu draws the list of completions below the line and reads the
// keys that move around it.
//
// moreAvailable says the completer stopped early because it had offered as
// many as it was asked for, so there may be others it never reached.
func (ev *env) completionMenu(e *editor.Editor, moreAvailable bool) {
	count := ev.completions.count()
	// How many of them the layout chose to show. Every branch of the layout
	// sets it before anything reads it, which is why it starts at nothing
	// rather than at count as the C does: the C's initial value is dead, and
	// keeping it would have hidden that the one comparison which could have
	// read it never does. See the note in PLAN.md.
	var countDisplayed int
	// The first entry is selected up front only when there is no preview to
	// show; with preview on, nothing is selected until the user moves.
	selected := -1
	if ev.completeNoPreview {
		selected = 0
	}

	// The key that ended the menu, which is given back to the edit loop
	// unless one of the branches below dealt with it and set it to zero.
	var c key.Code
	for {
		e.Extra.Clear()
		// One less than the terminal is wide, so that a full row does not
		// wrap.
		twidth := ev.term.width - 1
		col3 := 3 + ev.completionsMaxWidth(9)
		col2 := 3 + ev.completionsMaxWidth(8)
		switch {
		case count > 3 && col3*3+2*2 < twidth:
			// Three columns, filled down each column rather than across.
			countDisplayed = min(count, 9)
			perColumn := 3
			for rw := range perColumn {
				if rw > 0 {
					e.Extra.AppendString("\n")
				}
				ev.appendCompletionRow(e, col3, selected,
					rw, perColumn+rw, (2*perColumn)+rw)
			}
		case count > 4 && col2*2+2 < twidth:
			// Two columns, because something was too wide for three.
			countDisplayed = min(count, 8)
			perColumn := 4
			if countDisplayed <= 6 {
				perColumn = 3
			}
			for rw := range perColumn {
				if rw > 0 {
					e.Extra.AppendString("\n")
				}
				ev.appendCompletionRow(e, col2, selected, rw, perColumn+rw)
			}
		default:
			// One per row.
			countDisplayed = min(count, 9)
			for i := range countDisplayed {
				if i > 0 {
					e.Extra.AppendString("\n")
				}
				ev.appendCompletion(e, i, -1, true, selected == i)
			}
		}
		if count > countDisplayed {
			if moreAvailable {
				e.Extra.AppendString(
					"\n[ic-info](press page-down (or ctrl-j) to see all further completions)[/]")
			} else {
				e.Extra.Appendf(
					"\n[ic-info](press page-down (or ctrl-j) to see all %d completions)[/]", count)
			}
		}
		// The "or equal" is in the C, and reaches one past the last entry
		// shown. Applying that entry fails harmlessly, so it only means the
		// line is drawn again rather than previewed.
		if !ev.completeNoPreview && selected >= 0 && selected <= countDisplayed {
			// Show what picking this entry would do by actually doing it, and
			// then take it straight back out again. The line on screen and
			// the line in the buffer are the same thing, so the undo stack is
			// what keeps the real line safe while the menu is open.
			if ev.complete(e, selected) {
				e.UndoRestore(false)
			}
		} else {
			ev.refresh(e)
		}

		// Read the next key here rather than through readKey. There is no
		// hint while the menu is open, so the hint delay readKey adds has
		// nothing to wait for, and if one ever did linger readKey would draw
		// it over the menu. The resize check readKey would have done is done
		// here instead, which is what the C does.
		c = ev.tty.read()
		if ev.tty.resizeEvent() {
			ev.resize(e)
		}
		e.Extra.Clear()

		// A digit picks that entry outright.
		if c >= '1' && c <= '9' {
			if i := int(c - '1'); i < count {
				selected = i
				c = key.Enter
			}
		}

		switch {
		case c == key.Down || c == key.Tab:
			selected++
			if selected >= countDisplayed {
				selected = 0
			}
			continue
		case c == key.Up || c == key.ShiftTab:
			selected--
			if selected < 0 {
				selected = countDisplayed - 1
			}
			continue
		case c == key.F1:
			ev.showHelp(e)
			continue

		case c == key.Esc:
			ev.completions.clear()
			ev.refresh(e)
			c = 0

		case selected >= 0 && (c == key.Enter || c == key.Right || c == key.End):
			c = 0
			if ev.complete(e, selected) && ev.completeAutoTab {
				// Try to complete again straight away.
				ev.tty.pushCode(key.EventAutoTab)
			}

		case !ev.completeNoPreview && !c.IsVirtKey():
			// The previewed entry is what the user was looking at, so typing
			// anything else takes it and leaves the menu, and the key that
			// was typed is handled by the edit loop afterwards.
			ev.complete(e, selected)

		case (c == key.PageDown || c == key.Linefeed) && count > 9:
			c = 0
			ev.showAllCompletions(e, &count, moreAvailable)

		default:
			ev.refresh(e)
		}
		break
	}

	ev.completions.clear()
	if c != 0 {
		ev.tty.pushCode(c)
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
		ev.bb.println("[ic-info]... and more.[/]")
	} else {
		ev.bb.print(fmt.Sprintf("[ic-info](%d possible completions)[/]\n", *count))
	}
	for range rc.Row + 1 {
		ev.term.write(" \n")
	}
	e.CurRows = 0
	ev.refresh(e)
}
