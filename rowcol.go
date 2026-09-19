package rline

// Rows and columns. The edit loop draws a line that can be longer than the
// terminal is wide and that can hold newlines, so it needs to know which
// screen row and column a position in the buffer lands on.
//
// Ported from isocline/src/stringbuf.c.

// rowCol says where a position sits on the screen, and how long its row is.
type rowCol struct {
	row      int
	col      int
	rowStart int
	rowLen   int

	firstOnRow bool
	lastOnRow  bool
}

// rowFunc is called once for each screen row. s is the whole text, rowStart
// and rowLen give the row inside it, and startWidth is the width of the
// prompt in front of the row. isWrap says the row ended because it filled the
// terminal, not because of a newline. Returning true stops the walk.
type rowFunc func(s []byte, row, rowStart, rowLen, startWidth int, isWrap bool) bool

// forEachRow calls fn once for each screen row of s and returns the number of
// rows. termWidth of 0 means do not wrap. promptWidth is the width of the
// prompt on the first row, and contWidth the width of the prompt on every row
// after it.
//
// A row that wraps and then ends in a newline calls fn twice, once for each,
// and both calls carry the startWidth that was current before the wrap. The C
// code computes that value at the top of the loop and does not recompute it
// after the wrap, so the second call reports the prompt width of the row the
// text came from rather than of the row it moved to.
//
// When fn stops the walk the result is the row it stopped on, which is one
// less than the count that a complete walk returns for the same text.
func forEachRow(s []byte, termWidth, promptWidth, contWidth int, fn rowFunc) int {
	count, col, start := 0, 0, 0
	startWidth := promptWidth
	i := 0
	for i < len(s) {
		next, w := nextOfs(s, i)
		if next <= 0 {
			// Unreachable for a slice, because nextOfs returns at least one
			// byte for every position inside it. The C code asserts here.
			break
		}
		if count == 0 {
			startWidth = promptWidth
		} else {
			startWidth = contWidth
		}
		// The +1 leaves room for the cursor, which sits after the character.
		termCol := col + w + startWidth + 1
		if termWidth != 0 && i != 0 && termCol >= termWidth {
			if fn != nil && fn(s, count, start, i-start, startWidth, true) {
				return count
			}
			count++
			start = i
			col = 0
		}
		if s[i] == '\n' {
			if fn != nil && fn(s, count, start, i-start, startWidth, false) {
				return count
			}
			count++
			start = i + 1
			col = 0
		}
		i += next
		col += w
	}
	if fn != nil && fn(s, count, start, i-start, startWidth, false) {
		return count
	}
	return count + 1
}

// rowColAtPos returns the number of rows that s takes, and where pos sits.
func rowColAtPos(s []byte, termWidth, promptWidth, contWidth, pos int) (int, rowCol) {
	var rc rowCol
	rows := forEachRow(s, termWidth, promptWidth, contWidth,
		func(s []byte, row, rowStart, rowLen, _ int, isWrap bool) bool {
			if pos < rowStart || pos > rowStart+rowLen {
				return false // keep going, so that every row is counted
			}
			rc.rowStart = rowStart
			rc.rowLen = rowLen
			rc.row = row
			rc.col = strWidth(s[rowStart:pos])
			rc.firstOnRow = pos == rowStart
			if isWrap {
				// On a wrapped row the last position is the one whose
				// character reaches the end of the row.
				next, _ := nextOfs(s[:rowStart+rowLen], pos)
				rc.lastOnRow = pos+next >= rowStart+rowLen
			} else {
				rc.lastOnRow = pos >= rowStart+rowLen
			}
			return false
		})
	return rows, rc
}

// posAtRowCol returns the position in s that sits at row and col, or -1 when
// s has no such row. col does not count the prompt.
func posAtRowCol(s []byte, termWidth, promptWidth, contWidth, row, col int) int {
	pos := -1
	forEachRow(s, termWidth, promptWidth, contWidth,
		func(s []byte, r, rowStart, rowLen, _ int, _ bool) bool {
			if r != row {
				return false
			}
			w, i := 0, rowStart
			end := rowStart + rowLen
			for w < col && i < end {
				next, cw := nextOfs(s[:end], i)
				if next <= 0 {
					break
				}
				i += next
				w += cw
			}
			pos = i
			return true
		})
	return pos
}

// wrappedRowColAtPos returns where pos sits after the terminal is resized to
// newTermWidth, and how many rows the text then takes.
//
// The text was laid out for termWidth and the terminal has not redrawn it, so
// each of those rows can spill over several rows of the new width. Those
// extra rows are hard wraps: the terminal broke the line itself, with no
// newline in the text.
func wrappedRowColAtPos(s []byte, termWidth, newTermWidth, promptWidth, contWidth, pos int) (int, rowCol) {
	var rc rowCol
	hardRows := 0
	rows := forEachRow(s, termWidth, promptWidth, contWidth,
		func(s []byte, row, rowStart, rowLen, startWidth int, isWrap bool) bool {
			width := startWidth
			// The walk goes one past the last character, because the cursor
			// can sit just after it.
			for i := 0; i <= rowLen; {
				var cw, next int
				isCursor := pos == rowStart+i
				if i < rowLen {
					next, cw = nextOfs(s[rowStart:rowStart+rowLen], i)
				} else {
					// A wrap draws a back arrow and carries an invisible
					// newline, so it takes two columns.
					switch {
					case isWrap:
						cw = 2
					case isCursor:
						cw = 1
					}
					next = 1
				}
				if next > 0 {
					if width+cw > newTermWidth {
						width = 0
						hardRows++
					}
				} else {
					next++ // make sure the walk ends
				}
				if isCursor {
					rc.rowStart = rowStart
					rc.rowLen = rowLen
					rc.row = hardRows + row
					rc.col = width
					rc.firstOnRow = i == 0
					last := rowLen
					if isWrap {
						last--
					}
					rc.lastOnRow = i+next >= last
				}
				width += cw
				i += next
			}
			return false
		})
	return rows + hardRows, rc
}
