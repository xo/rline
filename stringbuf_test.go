package rline

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"testing"
)

const (
	// sbufCorpusPath holds what the C functions in stringbuf.c returned.
	sbufCorpusPath = "testdata/stringbuf.txt"

	// sbufDeltaPath holds the recorded calls whose answer differs because the
	// port measures character width with go-runewidth.
	sbufDeltaPath = "testdata/stringbuf-delta.txt"

	// sbufProbePath is where tools/build-probe-stringbuf.sh puts the probe.
	sbufProbePath = ".build/probe-stringbuf"
)

// tokenClasses is the order that the probe names a character class by.
var tokenClasses = []charClass{
	charIsLetter, charIsIDLetter, charIsNonWhite, charIsNonSeparator, charIsDigit,
}

// TestStringbufPort replays every recorded call to a C function in
// stringbuf.c and checks that the Go port answers the same.
//
// A difference is a failure, with one exception. The port does not carry the
// width table of the C code, so anything that adds up column widths differs
// for a string that holds a character the two tables disagree about. Those
// calls go to testdata/stringbuf-delta.txt instead, and the test checks that
// the file has not changed. A call that differs for any other reason fails,
// and so does a width difference in a string that holds no such character.
func TestStringbufPort(t *testing.T) {
	if *update {
		regenerateStringbuf(t)
	}
	b, err := os.ReadFile(sbufCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-stringbuf.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) < 10000 {
		t.Fatalf("the corpus holds %d lines, which is too few to prove anything", len(lines))
	}
	ranges := readWidthRanges(t, widthPath)

	// A string is checked for width divergence once, not once per line.
	diverges := make(map[string]bool, 64)
	divergesIn := func(s []byte) bool {
		key := string(s)
		d, ok := diverges[key]
		if !ok {
			d = widthDiverges(ranges, s)
			diverges[key] = d
		}
		return d
	}

	counts := make(map[string]int, 32)
	var deltas []string
	bad := 0
	for i, line := range lines {
		kind, hard, soft := checkStringbufLine(t, line)
		counts[kind]++
		switch {
		case hard != "":
			bad++
			if bad <= maxReported {
				t.Errorf("%s:%d: %s\n  %s", sbufCorpusPath, i+1, hard, line)
			}
		case soft != "":
			if !divergesIn(corpusOf(t, line)) {
				bad++
				if bad <= maxReported {
					t.Errorf("%s:%d: %s\n  (no character here has a disputed width, so this is a port bug)\n  %s",
						sbufCorpusPath, i+1, soft, line)
				}
				continue
			}
			deltas = append(deltas, line)
		}
	}
	if bad > maxReported {
		t.Errorf("%d differences in total, %d shown", bad, maxReported)
	}
	// Guard the corpus. A probe that stopped emitting a section would
	// otherwise make this test pass while checking nothing.
	for _, kind := range []string{
		"width", "fit", "next", "prev", "esc", "find", "rc", "posrc", "wrc",
		"ins", "del", "swap", "delbefore", "delat", "split", "bnext", "bprev",
		"charat", "fromutf8", "overlap", "istoken", "mtoken", "manytoken",
		"prevchar", "nextchar", "class", "atoz", "atoz2", "atou32",
	} {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
		}
	}
	if bad > 0 {
		return
	}
	checkStringbufDelta(t, deltas)
	t.Logf("checked %d calls, %d of them differ by width alone", len(lines), len(deltas))
}

// checkStringbufDelta compares the calls that differ by width against the
// committed record of them.
func checkStringbufDelta(t *testing.T, deltas []string) {
	t.Helper()
	slices.Sort(deltas)
	got := strings.Join(deltas, "\n")
	if got != "" {
		got += "\n"
	}
	if *update {
		if err := os.WriteFile(sbufDeltaPath, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", sbufDeltaPath, err)
		}
		t.Logf("wrote %s: %d calls", sbufDeltaPath, len(deltas))
		return
	}
	want, err := os.ReadFile(sbufDeltaPath)
	if err != nil {
		t.Fatalf("reading %s: %v (run go test -update)", sbufDeltaPath, err)
	}
	if got == string(want) {
		return
	}
	a, b := strings.Split(got, "\n"), strings.Split(string(want), "\n")
	for i := range max(len(a), len(b)) {
		x, y := lineAt(a, i), lineAt(b, i)
		if x != y {
			t.Fatalf("the set of calls that differ by width changed.\n"+
				"%s line %d\n  now:      %s\n  recorded: %s\n"+
				"If go-runewidth was upgraded, check what moved, then run go test -update.",
				sbufDeltaPath, i+1, x, y)
		}
	}
}

// corpusOf returns the string that a recorded line applies to, which is
// always its second field.
func corpusOf(t *testing.T, line string) []byte {
	t.Helper()
	f := strings.Fields(line)
	if len(f) < 2 {
		return nil
	}
	return mustHex(t, f[1])
}

// cRuneWidth returns the width that the C table gives r.
func cRuneWidth(ranges []widthRange, r rune) int {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].Hi >= r })
	if i < len(ranges) && r >= ranges[i].Lo {
		return ranges[i].Width
	}
	return 1
}

// cCharWidth is charWidth, measured with the C table instead of runeWidth.
// It repeats the decoding of utf8CharWidth on purpose, so that the test can
// tell a width difference from a decoding difference.
func cCharWidth(ranges []widthRange, s []byte) int {
	if len(s) == 0 || s[0] < ' ' {
		return 0
	}
	b := s[0]
	switch {
	case b <= 0xC1:
		return 1
	case b <= 0xDF && len(s) >= 2:
		return cRuneWidth(ranges, rune(b&0x1F)<<6|rune(s[1]&0x3F))
	case b <= 0xEF && len(s) >= 3:
		return cRuneWidth(ranges, rune(b&0x0F)<<12|rune(s[1]&0x3F)<<6|rune(s[2]&0x3F))
	case b <= 0xF4 && len(s) >= 4:
		return cRuneWidth(ranges, rune(b&0x07)<<18|rune(s[1]&0x3F)<<12|
			rune(s[2]&0x3F)<<6|rune(s[3]&0x3F))
	}
	return 1
}

// widthDiverges reports whether any character of s is one that the C table
// and go-runewidth measure differently.
func widthDiverges(ranges []widthRange, s []byte) bool {
	for pos := 0; pos < len(s); {
		ofs, w := nextOfs(s, pos)
		if ofs <= 0 {
			return false
		}
		if cCharWidth(ranges, s[pos:pos+ofs]) != w {
			return true
		}
		pos += ofs
	}
	return false
}

// checkStringbufLine checks one recorded call. It returns the kind of call, a
// difference that is always a failure, and a difference that a disputed
// character width can explain.
func checkStringbufLine(t *testing.T, line string) (kind, hard, soft string) {
	t.Helper()
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", "empty line", ""
	}
	// Every kind but class names its string in field 1.
	//
	// mustHex gives nil for an empty field. That is right for the kinds that
	// build a buffer, because an empty stringbuf holds a null pointer in C.
	// It is wrong for the kinds that call a function on the string itself,
	// because the corpus passes a string literal, which is never null even
	// when it is empty. Those kinds get a slice that is empty and not nil.
	var s []byte
	if f[0] != "class" && len(f) > 1 {
		s = mustHex(t, f[1])
		switch f[0] {
		case "ins", "del", "swap", "delbefore", "delat", "split",
			"bnext", "bprev", "charat", "fromutf8":
		default:
			if s == nil {
				s = []byte{}
			}
		}
	}
	switch f[0] {
	case "width":
		if got, want := strWidth(s), mustInt(t, f[2]); got != want {
			return f[0], "", fmt.Sprintf("strWidth = %d, want %d", got, want)
		}
	case "fit":
		maxw, wantSkip, wantTake := mustInt(t, f[2]), mustInt(t, f[3]), mustInt(t, f[4])
		if got := skipUntilFit(s, maxw); got != wantSkip {
			return f[0], "", fmt.Sprintf("skipUntilFit(%d) = %d, want %d", maxw, got, wantSkip)
		}
		if got := takeWhileFit(s, maxw); got != wantTake {
			return f[0], "", fmt.Sprintf("takeWhileFit(%d) = %d, want %d", maxw, got, wantTake)
		}
	case "next", "prev":
		pos, wantOfs, wantWidth := mustInt(t, f[2]), mustInt(t, f[3]), mustInt(t, f[4])
		gotOfs, gotWidth := nextOfs(s, pos)
		if f[0] == "prev" {
			gotOfs, gotWidth = prevOfs(s, pos)
		}
		if gotOfs != wantOfs {
			return f[0], fmt.Sprintf("%sOfs(%d) offset = %d, want %d", f[0], pos, gotOfs, wantOfs), ""
		}
		if gotWidth != wantWidth {
			return f[0], "", fmt.Sprintf("%sOfs(%d) width = %d, want %d", f[0], pos, gotWidth, wantWidth)
		}
	case "esc":
		pos, wantOK, wantLen := mustInt(t, f[2]), mustInt(t, f[3]) == 1, mustInt(t, f[4])
		// The C code is handed a negative length when pos runs past the end,
		// and answers no. An empty slice does the same here.
		gotLen, gotOK := skipEsc(s[min(pos, len(s)):])
		if gotOK != wantOK || gotLen != wantLen {
			return f[0], fmt.Sprintf("skipEsc at %d = (%d, %v), want (%d, %v)",
				pos, gotLen, gotOK, wantLen, wantOK), ""
		}
	case "find":
		pos := mustInt(t, f[2])
		for i, c := range []struct {
			name string
			fn   func([]byte, int) int
		}{
			{"findLineStart", findLineStart}, {"findLineEnd", findLineEnd},
			{"findWordStart", findWordStart}, {"findWordEnd", findWordEnd},
			{"findWSWordStart", findWSWordStart}, {"findWSWordEnd", findWSWordEnd},
		} {
			if got, want := c.fn(s, pos), mustInt(t, f[3+i]); got != want {
				return f[0], fmt.Sprintf("%s(%d) = %d, want %d", c.name, pos, got, want), ""
			}
		}
	case "rc":
		termw, promptw, cpromptw := mustInt(t, f[2]), mustInt(t, f[3]), mustInt(t, f[4])
		pos, wantRows := mustInt(t, f[5]), mustInt(t, f[6])
		gotRows, got := rowColAtPos(s, termw, promptw, cpromptw, pos)
		return f[0], "", diffRowCol(t, fmt.Sprintf("rowColAtPos(%d,%d,%d,%d)", termw, promptw, cpromptw, pos),
			gotRows, got, wantRows, f[7:])
	case "posrc":
		termw, promptw, cpromptw := mustInt(t, f[2]), mustInt(t, f[3]), mustInt(t, f[4])
		row, col, want := mustInt(t, f[5]), mustInt(t, f[6]), mustInt(t, f[7])
		if got := posAtRowCol(s, termw, promptw, cpromptw, row, col); got != want {
			return f[0], "", fmt.Sprintf("posAtRowCol(%d,%d,%d,%d,%d) = %d, want %d",
				termw, promptw, cpromptw, row, col, got, want)
		}
	case "wrc":
		termw, newtermw := mustInt(t, f[2]), mustInt(t, f[3])
		promptw, cpromptw := mustInt(t, f[4]), mustInt(t, f[5])
		pos, wantRows := mustInt(t, f[6]), mustInt(t, f[7])
		gotRows, got := wrappedRowColAtPos(s, termw, newtermw, promptw, cpromptw, pos)
		return f[0], "", diffRowCol(t, fmt.Sprintf("wrappedRowColAtPos(%d,%d,%d,%d,%d)",
			termw, newtermw, promptw, cpromptw, pos), gotRows, got, wantRows, f[8:])
	case "ins":
		ins, pos := mustStr(t, f[2]), mustInt(t, f[3])
		wantPos, wantBuf := mustInt(t, f[4]), f[5]
		b := &buffer{buf: append([]byte(nil), s...)}
		if got := b.insertAt(ins, pos); got != wantPos {
			return f[0], fmt.Sprintf("insertAt(%q, %d) = %d, want %d", ins, pos, got, wantPos), ""
		}
		return f[0], diffBuf(b, wantBuf, fmt.Sprintf("insertAt(%q, %d)", ins, pos)), ""
	case "del":
		pos, count, wantBuf := mustInt(t, f[2]), mustInt(t, f[3]), f[4]
		b := &buffer{buf: append([]byte(nil), s...)}
		b.deleteAt(pos, count)
		return f[0], diffBuf(b, wantBuf, fmt.Sprintf("deleteAt(%d, %d)", pos, count)), ""
	case "swap", "delbefore":
		pos, wantPos, wantBuf := mustInt(t, f[2]), mustInt(t, f[3]), f[4]
		b := &buffer{buf: append([]byte(nil), s...)}
		var got int
		if f[0] == "delbefore" {
			got = b.deleteCharBefore(pos)
		} else {
			got = b.swapChar(pos)
		}
		if got != wantPos {
			return f[0], fmt.Sprintf("%s(%d) = %d, want %d", f[0], pos, got, wantPos), ""
		}
		return f[0], diffBuf(b, wantBuf, fmt.Sprintf("%s(%d)", f[0], pos)), ""
	case "delat":
		pos, wantBuf := mustInt(t, f[2]), f[3]
		b := &buffer{buf: append([]byte(nil), s...)}
		b.deleteCharAt(pos)
		return f[0], diffBuf(b, wantBuf, fmt.Sprintf("deleteCharAt(%d)", pos)), ""
	case "split":
		pos, wantLeft, wantRight := mustInt(t, f[2]), f[3], f[4]
		b := &buffer{buf: append([]byte(nil), s...)}
		rest := b.splitAt(pos)
		if d := diffBuf(b, wantLeft, fmt.Sprintf("splitAt(%d) left", pos)); d != "" {
			return f[0], d, ""
		}
		if rest == nil {
			if wantRight != "!" {
				return f[0], fmt.Sprintf("splitAt(%d) right = nil, want %s", pos, wantRight), ""
			}
			return f[0], "", ""
		}
		return f[0], diffBuf(rest, wantRight, fmt.Sprintf("splitAt(%d) right", pos)), ""
	case "bnext", "bprev":
		pos, wantPos, wantWidth := mustInt(t, f[2]), mustInt(t, f[3]), mustInt(t, f[4])
		b := &buffer{buf: s}
		gotPos, gotWidth := b.next(pos)
		if f[0] == "bprev" {
			gotPos, gotWidth = b.prev(pos)
		}
		if gotPos != wantPos {
			return f[0], fmt.Sprintf("%s(%d) = %d, want %d", f[0], pos, gotPos, wantPos), ""
		}
		// The C code leaves the width at zero when it returns -1, because it
		// never calls the width function on that path.
		if wantPos >= 0 && gotWidth != wantWidth {
			return f[0], "", fmt.Sprintf("%s(%d) width = %d, want %d", f[0], pos, gotWidth, wantWidth)
		}
	case "charat":
		pos := mustInt(t, f[2])
		b := &buffer{buf: s}
		if got, want := b.charAt(pos), mustHex(t, f[3])[0]; got != want {
			return f[0], fmt.Sprintf("charAt(%d) = %02x, want %02x", pos, got, want), ""
		}
	case "fromutf8":
		b := &buffer{buf: s}
		got := b.decodeFromLocale()
		want := f[2]
		if got == nil {
			if want != "!" {
				return f[0], fmt.Sprintf("decodeFromLocale = nil, want %s", want), ""
			}
			return f[0], "", ""
		}
		if h := hexOrDash(got); h != want {
			return f[0], fmt.Sprintf("decodeFromLocale = %s, want %s", h, want), ""
		}
	case "overlap":
		a, p, want := mustStr(t, f[1]), mustStr(t, f[2]), mustInt(t, f[3])
		if got := countEndOverlap(a, p); got != want {
			return f[0], fmt.Sprintf("countEndOverlap(%q, %q) = %d, want %d", a, p, got, want), ""
		}
	case "istoken":
		pos, class, want := mustInt(t, f[2]), mustInt(t, f[3]), mustInt(t, f[4])
		if got := isToken(s, pos, tokenClasses[class]); got != want {
			return f[0], fmt.Sprintf("isToken(%d, class %d) = %d, want %d", pos, class, got, want), ""
		}
	case "mtoken":
		token, pos := mustStr(t, f[2]), mustInt(t, f[3])
		class, want := mustInt(t, f[4]), mustInt(t, f[5])
		if got := matchToken(s, pos, tokenClasses[class], token); got != want {
			return f[0], fmt.Sprintf("matchToken(%d, class %d, %q) = %d, want %d",
				pos, class, token, got, want), ""
		}
	case "manytoken":
		pos, class, want := mustInt(t, f[2]), mustInt(t, f[3]), mustInt(t, f[4])
		if got := matchAnyToken(s, pos, tokenClasses[class], probeTokens); got != want {
			return f[0], fmt.Sprintf("matchAnyToken(%d, class %d) = %d, want %d",
				pos, class, got, want), ""
		}
	case "prevchar", "nextchar":
		pos, want := mustInt(t, f[2]), mustInt(t, f[3])
		got := prevChar(s, pos)
		if f[0] == "nextchar" {
			got = nextChar(s, pos)
		}
		if got != want {
			return f[0], fmt.Sprintf("%s(%d) = %d, want %d", f[0], pos, got, want), ""
		}
	case "class":
		return f[0], checkClassLine(t, f), ""
	case "atoz":
		in, wantOK, wantVal := mustStr(t, f[1]), mustInt(t, f[2]) == 1, mustInt(t, f[3])
		got, ok := atoz(in)
		if ok != wantOK || (ok && got != wantVal) {
			return f[0], fmt.Sprintf("atoz(%q) = (%d, %v), want (%d, %v)", in, got, ok, wantVal, wantOK), ""
		}
	case "atoz2":
		in, wantOK := mustStr(t, f[1]), mustInt(t, f[2]) == 1
		wantA, wantB := mustInt(t, f[3]), mustInt(t, f[4])
		a, b, n := atoz2(in)
		if (n == 2) != wantOK {
			return f[0], fmt.Sprintf("atoz2(%q) read %d numbers, want ok %v", in, n, wantOK), ""
		}
		if n >= 1 && a != wantA {
			return f[0], fmt.Sprintf("atoz2(%q) first = %d, want %d", in, a, wantA), ""
		}
		if n == 2 && b != wantB {
			return f[0], fmt.Sprintf("atoz2(%q) second = %d, want %d", in, b, wantB), ""
		}
	case "atou32":
		in, wantOK := mustStr(t, f[1]), mustInt(t, f[2]) == 1
		got, ok := atou32(in)
		if ok != wantOK {
			return f[0], fmt.Sprintf("atou32(%q) ok = %v, want %v", in, ok, wantOK), ""
		}
		if ok && fmt.Sprint(got) != f[3] {
			return f[0], fmt.Sprintf("atou32(%q) = %d, want %s", in, got, f[3]), ""
		}
	default:
		return f[0], "unknown kind of call", ""
	}
	return f[0], "", ""
}

// probeTokens is the token list that the probe passes to ic_match_any_token.
var probeTokens = []string{"fun", "function", "foo", "a", "12", "日"}

// checkClassLine checks one recorded call to the character class functions.
func checkClassLine(t *testing.T, f []string) string {
	t.Helper()
	c, n := mustHex(t, f[1])[0], mustInt(t, f[2])
	s := []byte{c, 'x'}[:n]
	for i, class := range []struct {
		name string
		fn   charClass
	}{
		{"charIsWhite", charIsWhite}, {"charIsNonWhite", charIsNonWhite},
		{"charIsSeparator", charIsSeparator}, {"charIsNonSeparator", charIsNonSeparator},
		{"charIsDigit", charIsDigit}, {"charIsHexDigit", charIsHexDigit},
		{"charIsLetter", charIsLetter}, {"charIsIDLetter", charIsIDLetter},
		{"charIsFileNameLetter", charIsFileNameLetter},
	} {
		if got, want := class.fn(s), mustInt(t, f[3+i]) == 1; got != want {
			return fmt.Sprintf("%s(%02x, len %d) = %v, want %v", class.name, c, n, got, want)
		}
	}
	return ""
}

// diffRowCol compares a row and column result against the recorded fields.
func diffRowCol(t *testing.T, call string, gotRows int, got rowCol, wantRows int, f []string) string {
	t.Helper()
	want := rowCol{
		row:        mustInt(t, f[0]),
		col:        mustInt(t, f[1]),
		rowStart:   mustInt(t, f[2]),
		rowLen:     mustInt(t, f[3]),
		firstOnRow: mustInt(t, f[4]) == 1,
		lastOnRow:  mustInt(t, f[5]) == 1,
	}
	if gotRows != wantRows || got != want {
		return fmt.Sprintf("%s = %d rows %+v, want %d rows %+v", call, gotRows, got, wantRows, want)
	}
	return ""
}

// diffBuf compares a buffer against a recorded hex field.
func diffBuf(b *buffer, want, call string) string {
	if got := hexOrDash(b.bytes()); got != want {
		return fmt.Sprintf("%s left the buffer %s, want %s", call, got, want)
	}
	return ""
}

// hexOrDash renders bytes the way the probe does, with "-" for empty.
func hexOrDash(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	return hex.EncodeToString(b)
}

// regenerateStringbuf runs the C probe and writes the corpus.
func regenerateStringbuf(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(sbufProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-stringbuf.sh", sbufProbePath)
	}
	out, err := exec.Command(sbufProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(sbufCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", sbufCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", sbufCorpusPath, len(out))
}
