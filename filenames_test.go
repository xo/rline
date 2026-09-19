package rline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const (
	// filenamesCorpusPath holds what the C file name completion did.
	filenamesCorpusPath = "testdata/filenames.txt"

	// filenamesProbePath is where tools/build-probe-filenames.sh puts the
	// probe.
	filenamesProbePath = ".build/probe-filenames"
)

// makeProbeTree builds the same tree the probe builds, in dir.
//
// Everything here can be made without special privileges. A block device and
// a character device cannot, so those two file types are left to the unit
// test below, which finds existing ones.
func makeProbeTree(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{
		"apple.txt", "apricot.md", "cherry.TXT", "date.c", "UPPER.txt",
		".hidden", "no-ext",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "exec.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing exec.sh: %v", err)
	}
	for name, mode := range map[string]os.FileMode{
		"banana":  0o755,
		"sub dir": 0o755,
		"sticky":  0o755 | os.ModeSticky,
	} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatalf("making %s: %v", name, err)
		}
		if err := os.Chmod(filepath.Join(dir, name), mode); err != nil {
			t.Fatalf("setting the mode of %s: %v", name, err)
		}
	}
	inner := filepath.Join(dir, "banana", "inner.txt")
	if err := os.WriteFile(inner, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing inner.txt: %v", err)
	}
	if err := os.Symlink("apple.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatalf("making the link: %v", err)
	}
}

// TestFilenamesPort replays every recorded case and checks that the Go port
// does the same. The cases that read the file system run against the same
// tree the probe built.
func TestFilenamesPort(t *testing.T) {
	if *update {
		regenerateFilenames(t)
	}
	b, err := os.ReadFile(filenamesCorpusPath)
	if err != nil {
		t.Fatalf("reading the corpus: %v (run tools/build-probe-filenames.sh, then go test -update)", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) < 300 {
		t.Fatalf("the corpus holds %d lines, which is too few to prove anything", len(lines))
	}

	// The file cases complete against the working directory, so the test
	// moves into a copy of the tree the probe used. This cannot run beside
	// another test that also changes directory, so nothing here is parallel.
	dir := t.TempDir()
	makeProbeTree(t, dir)
	t.Chdir(dir)

	// The colour settings come from the environment, so start from a known
	// one. The corpus records what each case sets.
	for _, name := range []string{"CLICOLOR", "LS_COLORS", "LSCOLORS"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("clearing %s: %v", name, err)
		}
	}

	counts := make(map[string]int, 8)
	bad := 0
	for i, line := range lines {
		kind, diff := checkFilenameLine(t, line)
		counts[kind]++
		if diff == "" {
			continue
		}
		bad++
		if bad <= maxReported {
			t.Errorf("%s:%d: %s\n  %s", filenamesCorpusPath, i+1, diff, line)
		}
	}
	if bad > maxReported {
		t.Errorf("%d differences in total, %d shown", bad, maxReported)
	}
	for _, kind := range []string{"extmatch", "colorize", "files"} {
		if counts[kind] == 0 {
			t.Errorf("the corpus holds no %s cases", kind)
		}
	}
	t.Logf("checked %d cases: %v", len(lines), counts)
}

// checkFilenameLine checks one recorded case.
func checkFilenameLine(t *testing.T, line string) (string, string) {
	t.Helper()
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", "empty line"
	}
	p := &fieldReader{t: t, f: f, i: 1}
	switch f[0] {
	case "extmatch":
		name, extensions, want := p.str(), p.str(), p.flag()
		if got := matchExtension(name, extensions); got != want {
			return f[0], fmt.Sprintf("matchExtension(%q, %q) = %v, want %v",
				name, extensions, got, want)
		}
	case "colorize":
		clicolor, gnu, bsd := p.envStr(), p.envStr(), p.envStr()
		ft := fileType(p.num())
		name, ext := p.str(), p.nullableStr()
		hasExt := f[6] != "!"
		dirSep := mustHex(t, p.next())[0]
		noColor, want := p.flag(), p.next()
		setEnvOrUnset(t, "CLICOLOR", clicolor)
		setEnvOrUnset(t, "LS_COLORS", gnu)
		setEnvOrUnset(t, "LSCOLORS", bsd)
		got := colorizeEntry(noColor, ft, name, ext, hasExt, dirSep)
		if h := hexOrDash([]byte(got)); h != want {
			return f[0], fmt.Sprintf("colorizeEntry gave %s, want %s\n  got  %q", h, want, got)
		}
	case "files":
		return f[0], checkFilesCase(t, p)
	default:
		return f[0], "unknown kind of case"
	}
	return f[0], ""
}

// checkFilesCase checks one recorded completion against the tree.
func checkFilesCase(t *testing.T, p *fieldReader) string {
	t.Helper()
	prefix := p.str()
	dirSep := mustHex(t, p.next())[0]
	roots, extensions := p.str(), p.str()
	noColor := p.flag()

	// The probe turns colouring off for these, so the display stays plain.
	setEnvOrUnset(t, "CLICOLOR", "")
	setEnvOrUnset(t, "LS_COLORS", "")
	setEnvOrUnset(t, "LSCOLORS", "")

	c := &completions{}
	c.setCompleter(func(cenv *Completion, word string) {
		completeFilename(cenv, word, noColor, dirSep, roots, extensions)
	}, nil)
	c.completerMax = 200
	cenv := &Completion{input: prefix, cursor: len(prefix)}
	cenv.add = func(replacement, display, help string, before, after int) bool {
		return c.add(replacement, display, help, before, after)
	}
	c.completer(cenv, prefix)

	got := make([]string, 0, c.count())
	for _, cm := range c.items {
		got = append(got, fmt.Sprintf("%s:%s:%d:%d",
			hexOrDash([]byte(cm.replacement)), hexOrDash([]byte(cm.display)),
			cm.deleteBefore, cm.deleteAfter))
	}
	// The order is whatever the directory gives, which is not the same on
	// every file system, so both sides are sorted. The menu sorts before it
	// shows, so nothing depends on the order.
	slices.Sort(got)
	want := p.rawList()
	slices.Sort(want)
	if len(got) != len(want) {
		return fmt.Sprintf("completing %q offered %d, want %d\n  got  %v\n  want %v",
			prefix, len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			return fmt.Sprintf("completing %q gave %s at %d, want %s", prefix, got[i], i, want[i])
		}
	}
	return ""
}

// envStr reads a field that names an environment variable's value, where a
// null pointer means the variable was not set at all.
func (p *fieldReader) envStr() string {
	p.t.Helper()
	s := p.next()
	if s == "!" {
		return "\x00unset"
	}
	if s == "-" {
		return ""
	}
	return string(mustHex(p.t, s))
}

// setEnvOrUnset sets a variable, or removes it when the recorded value says
// it was not set.
func setEnvOrUnset(t *testing.T, name, value string) {
	t.Helper()
	if value == "\x00unset" {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("clearing %s: %v", name, err)
		}
		return
	}
	t.Setenv(name, value)
}

// regenerateFilenames runs the C probe and writes the corpus.
func regenerateFilenames(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filenamesProbePath); err != nil {
		t.Fatalf("no probe at %s: run tools/build-probe-filenames.sh", filenamesProbePath)
	}
	out, err := exec.Command(filenamesProbePath).Output()
	if err != nil {
		t.Fatalf("running the probe: %v", err)
	}
	if err := os.WriteFile(filenamesCorpusPath, out, 0o644); err != nil {
		t.Fatalf("writing %s: %v", filenamesCorpusPath, err)
	}
	t.Logf("wrote %s: %d bytes", filenamesCorpusPath, len(out))
}

// TestTypeOfRealEntries checks the mapping from a real directory entry to
// the type that decides its colour. Only a real file system can answer this,
// so the corpus cannot: it passes the type in rather than working it out.
//
// A block device and a character device cannot be made without privileges,
// so those two use ones the system already has, and are skipped when they
// are not there.
func TestTypeOfRealEntries(t *testing.T) {
	dir := t.TempDir()
	at := func(name string) string { return filepath.Join(dir, name) }

	mustWrite := func(name string, mode os.FileMode) string {
		t.Helper()
		path := at(name)
		if err := os.WriteFile(path, []byte("x"), mode); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("setting the mode of %s: %v", name, err)
		}
		return path
	}
	mustDir := func(name string, mode os.FileMode) string {
		t.Helper()
		path := at(name)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatalf("making %s: %v", name, err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("setting the mode of %s: %v", name, err)
		}
		return path
	}

	link := at("link")
	if err := os.Symlink("plain", link); err != nil {
		t.Skipf("this system will not make a symbolic link: %v", err)
	}

	tests := []struct {
		name string
		path string
		want fileType
		// fromPermissions says the answer is worked out from the permission
		// bits. Windows does not keep them: os.Chmod there maps only the
		// owner write bit onto the read-only attribute and drops the rest,
		// so a file always reads back as 0666 and a directory as 0777.
		// There is nothing to classify against, so these are skipped rather
		// than given different expected values.
		//
		// One of them would otherwise pass. A directory the group may write
		// expects the same answer that every directory gives on Windows, so
		// it would be green for the wrong reason and would stay green
		// through a real regression. That is worse than a skip.
		fromPermissions bool
	}{
		{"a symbolic link", link, ftSym, false},
		{"an ordinary file", mustWrite("plain", 0o644), ftDefault, false},
		{"a missing path", at("nothing-here"), ftDefault, false},
		{"a file anyone may run", mustWrite("runnable", 0o755), ftExe, true},
		{"a directory", mustDir("plaindir", 0o755), ftDir, true},
		{"a sticky directory", mustDir("stickydir", 0o755|os.ModeSticky), ftDirSticky, true},
		{"a directory the group may write", mustDir("groupdir", 0o775), ftDirOtherWritable, true},
		{"a sticky directory the group may write",
			mustDir("groupsticky", 0o775|os.ModeSticky), ftDirOtherWritableSticky, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.fromPermissions && runtime.GOOS == "windows" {
				t.Skip("this system does not keep the permission bits, so there is nothing to classify")
			}
			if got := typeOf(test.path); got != test.want {
				t.Errorf("typeOf(%s) = %d, want %d", test.name, got, test.want)
			}
		})
	}

	// A character device that every system has.
	t.Run("a character device", func(t *testing.T) {
		if _, err := os.Stat("/dev/null"); err != nil {
			t.Skip("there is no /dev/null here")
		}
		if got := typeOf("/dev/null"); got != ftChar {
			t.Errorf("typeOf(/dev/null) = %d, want %d", got, ftChar)
		}
	})
}

// TestTypeOfSetuidDirectory is separate because a file system can refuse to
// set these bits, and that is not a failure of the port.
func TestTypeOfSetuidDirectory(t *testing.T) {
	for _, test := range []struct {
		name string
		mode os.FileMode
		want fileType
	}{
		{"set user", 0o755 | os.ModeSetuid, ftSetuid},
		{"set group", 0o755 | os.ModeSetgid, ftSetgid},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "d")
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatalf("making the directory: %v", err)
			}
			if err := os.Chmod(path, test.mode); err != nil {
				t.Skipf("this file system will not take the mode: %v", err)
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("reading it back: %v", err)
			}
			if info.Mode()&(os.ModeSetuid|os.ModeSetgid) == 0 {
				t.Skip("the file system dropped the bit")
			}
			if got := typeOf(path); got != test.want {
				t.Errorf("typeOf = %d, want %d", got, test.want)
			}
		})
	}
}

// TestIsDirFollowsALink checks that a link to a directory completes as a
// directory, which is why the type and the directory test use different
// calls: one follows a link and the other does not.
func TestIsDirFollowsALink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("making the target: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink("target", link); err != nil {
		t.Skipf("this system will not make a symbolic link: %v", err)
	}
	if !isDir(link) {
		t.Error("a link to a directory did not count as one")
	}
	if got := typeOf(link); got != ftSym {
		t.Errorf("the type of a link to a directory is %d, want %d", got, ftSym)
	}
}
