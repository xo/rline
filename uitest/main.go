// Command uitest opens a real terminal emulator, types into it as a person
// would, and records what was sent and what was drawn.
//
// It does not run in CI and is not meant to. See README.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	var (
		list     = flag.Bool("list", false, "list the terminals this platform knows and whether each is installed")
		termName = flag.String("terminal", "", "run one terminal rather than every installed one")
		sessName = flag.String("session", "", "run one session rather than all of them")
		update   = flag.Bool("update", false, "write the current byte logs as the blessed ones")
		outDir   = flag.String("out", "uitest/out", "where to write this run's artifacts")
		keep     = flag.Bool("keep", false, "leave the terminal open after the last key, for watching a run")
		visible  = flag.Bool("visible", false, "run on your real desktop instead of a private compositor; types wherever focus is, so do not use the machine while it runs")
		video    = flag.Bool("video", false, "record the run to out/session.mp4")
		vnc      = flag.Bool("vnc", false, "serve the private compositor over VNC on 127.0.0.1:5900 and wait 10s for you to connect")
	)
	flag.Parse()

	if *list {
		listTerminals()
		return
	}
	if err := run(*termName, *sessName, *outDir, *update, *keep, *visible, *video, *vnc); err != nil {
		fmt.Fprintf(os.Stderr, "uitest: %v\n", err)
		os.Exit(1)
	}
}

// listTerminals prints the matrix for this platform. A terminal that is not
// installed is named anyway, because the useful question is usually "what am
// I not covering here" rather than "what can I run".
func listTerminals() {
	if err := platformReady(); err != nil {
		fmt.Printf("this platform cannot run uitest: %v\n\n", err)
	}
	ts := terminals()
	sort.Slice(ts, func(i, j int) bool { return ts[i].Name < ts[j].Name })
	for _, t := range ts {
		state := "not installed"
		if t.found() {
			state = "installed"
		}
		grid := ""
		if t.GetText != nil {
			grid = "  (can dump its screen)"
		}
		fmt.Printf("%-18s %-14s %-14s%s\n", t.Name, t.Engine, state, grid)
	}
}

// run builds the demo and drives every selected terminal and session.
func run(termName, sessName, outDir string, update, keep, visible, video, vnc bool) error {
	if err := platformReady(); err != nil {
		return err
	}
	bin, err := buildDemo()
	if err != nil {
		return err
	}
	d, err := startDisplay(visible, video, vnc, outDir)
	if err != nil {
		return err
	}
	defer d.stop()

	var chosen []Terminal
	switch {
	case termName != "":
		t, ok := lookup(termName)
		if !ok {
			return fmt.Errorf("no terminal named %q; try -list", termName)
		}
		if !t.found() {
			return fmt.Errorf("%s is not installed", t.Name)
		}
		chosen = []Terminal{t}
	default:
		for _, t := range terminals() {
			if t.found() {
				chosen = append(chosen, t)
			}
		}
		if len(chosen) == 0 {
			return errors.New("no terminal from the matrix is installed; try -list")
		}
	}

	var failed []string
	for _, t := range chosen {
		for _, s := range sessions {
			if sessName != "" && s.Name != sessName {
				continue
			}
			fmt.Printf("== %s / %s\n", t.Name, s.Name)
			res, err := runOne(bin, t, s, outDir, keep)
			if err != nil {
				fmt.Printf("   failed: %v\n", err)
				failed = append(failed, t.Name+"/"+s.Name)
				continue
			}
			if err := compare(t, s, res, update); err != nil {
				fmt.Printf("   %v\n", err)
				failed = append(failed, t.Name+"/"+s.Name)
				continue
			}
			fmt.Printf("   ok   log %s\n   ok   shot %s\n", res.logPath, res.shotPath)
			if msg := terminalSaid(res.errPath); msg != "" {
				fmt.Printf("   note %s also said: %s\n", t.Name, msg)
			}
		}
	}
	if len(failed) != 0 {
		return fmt.Errorf("%d run(s) failed: %s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

// result is what one run produced.
type result struct {
	logPath  string
	shotPath string
	gridPath string
	errPath  string
}

// buildDemo builds the example program, which is what gets driven. It is
// built rather than go run so that the terminal's child is the demo itself
// and not a toolchain that compiles for a second and then execs.
func buildDemo() (string, error) {
	dir, err := os.MkdirTemp("", "uitest-bin")
	if err != nil {
		return "", fmt.Errorf("making a directory for the demo: %w", err)
	}
	bin := filepath.Join(dir, "rline-demo")
	if isWindows {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./example")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("building the demo: %w", err)
	}
	return bin, nil
}

// runOne opens the terminal, types the session into it, and photographs it.
func runOne(bin string, t Terminal, s Session, outDir string, keep bool) (result, error) {
	var res result
	dir := filepath.Join(outDir, t.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, fmt.Errorf("making the artifact directory: %w", err)
	}
	res.logPath = filepath.Join(dir, s.Name+".log")
	res.shotPath = filepath.Join(dir, s.Name+".png")
	_ = os.Remove(res.logPath)

	// The path handed to the program has to be absolute. A terminal decides
	// for itself what working directory to give its child, and wezterm gives
	// it the home directory rather than ours: with a relative path the demo
	// wrote its log there, this harness watched a file that never appeared,
	// and the run failed saying the program had never started.
	absLog, err := filepath.Abs(res.logPath)
	if err != nil {
		return res, fmt.Errorf("making a temporary home: %w", err)
	}

	// A home of its own, so that a history file from an earlier run cannot
	// change what is drawn. internal/capture does the same and for the same
	// reason.
	home, err := os.MkdirTemp("", "uitest-home")
	if err != nil {
		return res, fmt.Errorf("creating the terminal's output file: %w", err)
	}
	defer func() { _ = os.RemoveAll(home) }()

	// The program sets the window title before it starts, and the harness
	// finds its window by that as well as by app id.
	//
	// Not every terminal takes an app id. GTK ignores one that is not
	// reverse-DNS, and gnome-terminal ignores it regardless because the
	// window belongs to a server process rather than to the command that was
	// run. A title set by the program itself works everywhere, because
	// setting one is a terminal feature rather than a launcher flag — and it
	// has to be unique, since matching a shared app id like
	// "org.gnome.Terminal" would find the person's own windows and type into
	// one of them.
	inner := []string{"/bin/sh", "-c",
		fmt.Sprintf("printf '\033]0;%s\007'; exec %q -log %q", appID, bin, absLog)}
	argv := t.Args(Cols, Rows, inner)
	cmd := exec.Command(argv[0], argv[1:]...)
	// Copied rather than appended to. current.env is shared by every run,
	// and append would write into its backing array when it has room, so the
	// second terminal could inherit the first one's HOME.
	cmd.Env = append(append([]string(nil), current.env...), "HOME="+home, "TERM=xterm-256color")

	// Whatever the terminal itself says is kept. It goes to a window that
	// closes when the run ends, so without this a complaint from the
	// emulator — a bad line in its own config, a font it cannot find — is
	// visible for a moment and then gone, and the run looks unexplained.
	res.errPath = filepath.Join(dir, s.Name+".stderr")
	errFile, err := os.Create(res.errPath)
	if err != nil {
		return res, fmt.Errorf("waiting for the program's first byte: %w", err)
	}
	defer errFile.Close()
	cmd.Stdout, cmd.Stderr = errFile, errFile

	if err := cmd.Start(); err != nil {
		return res, fmt.Errorf("starting %s: %w", t.Name, err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	// The log file is the readiness signal and the quiescence signal both.
	// The demo writes its prompt through rline, which logs every byte it
	// writes, so the file growing means the program is drawing and the file
	// going still means it has stopped. That needs nothing from the emulator,
	// which is what lets the same rule work for all of them.
	if err := waitForFirstByte(res.logPath, DefaultStart); err != nil {
		return res, err
	}
	if err := focusWindow(t); err != nil {
		return res, err
	}

	quiet := s.Quiet
	if quiet == 0 {
		quiet = DefaultQuiet
	}
	// A picture after every step rather than only at the end.
	//
	// The end state is what a golden log pins, but it is not what a person
	// needs to see: a cursor that lands in the wrong column for one step and
	// is corrected by the next leaves no trace in the final frame. The steps
	// are numbered so they sort, and they are what -video is for as well.
	steps := filepath.Join(dir, s.Name)
	if err := os.MkdirAll(steps, 0o755); err != nil {
		return res, fmt.Errorf("making the step directory: %w", err)
	}
	for i, step := range s.Keys {
		if err := sendStep(step); err != nil {
			return res, err
		}
		w := step.Wait
		if w == 0 {
			w = quiet
		}
		if err := waitForQuiet(res.logPath, w); err != nil {
			return res, err
		}
		_ = screenshot(t, filepath.Join(steps, fmt.Sprintf("%02d-%s.png", i+1, stepName(step))))
	}

	if t.GetText != nil {
		if text, err := t.GetText(); err == nil {
			res.gridPath = filepath.Join(dir, s.Name+".grid")
			_ = os.WriteFile(res.gridPath, []byte(text), 0o644)
		}
	}
	if err := screenshot(t, res.shotPath); err != nil {
		return res, fmt.Errorf("photographing the window: %w", err)
	}
	if keep {
		fmt.Printf("   (left open; press enter here to close)\n")
		_, _ = fmt.Scanln()
	}
	return res, nil
}

// sendStep types one step.
func sendStep(s Step) error {
	switch {
	case s.Cols != 0 && s.Rows != 0:
		return resizeWindow(s.Cols, s.Rows)
	case s.Key != "":
		return typeKey(s.Key)
	case s.Text != "":
		return typeText(s.Text)
	}
	return nil
}

// waitForFirstByte waits for the program to write anything at all.
//
// Reading until quiet cannot tell a program that has finished drawing from
// one that has not started, and a terminal emulator takes long enough to open
// a window that the difference is not theoretical. Keys typed before the
// program is reading go to the terminal, which echoes them, and then the
// program turns raw mode on and throws them away.
func waitForFirstByte(path string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("the program wrote nothing within %v, so it never started", limit)
}

// waitForQuiet waits until the log has not grown for quiet.
func waitForQuiet(path string, quiet time.Duration) error {
	var last int64 = -1
	still := time.Now()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		fi, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("reading the log while waiting for it to settle: %w", err)
		}
		if fi.Size() != last {
			last, still = fi.Size(), time.Now()
		} else if time.Since(still) >= quiet {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("the program never stopped writing")
}

// terminalSaid returns the first line the terminal wrote, if it wrote any.
//
// Reported even on a run that passed, because a terminal that is complaining
// about its own configuration is drawing something other than what a clean
// one would, and the screenshot is then a picture of that.
func terminalSaid(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return ""
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	return first
}

// stepName is a short label for a step, for a file name a person can read.
func stepName(s Step) string {
	switch {
	case s.Cols != 0:
		return fmt.Sprintf("resize-%dx%d", s.Cols, s.Rows)
	case s.Key != "":
		return strings.ReplaceAll(s.Key, "+", "-")
	default:
		t := s.Text
		if len(t) > 12 {
			t = t[:12]
		}
		return "type-" + strings.Map(func(r rune) rune {
			if r == ' ' || r == '/' || r == '\\' {
				return '_'
			}
			return r
		}, t)
	}
}
