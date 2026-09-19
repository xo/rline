package capture

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"
)

// goldenDir returns the directory holding the recordings for the system the
// test is running on.
//
// A recording is only comparable against one made on the same system, because
// isocline itself writes different bytes on each. term_update_ansi16 is
// guarded by "#if __APPLE__", so on macOS the demo asks the terminal for its
// color palette with an OSC 4 sequence and waits for an answer. A bare
// pseudo-terminal never answers, so the demo waits out its timeout, which both
// adds the query to the output and moves the rest of the startup text into the
// next exchange. On Linux the query is not compiled in at all.
//
// So each system keeps its own set, and each one checks bytes. A set that is
// missing is a failure rather than a skip: a skip would put back the hole this
// layout exists to close.
func goldenDir() string {
	return filepath.Join("testdata", runtime.GOOS)
}

// TestGoldenSetIsPresent checks that this system has a recording for every
// session.
//
// This test carries no build tag on purpose. Recording is only written for
// Linux and macOS, and every test that does the recording is tagged to those
// two, so on any other system the whole comparison used to vanish from the
// build and the package reported success while checking nothing. A missing set
// has to be able to fail on the system that is missing it, which means the
// check has to compile there.
func TestGoldenSetIsPresent(t *testing.T) {
	t.Parallel()
	dir := goldenDir()
	missing := 0
	for _, s := range Sessions {
		if _, err := os.Stat(filepath.Join(dir, s.Name+".txt")); err != nil {
			missing++
		}
	}
	if missing == 0 {
		return
	}
	t.Errorf("%s has no recording for %d of the %d sessions.\n"+
		"Record them on a %s machine with:\n"+
		"  ./tools/build-demo.sh && go test ./internal/capture -update\n"+
		"Recording is written for linux and darwin only. On %s it needs "+
		"Record to be written first, which for Windows means a pseudo console "+
		"made with CreatePseudoConsole rather than a pseudo-terminal opened.",
		dir, missing, len(Sessions), runtime.GOOS, runtime.GOOS)
}

// TestErrorTextMatchesTheName checks the rule the rline package follows: an
// error's text is its own name with the Err prefix taken off. Context belongs
// in the wrapping where the error is returned, not in the sentinel.
func TestErrorTextMatchesTheName(t *testing.T) {
	t.Parallel()
	for name, err := range map[string]Error{
		"ErrNoSteps":     ErrNoSteps,
		"ErrUnsupported": ErrUnsupported,
	} {
		want := strings.ToLower(strings.Join(splitWords(strings.TrimPrefix(name, "Err")), " "))
		if string(err) != want {
			t.Errorf("%s reads %q, want %q", name, string(err), want)
		}
	}
}

// splitWords cuts a name where each word starts. "NoSteps" becomes
// ["No", "Steps"].
func splitWords(name string) []string {
	var words []string
	start := 0
	for i, r := range name {
		if i > 0 && unicode.IsUpper(r) {
			words = append(words, name[start:i])
			start = i
		}
	}
	return append(words, name[start:])
}
