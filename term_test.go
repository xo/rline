package rline

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/xo/rline/ansi"
	"github.com/xo/rline/internal/text"
)

const (
	// termCorpusPath holds what the C term.c wrote.
	termCorpusPath = "testdata/term.txt"

	// termProbePath is where tools/build-probe-term.sh puts the probe.
	termProbePath = ".build/probe-term"
)

// termEnvVars are every variable that term_new and term_is_interactive read.
var termEnvVars = []string{
	"COLORTERM", "TERM", "NO_COLOR",
	"WT_SESSION", "ITERM_SESSION_ID", "VSCODE_PID",
	"COLUMNS", "LINES",
}

// fixedSize answers with the size of the pseudo-terminal the C probe uses.
type fixedSize struct {
	cols int
	rows int
}

func (f fixedSize) size() (int, int, bool) { return f.cols, f.rows, true }

// TestTermMatchesC replays the script that tools/probe-term.c runs and checks
// that the Go port writes the same bytes and keeps the same state.
func TestTermMatchesC(t *testing.T) {
	if *update {
		termRegenerate(t)
	}
	b, err := os.ReadFile(termCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-term.sh, then go test -update)", err)
	}
	want := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	got := termReplay(t)
	if len(got) != len(want) {
		t.Errorf("the replay produced %d lines and the corpus holds %d", len(got), len(want))
	}
	bad := 0
	for i := range max(len(got), len(want)) {
		g, w := lineAt(got, i), lineAt(want, i)
		if g == w {
			continue
		}
		bad++
		if bad <= 20 {
			t.Errorf("%s:%d\n  got:  %s\n  want: %s", termCorpusPath, i+1, g, w)
		}
	}
	if bad > 20 {
		t.Errorf("%d differences in total, 20 shown", bad)
	}
	if bad == 0 {
		t.Logf("checked %d lines", len(want))
	}
}

// termReplay runs the same script as the C probe and returns the lines it
// would print.
func termReplay(t *testing.T) []string {
	t.Helper()
	restore := saveEnv(t)
	defer restore()

	var out []string
	var sink bytes.Buffer
	emit := func(tag string) {
		out = append(out, "out "+tag+" "+escapeTerm(sink.Bytes()))
		sink.Reset()
	}
	newT := func() *term {
		return newTerm(&sink, termOptions{Sizer: fixedSize{cols: 80, rows: 24}})
	}

	// Palette detection.
	for i, e := range termEnvCases {
		setTermEnv(e[0], e[1], e[2])
		tm := newT()
		out = append(out, fmt.Sprintf("palette %d %d %d %d", i, tm.palette, boolInt(tm.nocolor), tm.colorBits()))
		tm.free()
		emit("discard")
	}

	// term_is_interactive.
	for _, name := range termInteractiveCases {
		// The probe always sets TERM here, including to the empty string,
		// which is a case of its own: an empty string is part of every
		// string, so the reversed test inside isInteractive answers yes.
		for _, k := range termEnvVars {
			_ = os.Unsetenv(k)
		}
		_ = os.Setenv("TERM", name) //nolint:usetesting // must unset the others too
		tm := newT()
		out = append(out, "interactive "+escapeTerm([]byte(name))+" "+fmt.Sprint(boolInt(isInteractive())))
		tm.free()
		emit("discard")
	}

	// Writing.
	setTermEnv("", "xterm-256color", "")
	for i, s := range termWriteCases {
		tm := newT()
		tm.setBufferMode(unbuffered)
		emit("discard")
		tm.write(s)
		tm.flush()
		emit("write")
		out = append(out, fmt.Sprintf("attrafter %d %s", i, attrString(tm.getAttr())))
		tm.free()
		emit("discard")
	}

	// Cursor movement, line clearing and the plain attribute setters.
	{
		tm := newT()
		tm.setBufferMode(unbuffered)
		emit("discard")
		step := func(tag string, fn func()) {
			fn()
			tm.flush()
			emit(tag)
		}
		for _, n := range []int{-1, 0, 1, 2, 10, 999} {
			step("left", func() { tm.left(n) })
			step("right", func() { tm.right(n) })
			step("up", func() { tm.up(n) })
			step("down", func() { tm.down(n) })
		}
		step("clearline", tm.clearLine)
		step("cleareol", tm.clearToEndOfLine)
		step("startline", tm.startOfLine)
		step("attrreset", tm.attrReset)
		step("ul1", func() { tm.underline(true) })
		step("ul0", func() { tm.underline(false) })
		step("rev1", func() { tm.reverse(true) })
		step("rev0", func() { tm.reverse(false) })
		step("bold1", func() { tm.bold(true) })
		step("bold0", func() { tm.bold(false) })
		step("it1", func() { tm.italic(true) })
		step("it0", func() { tm.italic(false) })
		step("writeln", func() { tm.writeln("ln") })
		step("writechar", func() { tm.writeChar('x') })
		step("writecharnl", func() { tm.writeChar('\n') })
		step("repeat3", func() { tm.writeRepeat("ab", 3) })
		step("repeat0", func() { tm.writeRepeat("ab", 0) })
		step("repeatneg", func() { tm.writeRepeat("ab", -1) })
		out = append(out, fmt.Sprintf("dim %d %d", tm.getWidth(), tm.getHeight()))
		tm.free()
		emit("discard")
	}

	// Setting attributes.
	{
		tm := newT()
		tm.setBufferMode(unbuffered)
		emit("discard")
		for i, s := range []string{"1", "1", "31", "31", "0", "4", "38;5;33", "38;2;1;2;3", "22", ""} {
			tm.setAttr(ansi.ParseSGR(s))
			tm.flush()
			emit("setattr")
			out = append(out, fmt.Sprintf("attrstate %d %s", i, attrString(tm.getAttr())))
		}
		tm.free()
		emit("discard")
	}

	// Formatted output.
	{
		tm := newT()
		tm.setBufferMode(unbuffered)
		emit("discard")
		ab := &ansi.AttrBuf{}
		sb := &text.Buffer{}
		appendMarked(ab, sb, "red", ansi.ParseSGR("31"))
		appendMarked(ab, sb, "plain", ansi.Attr{})
		appendMarked(ab, sb, "bold", ansi.ParseSGR("1"))
		s := sb.String()
		tm.writeFormatted(s, ab.Extend(sb.Length()))
		tm.flush()
		emit("formatted")
		tm.writeFormatted(s, nil)
		tm.flush()
		emit("formattednull")
		tm.free()
		emit("discard")
	}

	// Buffer modes.
	{
		tm := newT()
		emit("discard")
		out = append(out, fmt.Sprintf("bufmode %d", tm.setBufferMode(buffered)))
		tm.write("buffered")
		emit("buffered_nothing")
		tm.flush()
		emit("buffered_flushed")
		out = append(out, fmt.Sprintf("bufmode %d", tm.setBufferMode(lineBuffered)))
		tm.write("no newline")
		emit("line_nothing")
		tm.write(" and\n")
		emit("line_flushed")
		out = append(out, fmt.Sprintf("bufmode %d", tm.setBufferMode(unbuffered)))
		emit("unbuffered_switch")
		tm.write("direct")
		emit("unbuffered")
		tm.free()
		emit("discard")
	}
	return out
}

// termEnvCases are the environments the probe tests palette detection with.
var termEnvCases = [][3]string{
	{"", "", ""},
	{"truecolor", "", ""}, {"24bit", "", ""}, {"direct", "", ""},
	{"8bit", "", ""}, {"256color", "", ""}, {"4bit", "", ""},
	{"16color", "", ""}, {"3bit", "", ""}, {"8color", "", ""},
	{"1bit", "", ""}, {"nocolor", "", ""}, {"monochrome", "", ""},
	{"", "xterm", ""}, {"", "xterm-256color", ""}, {"", "xterm-truecolor", ""},
	{"", "alacritty", ""}, {"", "kitty", ""}, {"", "gnome", ""},
	{"", "screen-16color", ""}, {"", "vt100-8color", ""}, {"", "dumb", ""},
	{"", "monochrome", ""}, {"", "linux", ""},
	{"truecolor", "dumb", ""},
	{"", "", "1"}, {"truecolor", "", "1"},
	{"\x00", "\x00", ""}, // both set but empty, which the probe writes as ""
}

// termInteractiveCases are the TERM values the probe tests.
var termInteractiveCases = []string{
	"dumb", "DUMB", "cons25", "emacs", "EMACS", "xterm", "", "b|DUMB",
	"umb", "25|CONS", "|", "dum", "screen",
}

// termWriteCases are the strings the probe writes.
var termWriteCases = []string{
	"", "a", "hello", "line\nnext", "tab\there", "cr\rhere",
	"\x01\x02", "\x07bell", "\x08back", "\x0b\x0c", "\x1b", "\x1b[",
	"\x1b[m", "\x1b[31m", "\x1b[1;4mbold", "\x1b[0m", "\x1b]0;title\x07",
	"\xc3\xa9", "\xe6\x97\xa5\xe6\x9c\xac", "\xff", "a\xffb",
	"\xf0\x9f\x98\x80", "mixed \x1b[32mgreen\x1b[39m end",
}

// setTermEnv clears every variable term reads and sets the ones given. An
// empty value means the variable stays unset, and "\x00" means it is set to
// the empty string.
func setTermEnv(colorterm, termName, nocolor string) {
	for _, k := range termEnvVars {
		_ = os.Unsetenv(k)
	}
	// t.Setenv cannot help here: the replay has to unset variables as well as
	// set them, and the testing package offers no way to unset one.
	set := func(k, v string) {
		switch v {
		case "":
			return
		case "\x00":
			_ = os.Setenv(k, "") //nolint:usetesting // paired with Unsetenv above
		default:
			_ = os.Setenv(k, v) //nolint:usetesting // paired with Unsetenv above
		}
	}
	set("COLORTERM", colorterm)
	set("TERM", termName)
	set("NO_COLOR", nocolor)
}

// saveEnv records every variable term reads, and returns a function that puts
// them back.
func saveEnv(t *testing.T) func() {
	t.Helper()
	saved := make(map[string]*string, len(termEnvVars))
	for _, k := range termEnvVars {
		if v, ok := os.LookupEnv(k); ok {
			saved[k] = &v
		} else {
			saved[k] = nil
		}
	}
	return func() {
		for k, v := range saved {
			if v == nil {
				_ = os.Unsetenv(k)
				continue
			}
			_ = os.Setenv(k, *v) //nolint:usetesting // restoring what was saved
		}
	}
}

// escapeTerm renders bytes the way the C probe prints them.
func escapeTerm(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c == 0x1b:
			sb.WriteString(`\e`)
		case c == '\\':
			sb.WriteString(`\\`)
		case c == '\r':
			sb.WriteString(`\r`)
		case c == '\n':
			sb.WriteString(`\n`)
		case c == '\t':
			sb.WriteString(`\t`)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&sb, `\x%02x`, c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// termRegenerate runs the C probe and writes the corpus.
func termRegenerate(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(termProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-term.sh", termProbePath)
	}
	out, err := exec.Command(termProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(termCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", termCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", termCorpusPath, len(out))
}
