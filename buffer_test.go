package rline

import (
	"strings"
	"testing"
)

// TestBufferEdits checks the edit operations that the corpus does not reach,
// because the C probe builds every buffer the same way and then makes one
// call. These build a buffer up and take it apart again.
func TestBufferEdits(t *testing.T) {
	t.Parallel()
	var b buffer
	if got := b.length(); got != 0 {
		t.Fatalf("the zero value has length %d, want 0", got)
	}
	if got := b.appendString("hello"); got != 5 {
		t.Errorf("appendString gave %d, want 5", got)
	}
	if got := b.appendByte(' '); got != 6 {
		t.Errorf("appendByte gave %d, want 6", got)
	}
	if got := b.appendf("%s %d", "world", 42); got != 14 {
		t.Errorf("appendf gave %d, want 14", got)
	}
	if got, want := b.string(), "hello world 42"; got != want {
		t.Fatalf("the buffer holds %q, want %q", got, want)
	}
	if got := b.insertAt("big ", 6); got != 10 {
		t.Errorf("insertAt gave %d, want 10", got)
	}
	if got, want := b.string(), "hello big world 42"; got != want {
		t.Fatalf("after insertAt the buffer holds %q, want %q", got, want)
	}
	b.deleteFromTo(6, 10)
	if got, want := b.string(), "hello world 42"; got != want {
		t.Fatalf("after deleteFromTo the buffer holds %q, want %q", got, want)
	}
	b.deleteFrom(11)
	if got, want := b.string(), "hello world"; got != want {
		t.Fatalf("after deleteFrom the buffer holds %q, want %q", got, want)
	}
	rest := b.splitAt(5)
	if got, want := b.string(), "hello"; got != want {
		t.Errorf("after splitAt the left side holds %q, want %q", got, want)
	}
	if got, want := rest.string(), " world"; got != want {
		t.Errorf("after splitAt the right side holds %q, want %q", got, want)
	}
	b.replace("done")
	if got, want := b.string(), "done"; got != want {
		t.Fatalf("after replace the buffer holds %q, want %q", got, want)
	}
	if got, want := b.charAt(0), byte('d'); got != want {
		t.Errorf("charAt(0) = %q, want %q", got, want)
	}
	if got, want := b.charAt(4), byte(0); got != want {
		t.Errorf("charAt at the end = %d, want %d", got, want)
	}
	b.clear()
	if got := b.length(); got != 0 {
		t.Errorf("after clear the buffer has length %d, want 0", got)
	}
}

// TestBufferInsertStopsAtZero checks that an insert stops at a zero byte, the
// way the C code does when it measures the string it was handed. Everything
// else counts on a buffer never holding one.
func TestBufferInsertStopsAtZero(t *testing.T) {
	t.Parallel()
	var b buffer
	if got := b.insertAt("ab\x00cd", 0); got != 2 {
		t.Errorf("insertAt gave %d, want 2", got)
	}
	if got, want := b.string(), "ab"; got != want {
		t.Errorf("the buffer holds %q, want %q", got, want)
	}
	if got := b.insertByteAt(0, 1); got != 1 {
		t.Errorf("insertByteAt of a zero byte gave %d, want 1", got)
	}
	if got, want := b.string(), "ab"; got != want {
		t.Errorf("a zero byte changed the buffer to %q, want %q", got, want)
	}
}

// TestBufferRunesAndCharacters checks the operations that move by character
// over text that is not ASCII.
func TestBufferRunesAndCharacters(t *testing.T) {
	t.Parallel()
	var b buffer
	pos := b.insertRuneAt('日', 0)
	if got, want := pos, 3; got != want {
		t.Errorf("insertRuneAt gave %d, want %d", got, want)
	}
	if got, want := b.insertRuneAt('a', pos), 4; got != want {
		t.Errorf("insertRuneAt gave %d, want %d", got, want)
	}
	if got, want := b.string(), "日a"; got != want {
		t.Fatalf("the buffer holds %q, want %q", got, want)
	}
	if got, width := b.next(0); got != 3 || width != 2 {
		t.Errorf("next(0) = (%d, %d), want (3, 2)", got, width)
	}
	if got, width := b.prev(3); got != 0 || width != 2 {
		t.Errorf("prev(3) = (%d, %d), want (0, 2)", got, width)
	}
	if got, _ := b.prev(0); got != -1 {
		t.Errorf("prev(0) = %d, want -1", got)
	}
	if got := b.swapChar(3); got != 0 {
		t.Errorf("swapChar(3) = %d, want 0", got)
	}
	if got, want := b.string(), "a日"; got != want {
		t.Errorf("after swapChar the buffer holds %q, want %q", got, want)
	}
	b.deleteCharAt(1)
	if got, want := b.string(), "a"; got != want {
		t.Errorf("after deleteCharAt the buffer holds %q, want %q", got, want)
	}
	if got := b.deleteCharBefore(1); got != 0 {
		t.Errorf("deleteCharBefore(1) = %d, want 0", got)
	}
	if got := b.length(); got != 0 {
		t.Errorf("the buffer has length %d, want 0", got)
	}
}

// TestBufferDecodeFromLocale checks what survives for a terminal that does not
// read UTF-8.
func TestBufferDecodeFromLocale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty gives nothing at all", "", ""},
		{"ascii survives", "abc", "abc"},
		{"an escape sequence is dropped", "a\x1b[31mb", "ab"},
		{"a character outside ascii is dropped", "a日b", "ab"},
		{"a raw byte comes back", "a" + string(appendRune(nil, rawRune(0xFF))) + "b", "a\xffb"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b := buffer{}
			b.appendString(test.in)
			got := b.decodeFromLocale()
			if test.in == "" {
				if got != nil {
					t.Errorf("decodeFromLocale = %q, want nothing at all", got)
				}
				return
			}
			if string(got) != test.want {
				t.Errorf("decodeFromLocale = %q, want %q", got, test.want)
			}
		})
	}
}

// TestBufferForwardsToTheFunctions checks that each buffer method reaches the
// function it is named after. A method wired to the wrong one would still
// return a plausible number, and the corpus calls the functions rather than
// the methods, so nothing else catches it.
func TestBufferForwardsToTheFunctions(t *testing.T) {
	t.Parallel()
	const text = "one two\nthree four\nfive"
	var b buffer
	b.appendString(text)
	s := []byte(text)

	finders := []struct {
		name   string
		method func(int) int
		fn     func([]byte, int) int
	}{
		{"findLineStart", b.findLineStart, findLineStart},
		{"findLineEnd", b.findLineEnd, findLineEnd},
		{"findWordStart", b.findWordStart, findWordStart},
		{"findWordEnd", b.findWordEnd, findWordEnd},
		{"findWSWordStart", b.findWSWordStart, findWSWordStart},
		{"findWSWordEnd", b.findWSWordEnd, findWSWordEnd},
	}
	for _, f := range finders {
		for pos := range len(s) + 1 {
			if got, want := f.method(pos), f.fn(s, pos); got != want {
				t.Errorf("%s(%d) = %d, but the function gives %d", f.name, pos, got, want)
			}
		}
	}

	const termw, promptw, contw, newtermw = 10, 3, 2, 6
	for pos := range len(s) + 1 {
		gotRows, gotRC := b.rowColAtPos(termw, promptw, contw, pos)
		wantRows, wantRC := rowColAtPos(s, termw, promptw, contw, pos)
		if gotRows != wantRows || gotRC != wantRC {
			t.Errorf("rowColAtPos(%d) = %d %+v, but the function gives %d %+v",
				pos, gotRows, gotRC, wantRows, wantRC)
		}
		gotRows, gotRC = b.wrappedRowColAtPos(termw, newtermw, promptw, contw, pos)
		wantRows, wantRC = wrappedRowColAtPos(s, termw, newtermw, promptw, contw, pos)
		if gotRows != wantRows || gotRC != wantRC {
			t.Errorf("wrappedRowColAtPos(%d) = %d %+v, but the function gives %d %+v",
				pos, gotRows, gotRC, wantRows, wantRC)
		}
	}
	for row := range 4 {
		for col := range 8 {
			got := b.posAtRowCol(termw, promptw, contw, row, col)
			want := posAtRowCol(s, termw, promptw, contw, row, col)
			if got != want {
				t.Errorf("posAtRowCol(%d, %d) = %d, but the function gives %d", row, col, got, want)
			}
		}
	}

	var rows []string
	count := b.forEachRow(termw, promptw, contw, func(s []byte, _, rowStart, rowLen, _ int, _ bool) bool {
		rows = append(rows, string(s[rowStart:rowStart+rowLen]))
		return false
	})
	if count != len(rows) {
		t.Errorf("forEachRow counted %d rows but called back %d times", count, len(rows))
	}
	// Every byte of the text, apart from the newlines that end a row, has to
	// appear in exactly one row.
	if got, want := strings.Join(rows, ""), strings.ReplaceAll(text, "\n", ""); got != want {
		t.Errorf("the rows join to %q, want %q", got, want)
	}
}

// TestBufferBytesAlias records that bytes returns the buffer itself, not a
// copy, so a later edit is visible through it.
func TestBufferBytesAlias(t *testing.T) {
	t.Parallel()
	var b buffer
	b.appendString("abc")
	got := b.bytes()
	got[0] = 'x'
	if b.string() != "xbc" {
		t.Errorf("the buffer holds %q, want it to share storage with bytes", b.string())
	}
}
