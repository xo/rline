package rline

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The completion menu checked against the C.
//
// The menu is the one part of the editor that reads its own keys, so it
// cannot be driven from outside: each case loads the keys it types into the
// tty, calls the menu once, and compares what was drawn and what was left
// behind. tools/probe-completion-menu.c runs the same script against the C.

// menuProbePath is where tools/build-probe-completion-menu.sh puts the probe.
const menuProbePath = ".build/probe-completion-menu"

// menuCorpusPath returns where the recordings this build compares against
// live.
//
// Split by branch rather than by system, for the reason refreshCorpusPath
// gives: the menu draws through the same redraw, and the mark at the end of a
// wrapped row is a compile time choice. 152 of these recordings carry it.
func menuCorpusPath() string {
	return filepath.Join("testdata", corpusVariant, "completion-menu.txt")
}

// menuEntry is one completion a case offers.
type menuEntry struct {
	replacement string
	display     string
	help        string
}

// menuSet is a named set of completions, matching the sets in the C probe.
type menuSet struct {
	name  string
	items []menuEntry
}

// menuScript is a named sequence of keys, as the bytes a terminal sends.
type menuScript struct {
	name string
	keys string
}

// The sets and scripts, in the order the C probe walks them. They must stay
// in step with tools/probe-completion-menu.c: the corpus is compared line by
// line and each line names its set and script, so a set added on one side and
// not the other shows up as a mismatch rather than passing quietly.
var (
	menuSets = []menuSet{
		{"two", plainEntries("alpha", "alpine")},
		// Exactly three, and narrow. The three column branch asks for more
		// than three, so this is what pins that it does.
		{"three", plainEntries("ta", "tb", "tc")},
		{"five", plainEntries("aa", "ab", "ac", "ad", "ae")},
		{"nine", plainEntries("a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9")},
		{"twelve", plainEntries("b01", "b02", "b03", "b04", "b05", "b06",
			"b07", "b08", "b09", "b10", "b11", "b12")},
		// Nine wide, which at this terminal width is the one set that sits on
		// the boundary between three columns and two: three need
		// 3*(3+9)+2*2 = 40 and the width to beat is 40, so they do not fit
		// and two do. It is here because it is the only thing that can tell
		// the two spaces between three columns from one, or the one column
		// the menu keeps in hand from none. Neither is reachable at a width
		// of forty, where the arithmetic cannot land on the boundary at all.
		{"w9", plainEntries("g23456789", "h23456789", "i23456789",
			"j23456789", "k23456789")},
		// Too wide for three columns and for two, so this falls to the list.
		{"wide", plainEntries(
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaab",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaac",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaad",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaae")},
		// Too wide for any column layout, and more than the list shows.
		{"wide10", plainEntries(
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaa00", "aaaaaaaaaaaaaaaaaaaaaaaaaaaa01",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaa02", "aaaaaaaaaaaaaaaaaaaaaaaaaaaa03",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaa04", "aaaaaaaaaaaaaaaaaaaaaaaaaaaa05",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaa06", "aaaaaaaaaaaaaaaaaaaaaaaaaaaa07",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaa08", "aaaaaaaaaaaaaaaaaaaaaaaaaaaa09")},
		// The first of these is what the line already reads, so applying it
		// moves neither the line nor the cursor. That is the one case the
		// menu redraws for even though nothing changed, because the selection
		// still has to move.
		{"noop", plainEntries("a", "ab", "ac", "ad", "ae")},
		// Too wide for three columns, narrow enough for two. Three
		// columns need 3*(3+w)+4 < 40 and two need 2*(3+w)+2 < 40, so a width
		// of twelve reaches the two column branch.
		{"mid", plainEntries("cccccccccccc", "ccccccccccca", "cccccccccccb",
			"cccccccccccd", "ccccccccccce", "cccccccccccf", "cccccccccccg")},
		// Four of that width, which the two column branch asks for more than,
		// and ten, which is more than it shows.
		{"mid4", plainEntries("eeeeeeeeeeee", "eeeeeeeeeeea",
			"eeeeeeeeeeeb", "eeeeeeeeeeec")},
		// The same width, but only six of them, which is the other row count
		// the two column branch can choose.
		{"mid6", plainEntries("dddddddddddd", "ddddddddddda", "dddddddddddb",
			"dddddddddddc", "ddddddddddde", "dddddddddddf")},
		{"mid10", plainEntries("ffffffffff00", "ffffffffff01", "ffffffffff02",
			"ffffffffff03", "ffffffffff04", "ffffffffff05", "ffffffffff06",
			"ffffffffff07", "ffffffffff08", "ffffffffff09")},
		{"help", []menuEntry{
			{"one", "[ic-emphasis]one[/]", "the first"},
			{"two", "two", "the second"},
			{"three", "", "the third"},
		}},
		// Outside ASCII, so the column width is measured rather than counted.
		{"utf8", plainEntries("日本", "日曜", "été", "naïve", "straße")},
	}

	menuScripts = []menuScript{
		{"esc", "\x1b"},
		{"enter", "\r"},
		{"down-enter", "\x1b[B\r"},
		{"down2-enter", "\x1b[B\x1b[B\r"},
		{"up-enter", "\x1b[A\r"},
		{"tab-enter", "\t\r"},
		{"digit3", "3"},
		{"digit9", "9"},
		{"right", "\x1b[C"},
		{"end", "\x1b[F"},
		{"letter", "x"},
		{"pagedown", "\x1b[6~"},
		{"ctrl-j", "\n"},
		{"home", "\x1b[H"},
	}
)

// menuGenerated is what the test completer offers when the menu asks for the
// rest of the completions. A completer has to be set at all: the C leaves the
// default filename completer in place otherwise, which would make the
// recording depend on which directory it was made in.
var menuGenerated = []string{
	"gen01", "gen02", "gen03", "gen04", "gen05", "gen06",
	"gen07", "gen08", "gen09", "gen10", "gen11", "gen12",
}

// menuCompleter offers menuGenerated, whatever the prefix.
var menuCompleter = CompleterFunc(func(c *Completion, _ string) {
	for _, name := range menuGenerated {
		if !c.Add(name) {
			return
		}
	}
})

// plainEntries makes entries that have no display and no help of their own.
func plainEntries(replacements ...string) []menuEntry {
	entries := make([]menuEntry, len(replacements))
	for i, r := range replacements {
		entries[i] = menuEntry{replacement: r}
	}
	return entries
}

// TestCompletionMenuMatchesC replays the script that
// tools/probe-completion-menu.c runs and checks that the port draws the same
// bytes and leaves the same state.
func TestCompletionMenuMatchesC(t *testing.T) {
	if *update {
		menuRegenerate(t)
	}
	path := menuCorpusPath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no recordings for the %q build: %v\n"+
			"Record them on a machine that compiles the same wrap mark, which "+
			"for %q means macOS and otherwise means anything but macOS, and "+
			"that has a C compiler:\n"+
			"  ./tools/build-probe-completion-menu.sh && "+
			"go test . -run TestCompletionMenuMatchesC -update",
			corpusVariant, err, corpusVariant)
	}
	want := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	got := menuReplay(t)
	if len(got) != len(want) {
		t.Errorf("the replay produced %d lines and the corpus holds %d", len(got), len(want))
	}
	// A corpus this one could pass against by accident is one that holds no
	// menus at all, so say how many it holds.
	if drawn := strings.Count(string(b), "menu "); drawn < 1000 {
		t.Errorf("the corpus holds %d menus, which is too few to prove anything", drawn)
	}
	bad := 0
	for i := range max(len(got), len(want)) {
		g, w := lineAt(got, i), lineAt(want, i)
		if g == w {
			continue
		}
		bad++
		if bad <= 20 {
			t.Errorf("%s:%d\n  got:  %s\n  want: %s", path, i+1, g, w)
		}
	}
	if bad > 20 {
		t.Errorf("%d differences in total, 20 shown", bad)
	}
}

// menuReplay runs the same script as the C probe.
func menuReplay(t *testing.T) []string {
	t.Helper()
	restore := saveEnv(t)
	defer restore()
	setTermEnv("", "xterm-256color", "")

	var sink bytes.Buffer
	tm := newTerm(&sink, termOptions{Sizer: fixedSize{cols: 41, rows: 6}})
	ev := &env{
		term:          tm,
		bb:            newBBCode(tm),
		completions:   &completions{},
		promptMarker:  "> ",
		cpromptMarker: "| ",
		opts: editOptions{
			MatchBraces:  "()[]{}",
			AutoBraces:   "()[]{}",
			MultilineEOL: '\\',
		},
		noHighlight: true,
	}
	// The styles a Reader defines. Without them every style name in the menu
	// renders to nothing, and the recording cannot tell one name from
	// another. The C probe defines the same list by hand, so a name that
	// drifts between the two shows up here as well.
	for _, s := range defaultStyles {
		ev.bb.styleDef(s[0], s[1])
	}
	ev.completions.setCompleter(menuCompleter, nil)
	e := &editor{}

	emit := func() string {
		s := escapeBB(sink.Bytes())
		sink.Reset()
		return s
	}
	out := make([]string, 0, 4096)
	caseno := 0
	for _, set := range menuSets {
		for _, script := range menuScripts {
			for _, more := range []bool{false, true} {
				for _, noPreview := range []bool{false, true} {
					for _, autoTab := range []bool{false, true} {
						for _, utf8 := range []bool{false, true} {
							ev.completions.clear()
							// add refuses everything once the budget a
							// completer would have been given runs out, and
							// nothing runs a completer here, so it is set by
							// hand the way the C probe sets it.
							ev.completions.completerMax = maxCompletionsToShow
							for _, it := range set.items {
								ev.completions.add(it.replacement, it.display, it.help, 1, 0)
							}
							e.input.replace("a")
							e.extra.clear()
							e.hint.clear()
							e.hintHelp.clear()
							e.pos = 1
							e.termW = 41
							e.curRows = 1
							e.curRow = 0
							e.promptText = "p"
							e.modified = false
							e.disableUndo = false
							e.undo = editStack{}
							e.redo = editStack{}
							ev.completeNoPreview = noPreview
							ev.completeAutoTab = autoTab

							ev.tty = newTTY(&idleReader{bytes: []byte(script.keys)})
							ev.tty.isUTF8 = utf8
							ev.tty.setEscDelay(0, 0)

							tm.flush()
							out = append(out, emit())

							ev.completionMenu(e, more)
							tm.flush()

							out = append(out, fmt.Sprintf(
								"menu %d %s %s more=%d nopreview=%d autotab=%d utf8=%d %s"+
									" pos=%d input=%s left=%d pushed=%s",
								caseno, set.name, script.name,
								btoi(more), btoi(noPreview), btoi(autoTab), btoi(utf8),
								emit(), e.pos, escapeBB([]byte(e.input.string())),
								ev.completions.count(), pushedCodes(ev.tty)))
							caseno++
						}
					}
				}
			}
		}
	}
	return out
}

// btoi turns a flag into the 0 or 1 the C probe prints.
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// pushedCodes formats the keys the menu handed back to the edit loop.
func pushedCodes(t *tty) string {
	if len(t.pushedCodes) == 0 {
		return "-"
	}
	var sb strings.Builder
	for _, c := range t.pushedCodes {
		fmt.Fprintf(&sb, "%08x", uint32(c))
	}
	return sb.String()
}

// menuRegenerate runs the C probe and writes the corpus.
//
// It refuses to run anywhere but Linux and macOS, for the reason
// refreshRegenerate gives: the "default" set is shared by every system that
// is not macOS, so a system that recorded it would overwrite the recording
// the others compare against.
func menuRegenerate(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Fatalf("refusing to record on %s: the %q recordings are shared with "+
			"other systems, and only linux and darwin may write them",
			runtime.GOOS, corpusVariant)
	}
	if _, err := os.Stat(menuProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-completion-menu.sh", menuProbePath)
	}
	out, err := exec.Command(menuProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	path := menuCorpusPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	t.Logf("wrote %s: %d bytes", path, len(out))
}
