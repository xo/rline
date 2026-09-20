package rline

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/rline/key"
)

// --------------------------------------------------------------------------
// tty_test.go

// --------------------------------------------------------------------------
// tty_test.go

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
	// A floor would let the corpus shrink. These tests are driven by the
	// corpus rather than replaying a script against it, so a line that goes
	// missing is one case fewer checked and nothing else: measured by
	// deleting a line from the middle and watching the suite stay green.
	// The exact count is the smallest thing that notices, and changing it is
	// a deliberate edit beside the corpus it describes.
	if len(lines) != 7175 {
		t.Fatalf("testdata/tty.txt holds %d lines, want %d: a corpus that changed size "+
			"was either regenerated on purpose, in which case set this number, "+
			"or lost lines, in which case it now checks less than it says",
			len(lines), 7175)
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
	for _, kind := range []string{"vt", "xterm", "ss3", "code", "keys", "escresp"} {
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
	case "escresp":
		in, start := mustHex(t, f[1]), mustHex(t, f[2])[0]
		finalST, max := mustInt(t, f[3]) == 1, mustInt(t, f[4])
		wantOK, wantBuf, wantRest := mustInt(t, f[5]) == 1, f[6], f[7]
		term := newTTY(&idleReader{bytes: in})
		term.setEscDelay(0, 0)
		got, ok := term.readEscResponse(start, finalST, max)
		if ok != wantOK {
			return f[0], fmt.Sprintf("readEscResponse reported %v, want %v", ok, wantOK)
		}
		// The C code fills its buffer as it goes and only terminates it when
		// it succeeds, so what is in there after a failure is not a string at
		// all and no caller may read it. The port returns nothing instead, so
		// the answer is only compared when the call succeeded.
		if ok {
			if h := hexOrDash([]byte(got)); h != wantBuf {
				return f[0], fmt.Sprintf("readEscResponse gave %s, want %s", h, wantBuf)
			}
		} else if got != "" {
			return f[0], fmt.Sprintf("readEscResponse failed but gave %q, want nothing", got)
		}
		// Whatever is left unread, which records the bytes put back.
		var rest []byte
		for range ttyPushMax {
			b, more := term.readByte(0)
			if !more {
				break
			}
			rest = append(rest, b)
		}
		if h := hexOrDash(rest); h != wantRest {
			return f[0], fmt.Sprintf("readEscResponse left %s unread, want %s", h, wantRest)
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

// --------------------------------------------------------------------------
// ttyread_test.go

// TestTTYPushedCodeComesBackUnchanged checks that a key pushed back is
// returned exactly as it was given.
//
// A key read from the terminal goes through modifyCode, which rewrites
// several of them. A key pushed back has been through that already, so
// running it through a second time would change it again. key.Rubout shows
// the difference, because modifyCode turns it into key.Backspace.
func TestTTYPushedCodeComesBackUnchanged(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{})
	term.pushCode(key.Rubout)
	got, ok := term.readTimeout(0)
	if !ok {
		t.Fatal("reading a pushed back key found nothing")
	}
	if got != key.Rubout {
		t.Errorf("a pushed back key came back as %08x, want %08x", got, key.Rubout)
	}
	if _, ok := term.readTimeout(0); ok {
		t.Error("a second read found a key where there should be none")
	}
}

// TestTTYPushedCodesAreReadInReverse records that the code buffer is a stack,
// so the key pushed last is read first.
func TestTTYPushedCodesAreReadInReverse(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{})
	term.pushCode(key.Up)
	term.pushCode(key.Down)
	for i, want := range []key.Code{key.Down, key.Up} {
		got, ok := term.readTimeout(0)
		if !ok {
			t.Fatalf("read %d found nothing", i)
		}
		if got != want {
			t.Errorf("read %d gave %08x, want %08x", i, got, want)
		}
	}
}

// TestTTYPushbackStopsAtTheLimit checks that neither buffer grows without
// bound. Anything past the limit is dropped, as in the C code.
func TestTTYPushbackStopsAtTheLimit(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{})
	for range ttyPushMax + 10 {
		term.pushByte('x')
		term.pushCode(key.Up)
	}
	if got := len(term.pushedBytes); got != ttyPushMax {
		t.Errorf("the byte buffer holds %d, want %d", got, ttyPushMax)
	}
	if got := len(term.pushedCodes); got != ttyPushMax {
		t.Errorf("the code buffer holds %d, want %d", got, ttyPushMax)
	}
}

// TestTTYReadEndsAtKeyNone checks the blocking read, which returns key.None
// once the input is finished.
func TestTTYReadEndsAtKeyNone(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{bytes: []byte("hi")})
	for _, want := range []key.Code{'h', 'i', key.None} {
		if got := term.read(); got != want {
			t.Errorf("read gave %08x, want %08x", got, want)
		}
	}
}

// TestTTYSetEscDelay checks that the waits can be changed, which the timing
// tests will need once a clock can be injected.
func TestTTYSetEscDelay(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{})
	if term.escInitialTimeout != defaultEscInitialTimeout || term.escTimeout != defaultEscTimeout {
		t.Fatalf("a new tty waits %v and %v, want %v and %v",
			term.escInitialTimeout, term.escTimeout,
			defaultEscInitialTimeout, defaultEscTimeout)
	}
	term.setEscDelay(5*time.Millisecond, time.Millisecond)
	if term.escInitialTimeout != 5*time.Millisecond || term.escTimeout != time.Millisecond {
		t.Errorf("after setEscDelay the tty waits %v and %v, want 5ms and 1ms",
			term.escInitialTimeout, term.escTimeout)
	}
}

// TestTTYDecodesAcrossReads checks that a sequence split over several reads
// still decodes as one key. The terminal delivers bytes as they arrive, so
// the decoder cannot assume a whole sequence is ready at once.
func TestTTYDecodesAcrossReads(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{bytes: []byte("\x1b[1;5A")})
	term.setEscDelay(0, 0)
	got, ok := term.readTimeout(0)
	if !ok {
		t.Fatal("no key decoded")
	}
	if want := key.Up | key.ModCtrl; got != want {
		t.Errorf("decoded %08x, want %08x", got, want)
	}
}

// --------------------------------------------------------------------------
// winkey_test.go

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

// --------------------------------------------------------------------------
// ttyfuzz_test.go

// Differential fuzzing of the escape decoder.
//
// The recorded corpus only holds sequences somebody thought to write down.
// This generates them instead, and asks the C decoder and the Go one the same
// question. A difference is a failure: unlike the width tables, where the two
// disagree on purpose, there is no reason for these two to answer differently.
//
// The C side runs as a server rather than once per input, because starting a
// process for every input would make this too slow to be worth running.

const (
	// fuzzProbePath is where tools/build-probe-ttyfuzz.sh puts the server.
	fuzzProbePath = ".build/probe-ttyfuzz"

	// fuzzMaxCase is the longest input the C side accepts. Anything longer is
	// left alone rather than half compared.
	fuzzMaxCase = 4096

	// fuzzMaxKeys is the most keys read from one input, which both sides cap
	// at the same number.
	fuzzMaxKeys = 4096
)

// cDecoder drives the C escape decoder in another process.
//
// Commands go out on the process's file descriptor 3 rather than its standard
// input, because the decoder asks FIONREAD about descriptor 0 to decide
// whether terminal input is waiting, and commands sitting there would make it
// believe there was.
type cDecoder struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	send *os.File
	recv *bufio.Reader
}

// startCDecoder starts the C decoder. It reports false when the probe has not
// been built, which is not a failure: the seed corpus still runs on the Go
// side alone in that case.
func startCDecoder(tb testing.TB) (*cDecoder, bool) {
	tb.Helper()
	if _, err := os.Stat(fuzzProbePath); err != nil {
		return nil, false
	}
	cmdRead, cmdWrite, err := os.Pipe()
	if err != nil {
		tb.Fatalf("making the command pipe: %v", err)
	}
	cmd := exec.Command(fuzzProbePath)
	// ExtraFiles starts at descriptor 3, which is where the probe looks.
	cmd.ExtraFiles = []*os.File{cmdRead}
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		tb.Fatalf("taking the output: %v", err)
	}
	if err := cmd.Start(); err != nil {
		tb.Fatalf("starting %s: %v", fuzzProbePath, err)
	}
	_ = cmdRead.Close()
	d := &cDecoder{cmd: cmd, send: cmdWrite, recv: bufio.NewReader(out)}
	tb.Cleanup(func() {
		_ = d.send.Close()
		_ = d.cmd.Wait()
	})
	return d, true
}

// decode asks the C decoder what a stream of bytes turns into.
func (d *cDecoder) decode(tb testing.TB, utf8 bool, in []byte) []key.Code {
	tb.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()

	flag := '0'
	if utf8 {
		flag = '1'
	}
	body := "-"
	if len(in) > 0 {
		body = hex.EncodeToString(in)
	}
	if _, err := fmt.Fprintf(d.send, "%c %s\n", flag, body); err != nil {
		tb.Fatalf("sending a case to the C decoder: %v", err)
	}
	line, err := d.recv.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			tb.Fatalf("the C decoder stopped; it may have crashed on %x", in)
		}
		tb.Fatalf("reading the answer: %v", err)
	}
	line = strings.TrimRight(line, "\n")
	if line == "!write-failed" {
		tb.Fatalf("the C decoder could not take a case of %d bytes", len(in))
	}
	if line == "" {
		return nil
	}
	fields := strings.Fields(line)
	out := make([]key.Code, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.ParseUint(f, 16, 32)
		if err != nil {
			tb.Fatalf("reading a key code %q: %v", f, err)
		}
		out = append(out, key.Code(v))
	}
	return out
}

// goDecode reads every key that in turns into, with the same cap the C side
// uses.
func goDecode(utf8 bool, in []byte) []key.Code {
	term := newTTY(&idleReader{bytes: in})
	term.isUTF8 = utf8
	// Both waits are zero, so nothing is waited for and the answer depends
	// only on the bytes.
	term.setEscDelay(0, 0)
	var out []key.Code
	for range fuzzMaxKeys {
		code, ok := term.readTimeout(0)
		if !ok {
			break
		}
		out = append(out, code)
	}
	return out
}

// FuzzEscapeDecoder compares the Go escape decoder against the C one on
// generated input.
//
// Without -fuzz this runs the seed corpus, which is every sequence already in
// testdata/tty.txt. With -fuzz it generates more.
func FuzzEscapeDecoder(f *testing.F) {
	for _, seed := range escapeSeeds(f) {
		f.Add(seed.utf8, seed.bytes)
	}
	d, ok := startCDecoder(f)
	if !ok {
		f.Skip("no C decoder at " + fuzzProbePath + ": run tools/build-probe-ttyfuzz.sh")
	}
	f.Fuzz(func(t *testing.T, utf8 bool, in []byte) {
		if len(in) > fuzzMaxCase {
			// The C side takes no more than this, so there is nothing to
			// compare rather than something to disagree about.
			t.Skip()
		}
		want := d.decode(t, utf8, in)
		got := goDecode(utf8, in)
		if len(got) != len(want) {
			t.Fatalf("input %x utf8=%v decoded to %d keys, want %d\n got  %s\n want %s",
				in, utf8, len(got), len(want), showCodes(got), showCodes(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("input %x utf8=%v: key %d is %08x, want %08x\n got  %s\n want %s",
					in, utf8, i, got[i], want[i], showCodes(got), showCodes(want))
			}
		}
	})
}

// escapeSeed is one starting point for the fuzzer.
type escapeSeed struct {
	utf8  bool
	bytes []byte
}

// escapeSeeds returns every sequence the recorded corpus holds, plus a few
// shapes worth starting from that it does not.
//
// Starting from the corpus rather than from nothing means the fuzzer begins
// with input that already reaches the interesting parts of the decoder, and
// spends its time on what nobody thought to write down.
func escapeSeeds(tb testing.TB) []escapeSeed {
	tb.Helper()
	seeds := []escapeSeed{
		{true, nil},
		{false, nil},
		{true, []byte("\x1b")},
		{true, []byte("\x1b\x1b")},
		{true, []byte("\x1b[")},
		{true, []byte("\x1bO")},
		{true, []byte("\x1b]")},
		{true, []byte("\x1b[999999999999999999999;1A")},
		{true, []byte("\x1b[;;;;;;;;A")},
		{true, []byte("\x1b[1;2;3;4;5A")},
		{true, []byte("\x1b[\x00A")},
		{true, []byte{0x1b, '[', 0, 0, 0}},
		{true, []byte("\xff\xfe\xfd")},
		{true, []byte("\xf4\x8f\xbf\xbf")},
		{true, []byte("\x1b[1;5A\x1b[1;5B\x1b[1;5C")},
	}
	b, err := os.ReadFile(ttyCorpusPath)
	if err != nil {
		tb.Fatalf("reading the corpus for seeds: %v", err)
	}
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "keys" {
			continue
		}
		in := mustHexTB(tb, fields[1])
		seeds = append(seeds, escapeSeed{utf8: fields[2] == "1", bytes: in})
	}
	return seeds
}

// mustHexTB reads a hex field. The corpus writes "-" for an empty one.
func mustHexTB(tb testing.TB, s string) []byte {
	tb.Helper()
	if s == "-" {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		tb.Fatalf("reading hex %q: %v", s, err)
	}
	return b
}
