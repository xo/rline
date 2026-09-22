package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// blessedDir holds the byte logs a person has looked at and accepted.
//
// They are per terminal, and that is the point rather than a nuisance: the
// same keystroke reaches the program as different bytes depending on the
// emulator, so one blessed log for all of them would either be wrong or
// would have to throw away the difference this harness exists to find.
const blessedDir = "uitest/testdata"

// compare holds this run's byte log against the blessed one.
//
// Only the log is compared. The screenshot is an artifact for a person: font
// rendering varies by machine, by hinting and by graphics driver, so an image
// comparison would fail for reasons that have nothing to do with this port.
func compare(t Terminal, s Session, res result, update bool) error {
	if s.Watch {
		fmt.Printf("   watch only, no log compared: %s\n", filepath.Dir(res.shotPath))
		return nil
	}
	want := filepath.Join(blessedDir, t.Name, s.Name+".log")
	got, err := os.ReadFile(res.logPath)
	if err != nil {
		return fmt.Errorf("reading this run's log: %w", err)
	}
	if update {
		if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
			return fmt.Errorf("making the blessed directory: %w", err)
		}
		if err := os.WriteFile(want, got, 0o644); err != nil {
			return fmt.Errorf("writing the blessed log: %w", err)
		}
		fmt.Printf("   blessed %s\n", want)
		return nil
	}
	old, err := os.ReadFile(want)
	if os.IsNotExist(err) {
		return fmt.Errorf("no blessed log yet: look at %s and %s, then rerun with -update",
			res.logPath, res.shotPath)
	}
	if err != nil {
		return fmt.Errorf("reading the blessed log: %w", err)
	}
	a, b := normalise(lines(string(old))), normalise(lines(string(got)))
	if strings.Join(a, "\n") == strings.Join(b, "\n") {
		return nil
	}
	return fmt.Errorf("the byte log changed:\n%s", diffLines(a, b))
}

// normalise removes from a log what the desktop contributed rather than the
// port.
//
// A terminal window is resized by things that have nothing to do with the
// test: another window opening, a focus change, the compositor settling a
// window it has just mapped. Every resize is a SIGWINCH and the editor
// repaints, so the log grows by a redraw that is identical to the one before
// it. Measured here: three runs of one session produced 14, 29 and 1 of them,
// all after the last keystroke, while the ten input lines were the same every
// time.
//
// The port is not spinning, which was the other candidate and was ruled out
// rather than assumed: the demo left idle on a plain pseudo-terminal for five
// seconds writes nothing after its prompt. So these really are resize events
// and dropping them is dropping the desktop, not the port.
//
// Two things are removed. Output after the last keystroke, which no input
// caused. And runs of identical output lines, which is what a repaint of an
// unchanged screen looks like.
//
// What that hides, stated plainly because a normalisation nobody has bounded
// is worse than none: a real regression that makes the port redraw the same
// thing twice would be invisible here. The screenshot and the grid still show
// the end state, and a redraw whose *content* changed still differs, so this
// is a hole in one direction only. If double-redrawing ever matters, it needs
// a test that counts rather than compares.
func normalise(in []string) []string {
	lastKey := -1
	for i, l := range in {
		if strings.HasPrefix(l, "> ") {
			lastKey = i
		}
	}
	if lastKey >= 0 {
		// Keep the answer to the last key: everything up to the next key,
		// which there is not one of, so up to the first repeat.
		end := len(in)
		for i := lastKey + 1; i < len(in); i++ {
			if i > lastKey+1 && in[i] == in[i-1] {
				end = i
				break
			}
		}
		in = in[:end]
	}
	out := make([]string, 0, len(in))
	for i, l := range in {
		if i > 0 && l == in[i-1] && strings.HasPrefix(l, "< ") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// diffLines returns the first few differing lines, with their numbers.
//
// A whole-file diff of escape sequences is unreadable and nobody scrolls it.
// The first disagreement is almost always the one that explains the rest.
func diffLines(a, b []string) string {
	var sb strings.Builder
	shown := 0
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x == y {
			continue
		}
		fmt.Fprintf(&sb, "     line %d\n       blessed: %s\n       now:     %s\n", i+1, x, y)
		if shown++; shown == 3 {
			fmt.Fprintf(&sb, "     (and possibly more)\n")
			break
		}
	}
	return sb.String()
}

func lines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}
