package readline

import (
	"bufio"
	"encoding/hex"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/xo/inputrc"
	"github.com/xo/rline/key"
)

// update records the corpora again from GNU readline, through the probe that
// tools/build-probe-readline.sh builds. It needs readline 8.3 and its
// headers, so it runs only when asked.
var update = flag.Bool("update", false, "record the corpora again from GNU readline")

const (
	emacsPath = "../../testdata/readline-emacs.txt"
	bindsPath = "../../testdata/readline-binds.txt"
	probePath = "../../.build/probe-readline"
)

// TestMain records the corpora before the tests read them, when -update asks.
func TestMain(m *testing.M) {
	flag.Parse()
	if *update {
		for path, args := range map[string][]string{emacsPath: nil, bindsPath: {"binds"}} {
			out, err := exec.Command(probePath, args...).Output()
			if err != nil {
				panic("running " + probePath + ": " + err.Error() + " (run tools/build-probe-readline.sh)")
			}
			if err := os.WriteFile(path, out, 0o644); err != nil {
				panic(err)
			}
		}
	}
	os.Exit(m.Run())
}

// version is the readline that the corpora come from (D46).
const version = "readline 8.3"

// binding is one key sequence and the command it runs.
type binding struct {
	seq  string
	name string
}

// bindKeymap builds a keymap from bindings.
func bindKeymap(bindings []binding) *Keymap {
	km := NewKeymap()
	for _, b := range bindings {
		km.BindSeq(b.seq, keysOf(b.seq), b.name)
	}
	return km
}

// defaultBindings reads the bindings that the probe dumped for the keymap
// called name.
func defaultBindings(t *testing.T, name string) []binding {
	t.Helper()
	f, err := os.Open(filepath.FromSlash(bindsPath))
	if err != nil {
		t.Fatalf("reading %s: %v (run tools/build-probe-readline.sh, then go test -update)", bindsPath, err)
	}
	defer f.Close()
	var out []binding
	s := bufio.NewScanner(f)
	if !s.Scan() || s.Text() != version {
		t.Fatalf("%s comes from %q, and the tests expect %q", bindsPath, s.Text(), version)
	}
	for s.Scan() {
		if strings.HasPrefix(s.Text(), "command ") {
			continue
		}
		fields := strings.SplitN(s.Text(), " ", 3)
		if len(fields) != 3 {
			t.Fatalf("%s: a binding has three fields: %q", bindsPath, s.Text())
		}
		if fields[0] != name {
			continue
		}
		out = append(out, binding{seq: inputrc.Unescape(fields[2]), name: fields[1]})
	}
	if len(out) == 0 {
		t.Fatalf("%s binds nothing in the keymap %s", bindsPath, name)
	}
	return out
}

// keysOf turns text into the keys that typing it gives, one for each
// character.
func keysOf(text string) []key.Code {
	out := make([]key.Code, 0, utf8.RuneCountInString(text))
	for _, r := range text {
		out = append(out, key.Code(r))
	}
	return out
}

// step is one key of a recorded case, and the state readline was in after
// it.
type step struct {
	keys  []byte
	point int
	mark  int
	done  bool
	line  string
	list  []string // the completions readline listed for this key
	// prompt is the prompt that readline showed after the key in place of
	// the program's, as incremental search does.
	prompt string
	// out is what readline wrote to its output stream for the key, as the
	// commands that print write.
	out string
}

// recordedCase is one case of the corpus: its history, the bindings it
// makes, and its steps.
type recordedCase struct {
	name     string
	history  []string
	bindings []binding
	steps    []step
	words    []string // what completion offers, for a case that completes
	rc       []string // lines of an inputrc file that readline read first
}

// readCorpus reads the cases of a corpus, in order.
func readCorpus(t *testing.T, path string) []recordedCase {
	t.Helper()
	f, err := os.Open(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("reading %s: %v (run tools/build-probe-readline.sh, then go test -update)", path, err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() || s.Text() != version {
		t.Fatalf("%s comes from %q, and the tests expect %q", path, s.Text(), version)
	}
	var cases []recordedCase
	at := func(name string) *recordedCase {
		if len(cases) == 0 || cases[len(cases)-1].name != name {
			cases = append(cases, recordedCase{name: name})
		}
		return &cases[len(cases)-1]
	}
	var list []string
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) >= 2 && (f[0] == "words" || f[0] == "list") {
			texts := make([]string, 0, len(f)-2)
			for _, h := range f[2:] {
				texts = append(texts, mustText(t, h))
			}
			if f[0] == "words" {
				at(f[1]).words = texts
			} else {
				list = texts
			}
			continue
		}
		if len(f) == 3 && f[0] == "rc" {
			c := at(f[1])
			c.rc = append(c.rc, mustText(t, f[2]))
			continue
		}
		if len(f) == 3 && f[0] == "prompt" {
			c := at(f[1])
			c.steps[len(c.steps)-1].prompt = mustText(t, f[2])
			continue
		}
		if len(f) == 3 && f[0] == "out" {
			c := at(f[1])
			c.steps[len(c.steps)-1].out = mustText(t, f[2])
			continue
		}
		if len(f) == 3 && f[0] == "history" {
			c := at(f[1])
			c.history = append(c.history, mustText(t, f[2]))
			continue
		}
		if len(f) == 4 && f[0] == "bind" {
			c := at(f[1])
			seq := inputrc.Unescape(mustText(t, f[3]))
			c.bindings = append(c.bindings, binding{seq: seq, name: f[2]})
			continue
		}
		if len(f) != 6 {
			t.Fatalf("%s: a step has six fields: %q", path, s.Text())
		}
		c := at(f[0])
		var keys []byte
		if f[1] != "-" {
			keys = mustHex(t, f[1])
		}
		c.steps = append(c.steps, step{
			keys:  keys,
			point: mustInt(t, f[2]),
			mark:  mustInt(t, f[3]),
			done:  f[4] == "1",
			line:  mustText(t, f[5]),
			list:  list,
		})
		list = nil
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}

// TestEmacsCommandsMatchReadline replays every recorded case on a line with
// readline's default emacs keymap, and checks that each key leaves the line,
// the cursor, the mark and whether the line was handed back as readline
// left them.
func TestEmacsCommandsMatchReadline(t *testing.T) {
	t.Parallel()
	defaults := defaultBindings(t, "emacs")
	viInsert := defaultBindings(t, "vi-insert")
	viCommand := defaultBindings(t, "vi-command")
	cases := readCorpus(t, emacsPath)
	if len(cases) < 60 {
		t.Fatalf("%s holds %d cases, expected at least 60", emacsPath, len(cases))
	}
	for _, rc := range cases {
		t.Run(rc.name, func(t *testing.T) {
			t.Parallel()
			h := &History{}
			for _, e := range rc.history {
				h.Add(e)
			}
			km := bindKeymap(append(append([]binding(nil), defaults...), rc.bindings...))
			vi, vc := bindKeymap(viInsert), bindKeymap(viCommand)
			vars := readInputrc(t, map[string]*Keymap{"emacs": km, "vi-insert": vi, "vi-command": vc}, rc.rc)
			newLine := func() *Line {
				l := New(km, h)
				l.SetKeymaps(km, vi, vc)
				l.SetScreen(80, 0)
				// The probe sets keyseq-timeout to 0, and TERM to xterm, whose
				// standout sequences readline takes for the active region.
				l.Configure(map[string]any{
					"keyseq-timeout":            0,
					"active-region-start-color": "\x1b[7m",
					"active-region-end-color":   "\x1b[27m",
				})
				if unknown := l.Configure(vars); len(unknown) > 0 {
					t.Fatalf("the case sets %q, which Configure does not know", unknown)
				}
				// The probe sets HOME to /home/probe for tilde expansion.
				l.home = func(name string) (string, bool) { return "/home/probe", name == "" }
				if rc.words != nil {
					l.SetCompleter(wordCompleter(rc.words))
				}
				return l
			}
			l := newLine()
			for i, st := range rc.steps {
				// A case that goes on after the line was handed back edits the
				// next line, which operate-and-get-next can start with a
				// history line. In any other case readline reads no more.
				if l.Done() && !st.done {
					next, ok := l.NextLine()
					l = newLine()
					if text, found := h.Entry(next); ok && found {
						l.StartWith(text)
					}
				}
				keys := st.keys
				// The first step of most cases types the text the case
				// starts from, and the corpus gives the line it left.
				if keys == nil {
					keys = []byte(st.line)
				}
				for len(keys) > 0 {
					r, n := utf8.DecodeRune(keys)
					l.Key(key.Code(r))
					keys = keys[n:]
				}
				got := step{point: l.Point(), mark: l.Mark(), done: l.Done(), line: l.Text()}
				listing := l.Listing()
				listed := make([]string, 0, len(listing))
				for _, c := range listing {
					listed = append(listed, c.Text)
				}
				if p := l.Prompt(); p != st.prompt && !st.done {
					t.Fatalf("step %d, key %x: the prompt is %q, and readline showed %q", i, st.keys, p, st.prompt)
				}
				if !slices.Equal(listed, st.list) {
					t.Fatalf("step %d, key %x: listed %q, and readline listed %q", i, st.keys, listed, st.list)
				}
				// readline writes line breaks around what it prints, which
				// rline draws in its own way, so only the text is compared.
				if out, want := strings.Trim(l.Output(), "\r\n"), strings.Trim(st.out, "\r\n"); out != want {
					t.Fatalf("step %d, key %x: printed %q, and readline printed %q", i, st.keys, out, want)
				}
				if st.done {
					// Once readline hands the line back, its buffer and cursor
					// are already reset for the next line, so the probe
					// prints no line and a cursor of 0. Only the hand back
					// itself is compared.
					got.line, got.point, got.mark = "", st.point, st.mark
				}
				if got.point != st.point || got.mark != st.mark || got.done != st.done || got.line != st.line {
					t.Fatalf("step %d, key %x: got cursor %d, mark %d, done %v, line %q\n"+
						"readline: cursor %d, mark %d, done %v, line %q",
						i, st.keys, got.point, got.mark, got.done, got.line,
						st.point, st.mark, st.done, st.line)
				}
			}
		})
	}
}

// inputrcHandler reads lines of an inputrc file for a case: it keeps the
// variables that the lines set, and binds their keys in a keymap.
type inputrcHandler struct {
	*inputrc.Config
	t    *testing.T
	km   map[string]*Keymap
	vars map[string]any
}

func (h *inputrcHandler) Set(name string, v any) error {
	h.vars[name] = v
	return nil
}

func (h *inputrcHandler) Bind(keymap, seq, action string, macro bool) error {
	// The meta and Ctrl-X keymaps are the keys after Esc and after Ctrl-X,
	// and vi and vi-move are other names of vi-command.
	switch keymap {
	case "emacs", "emacs-standard":
		keymap = "emacs"
	case "emacs-meta":
		keymap, seq = "emacs", "\x1b"+seq
	case "emacs-ctlx":
		keymap, seq = "emacs", "\x18"+seq
	case "vi", "vi-move":
		keymap = "vi-command"
	}
	km := h.km[keymap]
	if km == nil {
		h.t.Fatalf("the case binds in the keymap %s, which this test does not read", keymap)
	}
	if macro {
		km.BindMacroSeq(seq, keysOf(seq), action, keysOf(action))
		return nil
	}
	km.BindSeq(seq, keysOf(seq), action)
	return nil
}

// readInputrc reads the lines of an inputrc file that a case gave readline,
// binds their keys in km, and returns the variables that they set. The probe
// names itself rline-probe, which $if tests.
func readInputrc(t *testing.T, km map[string]*Keymap, lines []string) map[string]any {
	t.Helper()
	h := &inputrcHandler{Config: inputrc.NewDefaultConfig(), t: t, km: km, vars: map[string]any{}}
	if len(lines) == 0 {
		return h.vars
	}
	p := inputrc.New(inputrc.WithApp("rline-probe"))
	if err := p.Parse(strings.NewReader(strings.Join(lines, "\n")+"\n"), h); err != nil {
		t.Fatal(err)
	}
	if errs := p.Errs(); len(errs) > 0 {
		t.Fatalf("reading %q: %v", lines, errs)
	}
	return h.vars
}

// wordCompleter completes the word before the cursor from words, as the
// probe's completion function does, and adds a space after a single one, as
// readline does by default.
func wordCompleter(words []string) Completer {
	return func(line string, point int) []Candidate {
		start := strings.LastIndexAny(line[:point], completerWordBreaks) + 1
		var out []Candidate
		for _, w := range words {
			if strings.HasPrefix(w, line[start:point]) {
				out = append(out, Candidate{Start: start, End: point, Text: w, Suffix: " "})
			}
		}
		return out
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("reading hex %q: %v", s, err)
	}
	return b
}

func mustText(t *testing.T, s string) string {
	t.Helper()
	if s == "-" {
		return ""
	}
	return string(mustHex(t, s))
}

func mustInt(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("reading a number %q: %v", s, err)
	}
	return n
}

// TestEveryCommandIsReadlines checks that every command this package has is
// one that readline 8.3 has, by the same name, so that an inputrc file can
// bind it (D44). A name readline does not know is one no inputrc can reach.
func TestEveryCommandIsReadlines(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.FromSlash(bindsPath))
	if err != nil {
		t.Fatal(err)
	}
	known := make(map[string]bool)
	for _, line := range strings.Split(string(b), "\n") {
		if name, ok := strings.CutPrefix(line, "command "); ok {
			known[name] = true
		}
	}
	if len(known) < 100 {
		t.Fatalf("%s names %d commands, expected readline's full list", bindsPath, len(known))
	}
	for name := range commands {
		if !known[name] {
			t.Errorf("%q is not a command of %s", name, version)
		}
	}
}

// TestInputrcDefaultsAreReadlines checks that the default keymaps and the
// command list of the inputrc package are readline 8.3's, as the probe
// dumped them (D41, D46). The package once took its defaults from bash, which
// binds commands that only bash has. A difference here is one to fix in
// inputrc, not here.
func TestInputrcDefaultsAreReadlines(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.FromSlash(bindsPath))
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]map[string]string)
	commandNames := make(map[string]bool)
	for _, line := range strings.Split(string(b), "\n")[1:] {
		if name, ok := strings.CutPrefix(line, "command "); ok {
			commandNames[name] = true
			continue
		}
		f := strings.SplitN(line, " ", 3)
		if len(f) != 3 {
			continue
		}
		if want[f[0]] == nil {
			want[f[0]] = make(map[string]string)
		}
		want[f[0]][inputrc.Unescape(f[2])] = f[1]
	}
	got := inputrc.DefaultBinds()
	for _, km := range []string{"emacs", "vi-insert", "vi-command"} {
		if len(want[km]) == 0 {
			t.Fatalf("%s holds no binding for %s", bindsPath, km)
		}
		for seq, name := range want[km] {
			if g := got[km][seq]; g.Action != name || g.Macro {
				t.Errorf("%s: readline binds %q to %s, and inputrc to %q", km, seq, name, g.Action)
			}
		}
		for seq, g := range got[km] {
			if _, ok := want[km][seq]; !ok {
				t.Errorf("%s: inputrc binds %q to %s, and readline binds nothing there", km, seq, g.Action)
			}
		}
	}
	cmds := inputrc.Commands()
	if len(cmds) != len(commandNames) {
		t.Errorf("inputrc names %d commands, and readline has %d", len(cmds), len(commandNames))
	}
	for _, name := range cmds {
		if !commandNames[name] {
			t.Errorf("inputrc names %q, which is not a command of %s", name, version)
		}
	}
}
