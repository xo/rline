//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// macOS.
//
// Everything here goes through osascript, because the things this harness
// needs — focus a window, type into it, resize it — are exactly what the
// Accessibility API exposes to AppleScript and nothing else offers without
// cgo. The cost is that the person running this has to grant Accessibility
// permission to the terminal they run it from, once, and macOS will ask.
//
// NOT YET RUN ON A MAC. Written from the documented behaviour of osascript
// and screencapture. Whoever runs it first should expect the permission
// prompt and should check the key names in typeKey against what System Events
// actually produces, because that mapping is the part most likely to be
// wrong.

const isWindows = false

// platformReady reports whether this Mac can run the harness.
func platformReady() error {
	if _, err := exec.LookPath("osascript"); err != nil {
		return fmt.Errorf("osascript is missing, which should not happen on macOS: %w", err)
	}
	if _, err := exec.LookPath("screencapture"); err != nil {
		return fmt.Errorf("screencapture is missing: %w", err)
	}
	// Accessibility permission cannot be checked without asking for it, and
	// asking is what produces the prompt, so this does not try. A run without
	// it fails at the first keystroke with an osascript error, which names
	// the problem clearly enough.
	return nil
}

// platformTerminals is the macOS matrix.
//
// Terminal.app and iTerm2 are here because they are what people actually use
// on a Mac, and they are the two that lag: Terminal.app uses older width
// tables and ignores much of DECSCUSR, which is where a port will disagree
// with the screen.
func platformTerminals() []Terminal {
	return []Terminal{
		{
			Name: "wezterm", Bin: "wezterm", Engine: "wezterm",
			Args: func(c, r int, cmd []string) []string {
				return append([]string{"wezterm",
					"--config", fmt.Sprintf("initial_cols=%d", c),
					"--config", fmt.Sprintf("initial_rows=%d", r),
					"--config", "cursor_blink_rate=0",
					"start", "--class", appID, "--"}, cmd...)
			},
			GetText: weztermGetText,
		},
		{
			Name: "ghostty", Bin: "ghostty", Engine: "ghostty",
			Args: func(c, r int, cmd []string) []string {
				return append([]string{"ghostty",
					fmt.Sprintf("--window-width=%d", c),
					fmt.Sprintf("--window-height=%d", r),
					"--cursor-style-blink=false",
					"-e"}, cmd...)
			},
		},
		{
			Name: "iterm2", Bin: "osascript", Engine: "iTerm2",
			Args: func(c, r int, cmd []string) []string {
				// iTerm2 has no command line that opens a window running a
				// program, so it is driven through its own scripting instead.
				return osascriptArgs(fmt.Sprintf(`
					tell application "iTerm2"
						create window with default profile
						tell current session of current window
							set columns to %d
							set rows to %d
							write text "exec %s"
						end tell
					end tell`, c, r, shellJoin(cmd)))
			},
		},
		{
			Name: "terminal-app", Bin: "osascript", Engine: "Terminal.app",
			Args: func(c, r int, cmd []string) []string {
				return osascriptArgs(fmt.Sprintf(`
					tell application "Terminal"
						do script "exec %s"
						set number of columns of front window to %d
						set number of rows of front window to %d
						activate
					end tell`, shellJoin(cmd), c, r))
			},
		},
	}
}

// appID is the window class wezterm is told to use. The two AppleScript
// terminals are found by application name instead, because they do not take
// one.
const appID = "rline-uitest"

// frontApp is the application whose window the harness is driving. It is set
// when a terminal is opened, because on macOS the window is addressed by the
// name of the application that owns it rather than by a class.
var frontApp = "WezTerm"

// focusWindow brings the terminal to the front and waits for it to settle.
func focusWindow(t Terminal) error {
	frontApp = appName(t)
	if err := osa(fmt.Sprintf(`tell application "%s" to activate`, frontApp)); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return nil
}

// appName is what AppleScript calls each terminal.
func appName(t Terminal) string {
	switch t.Name {
	case "iterm2":
		return "iTerm2"
	case "terminal-app":
		return "Terminal"
	case "ghostty":
		return "Ghostty"
	default:
		return "WezTerm"
	}
}

// ensureFocused refuses to type unless the terminal being driven is frontmost.
//
// The same rule as the other platforms and for the same reason: synthetic
// keys go to whatever has focus, so a harness that does not check types into
// whatever the person is using.
func ensureFocused() error {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to get name of first application process whose frontmost is true`).Output()
	if err != nil {
		return fmt.Errorf("cannot tell which application is frontmost, so refusing to type: %w", err)
	}
	got := strings.TrimSpace(string(out))
	if !strings.EqualFold(got, frontApp) {
		return fmt.Errorf("the frontmost application is %q rather than %q, so refusing to type into it", got, frontApp)
	}
	return nil
}

// typeText types literal text.
func typeText(s string) error {
	if err := ensureFocused(); err != nil {
		return err
	}
	return osa(fmt.Sprintf(`tell application "System Events" to keystroke %s`, quote(s)))
}

// typeKey presses a named key.
//
// The session names keys the way X11 does, because that is what the Linux
// side needs, so they are translated here rather than in the sessions. A key
// that is not in the table is passed through as a keystroke, which is right
// for letters and wrong for anything else, so an unknown name is an error
// instead.
func typeKey(name string) error {
	if err := ensureFocused(); err != nil {
		return err
	}
	parts := strings.Split(name, "+")
	key := parts[len(parts)-1]
	var mods []string
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case "ctrl":
			mods = append(mods, "control down")
		case "alt":
			mods = append(mods, "option down")
		case "shift":
			mods = append(mods, "shift down")
		default:
			return fmt.Errorf("unknown modifier %q", m)
		}
	}
	using := ""
	if len(mods) != 0 {
		using = " using {" + strings.Join(mods, ", ") + "}"
	}
	// Key codes rather than names, because System Events takes a number for
	// anything that is not a character.
	codes := map[string]int{
		"Return": 36, "Tab": 48, "Escape": 53, "BackSpace": 51, "Delete": 117,
		"Left": 123, "Right": 124, "Down": 125, "Up": 126,
		"Home": 115, "End": 119, "Prior": 116, "Next": 121,
	}
	if code, ok := codes[key]; ok {
		return osa(fmt.Sprintf(`tell application "System Events" to key code %d%s`, code, using))
	}
	if len([]rune(key)) == 1 {
		return osa(fmt.Sprintf(`tell application "System Events" to keystroke %s%s`, quote(key), using))
	}
	return fmt.Errorf("no macOS key code for %q", key)
}

// resizeWindow resizes the terminal to cols by rows.
//
// Terminal.app and iTerm2 take the size in characters, which is what is
// wanted. The others are resized in pixels, by measuring the window and
// scaling it, the same way the Linux side does.
func resizeWindow(cols, rows int) error {
	switch frontApp {
	case "Terminal":
		return osa(fmt.Sprintf(`tell application "Terminal"
			set number of columns of front window to %d
			set number of rows of front window to %d
		end tell`, cols, rows))
	case "iTerm2":
		return osa(fmt.Sprintf(`tell current session of current window of application "iTerm2"
			set columns to %d
			set rows to %d
		end tell`, cols, rows))
	}
	b, err := windowBounds()
	if err != nil {
		return err
	}
	w := b[2] * cols / Cols
	h := b[3] * rows / Rows
	return osa(fmt.Sprintf(`tell application "System Events" to tell process "%s" to set size of front window to {%d, %d}`,
		frontApp, w, h))
}

// windowBounds returns the front window as x, y, width, height.
func windowBounds() ([4]int, error) {
	out, err := exec.Command("osascript", "-e", fmt.Sprintf(
		`tell application "System Events" to tell process "%s"
			set p to position of front window
			set s to size of front window
			return (item 1 of p as text) & "," & (item 2 of p as text) & "," & (item 1 of s as text) & "," & (item 2 of s as text)
		end tell`, frontApp)).Output()
	if err != nil {
		return [4]int{}, err
	}
	var b [4]int
	for i, f := range strings.Split(strings.TrimSpace(string(out)), ",") {
		if i > 3 {
			break
		}
		b[i], _ = strconv.Atoi(strings.TrimSpace(f))
	}
	return b, nil
}

// screenshot photographs the terminal window.
//
// Cropped to the window rather than the screen, for the same reasons as the
// other platforms: a picture of somebody's desktop is hard to compare and
// twenty times the size.
func screenshot(_ Terminal, path string) error {
	b, err := windowBounds()
	if err != nil {
		// -o leaves out the window shadow, which otherwise varies with
		// what is behind the window.
		return runTool("screencapture", "-o", "-x", path)
	}
	return runTool("screencapture", "-o", "-x",
		"-R", fmt.Sprintf("%d,%d,%d,%d", b[0], b[1], b[2], b[3]), path)
}

// weztermGetText asks wezterm for its screen.
func weztermGetText() (string, error) {
	out, err := exec.Command("wezterm", "cli", "get-text", "--escapes").Output()
	return string(out), err
}

// osa runs one AppleScript.
func osa(script string) error { return runTool("osascript", "-e", script) }

// osascriptArgs returns the command line that runs a script.
func osascriptArgs(script string) []string { return []string{"osascript", "-e", script} }

// quote returns an AppleScript string literal.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// shellJoin quotes a command for a shell inside an AppleScript string.
func shellJoin(cmd []string) string {
	var parts []string
	for _, c := range cmd {
		parts = append(parts, strings.ReplaceAll(c, `"`, `\\\"`))
	}
	return strings.Join(parts, " ")
}

// runTool executes a helper and reports what it said when it fails.
func runTool(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
