package rline

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/xo/rline/key"
)

const (
	// winKeyCorpusPath holds the sequences the C code pushes for a Windows
	// key event, and the keys they decode back to.
	winKeyCorpusPath = "testdata/wintty.txt"

	// winKeyProbePath is where tools/build-probe-wintty.sh puts the probe.
	winKeyProbePath = ".build/probe-wintty"
)

// TestWinKeyPort replays the Windows key encoding and checks that the Go port
// pushes the same bytes and reads back the same keys.
//
// This runs on every system, not only on Windows. The functions being checked
// sit above the part of tty.c that is compiled per system, so the C side
// could be recorded here, and the Go side has no build tag either. Only
// fetching a key event from the console needs Windows, and that part is in
// ttydev_windows.go where no corpus can reach it.
func TestWinKeyPort(t *testing.T) {
	if *update {
		regenerateWinKey(t)
	}
	b, err := os.ReadFile(winKeyCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-wintty.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) < 300 {
		t.Fatalf("the corpus holds %d lines, which is too few to prove anything", len(lines))
	}
	counts := make(map[string]int, 8)
	bad := 0
	for i, line := range lines {
		kind, diff := checkWinKeyLine(t, line)
		counts[kind]++
		if diff == "" {
			continue
		}
		bad++
		if bad <= maxReported {
			t.Errorf("%s:%d: %s\n  %s", winKeyCorpusPath, i+1, diff, line)
		}
	}
	if bad > maxReported {
		t.Errorf("%d differences in total, %d shown", bad, maxReported)
	}
	for _, kind := range []string{"csimods", "pushvt", "pushxterm", "pushuni"} {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
		}
	}
	t.Logf("checked %d cases: %v", len(lines), counts)
}

// checkWinKeyLine checks one recorded case.
func checkWinKeyLine(t *testing.T, line string) (string, string) {
	t.Helper()
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", "empty line"
	}
	if f[0] == "csimods" {
		mods, want := mustCode(t, f[1]), mustInt(t, f[2])
		if got := csiMods(mods); got != uint32(want) {
			return f[0], fmt.Sprintf("csiMods(%08x) = %d, want %d", mods, got, want)
		}
		return f[0], ""
	}

	mods := mustCode(t, f[1])
	term := newTTY(&idleReader{})
	term.setEscDelay(0, 0)
	var what string
	switch f[0] {
	case "pushvt":
		vtcode := mustInt(t, f[2])
		what = fmt.Sprintf("pushCSIVT(%08x, %d)", mods, vtcode)
		term.pushCSIVT(mods, uint32(vtcode))
	case "pushxterm":
		xcode := mustHex(t, f[2])[0]
		what = fmt.Sprintf("pushCSIXterm(%08x, %q)", mods, xcode)
		term.pushCSIXterm(mods, xcode)
	case "pushuni":
		code, err := strconv.ParseUint(f[2], 16, 32)
		if err != nil {
			t.Fatalf("reading a code point %q: %v", f[2], err)
		}
		what = fmt.Sprintf("pushCSIUnicode(%08x, %x)", mods, code)
		term.pushCSIUnicode(mods, uint32(code))
	default:
		return f[0], "unknown kind of case"
	}

	// The bytes, in the order the decoder will read them.
	pushed := make([]byte, 0, len(term.pushedBytes))
	for i := len(term.pushedBytes) - 1; i >= 0; i-- {
		pushed = append(pushed, term.pushedBytes[i])
	}
	if got, want := hexOrDash(pushed), f[3]; got != want {
		return f[0], fmt.Sprintf("%s pushed %s, want %s", what, got, want)
	}

	// And what those bytes decode back to, which is what the edit loop sees.
	var got []key.Code
	for range 8 {
		code, ok := term.readTimeout(0)
		if !ok {
			break
		}
		got = append(got, code)
	}
	want := make([]key.Code, 0, len(f)-4)
	for _, field := range f[4:] {
		want = append(want, mustCode(t, field))
	}
	if len(got) != len(want) {
		return f[0], fmt.Sprintf("%s decoded to %s, want %s", what, showCodes(got), showCodes(want))
	}
	for i := range got {
		if got[i] != want[i] {
			return f[0], fmt.Sprintf("%s decoded to %s, want %s", what, showCodes(got), showCodes(want))
		}
	}
	return f[0], ""
}

// regenerateWinKey runs the C probe and writes the corpus.
func regenerateWinKey(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(winKeyProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-wintty.sh", winKeyProbePath)
	}
	out, err := exec.Command(winKeyProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(winKeyCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", winKeyCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", winKeyCorpusPath, len(out))
}
