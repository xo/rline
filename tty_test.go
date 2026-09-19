package rline

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/rline/key"
)

const (
	// ttyCorpusPath holds the key codes that the C code decoded.
	ttyCorpusPath = "testdata/tty.txt"

	// ttyProbePath is where tools/build-probe-tty.sh puts the probe.
	ttyProbePath = ".build/probe-tty"
)

// idleReader is a byteReader over a fixed slice of bytes. Once the bytes run
// out it always reports that nothing arrived, which is what a terminal with
// no one typing at it looks like to the decoder.
//
// The probe does the same on the C side, with an empty pipe. It must not use
// a source that is always at end of file, because the C code clears the byte
// it was asked for before it reads, so a read that happens and fails answers
// with a zero where a terminal that stayed quiet leaves the byte alone.
type idleReader struct {
	bytes []byte
	pos   int
}

func (r *idleReader) readByte(_ time.Duration) (byte, bool) {
	if r.pos >= len(r.bytes) {
		return 0, false
	}
	b := r.bytes[r.pos]
	r.pos++
	return b, true
}

// TestTTYPort replays every recorded decode and checks that the Go port
// answers the same.
func TestTTYPort(t *testing.T) {
	if *update {
		regenerateTTY(t)
	}
	b, err := os.ReadFile(ttyCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-tty.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) < 5000 {
		t.Fatalf("the corpus holds %d lines, which is too few to prove anything", len(lines))
	}
	counts := make(map[string]int, 8)
	bad := 0
	for i, line := range lines {
		kind, diff := checkTTYLine(t, line)
		counts[kind]++
		if diff == "" {
			continue
		}
		bad++
		if bad <= maxReported {
			t.Errorf("%s:%d: %s\n  %s", ttyCorpusPath, i+1, diff, line)
		}
	}
	if bad > maxReported {
		t.Errorf("%d differences in total, %d shown", bad, maxReported)
	}
	for _, kind := range []string{"vt", "xterm", "ss3", "code", "keys"} {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
		}
	}
	t.Logf("checked %d decodes: %v", len(lines), counts)
}

// checkTTYLine checks one recorded case. It returns the kind of case, and a
// description of the difference, or an empty string when the port agrees.
func checkTTYLine(t *testing.T, line string) (string, string) {
	t.Helper()
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", "empty line"
	}
	switch f[0] {
	case "vt":
		num, want := mustInt(t, f[1]), mustCode(t, f[2])
		if got := decodeVT(uint32(num)); got != want {
			return f[0], fmt.Sprintf("decodeVT(%d) = %08x, want %08x", num, got, want)
		}
	case "xterm", "ss3":
		final, want := mustHex(t, f[1])[0], mustCode(t, f[2])
		got := decodeXterm(final)
		if f[0] == "ss3" {
			got = decodeSS3(final)
		}
		if got != want {
			return f[0], fmt.Sprintf("decode%s(%02x) = %08x, want %08x", f[0], final, got, want)
		}
	case "code":
		code := mustCode(t, f[1])
		wantASCII, wantChar := mustInt(t, f[2]) == 1, mustHex(t, f[3])[0]
		wantUni, wantRune := mustInt(t, f[4]) == 1, mustCode(t, f[5])
		wantVirt, wantMod := mustInt(t, f[6]) == 1, mustCode(t, f[7])
		if gotChar, gotOK := code.ASCIIChar(); gotOK != wantASCII || gotChar != wantChar {
			return f[0], fmt.Sprintf("ASCIIChar(%08x) = (%02x, %v), want (%02x, %v)",
				code, gotChar, gotOK, wantChar, wantASCII)
		}
		if gotRune, gotOK := code.Unicode(); gotOK != wantUni || key.Code(gotRune) != wantRune {
			return f[0], fmt.Sprintf("Unicode(%08x) = (%08x, %v), want (%08x, %v)",
				code, gotRune, gotOK, wantRune, wantUni)
		}
		if got := code.IsVirtKey(); got != wantVirt {
			return f[0], fmt.Sprintf("IsVirtKey(%08x) = %v, want %v", code, got, wantVirt)
		}
		if got := modifyCode(code); got != wantMod {
			return f[0], fmt.Sprintf("modifyCode(%08x) = %08x, want %08x", code, got, wantMod)
		}
	case "keys":
		in, utf8 := mustHex(t, f[1]), mustInt(t, f[2]) == 1
		want := make([]key.Code, 0, len(f)-3)
		for _, field := range f[3:] {
			want = append(want, mustCode(t, field))
		}
		got := decodeKeys(in, utf8)
		if len(got) != len(want) {
			return f[0], fmt.Sprintf("decoded %d keys %s, want %d keys %s",
				len(got), showCodes(got), len(want), showCodes(want))
		}
		for i := range got {
			if got[i] != want[i] {
				return f[0], fmt.Sprintf("key %d is %08x, want %08x (got %s, want %s)",
					i, got[i], want[i], showCodes(got), showCodes(want))
			}
		}
	default:
		return f[0], "unknown kind of case"
	}
	return f[0], ""
}

// decodeKeys reads every key that in decodes to. The probe stops after
// sixteen, so this does too.
func decodeKeys(in []byte, utf8 bool) []key.Code {
	term := newTTY(&idleReader{bytes: in})
	term.isUTF8 = utf8
	// The probe sets both waits to zero, because its input is already there
	// and nothing ever has to be waited for.
	term.setEscDelay(0, 0)
	out := make([]key.Code, 0, 4)
	for range 16 {
		code, ok := term.readTimeout(0)
		if !ok {
			break
		}
		out = append(out, code)
	}
	return out
}

// showCodes renders a list of key codes for a failure message.
func showCodes(codes []key.Code) string {
	if len(codes) == 0 {
		return "(none)"
	}
	parts := make([]string, len(codes))
	for i, c := range codes {
		parts[i] = fmt.Sprintf("%08x", uint32(c))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// mustCode reads a key code field, which the probe prints as eight hex
// digits.
func mustCode(t *testing.T, s string) key.Code {
	t.Helper()
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		t.Fatalf("reading key code %q: %v", s, err)
	}
	return key.Code(v)
}

// regenerateTTY runs the C probe and writes the corpus.
func regenerateTTY(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(ttyProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-tty.sh", ttyProbePath)
	}
	out, err := exec.Command(ttyProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(ttyCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", ttyCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", ttyCorpusPath, len(out))
}
