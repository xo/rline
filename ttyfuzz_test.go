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

	"github.com/xo/rline/key"
)

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
