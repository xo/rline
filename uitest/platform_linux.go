//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Linux, on both X11 and Wayland.
//
// The two differ only in how a key is injected and how the screen is
// photographed. Wayland does not let one client type into another, so the
// tools are compositor-side rather than X11's XTEST: wtype talks the virtual
// keyboard protocol that wlroots compositors implement, and grim asks the
// compositor for the picture. On X11 it is xdotool and import, which work
// anywhere an X server does.

const isWindows = false

// wayland reports whether this is a Wayland session. DISPLAY is often set
// under Wayland as well, because XWayland is running, so the presence of
// WAYLAND_DISPLAY is the question that actually distinguishes them.
func wayland() bool { return os.Getenv("WAYLAND_DISPLAY") != "" }

// platformReady reports whether the tools this session needs are installed.
func platformReady() error {
	if wayland() {
		var missing []string
		for _, bin := range []string{"wtype", "grim"} {
			if _, err := exec.LookPath(bin); err != nil {
				missing = append(missing, bin)
			}
		}
		if len(missing) != 0 {
			return fmt.Errorf("on Wayland this needs %s; wtype works on wlroots compositors "+
				"such as sway and hyprland, and GNOME and KDE need the ydotool route instead",
				strings.Join(missing, " and "))
		}
		ensureFloating()
		return nil
	}
	if os.Getenv("DISPLAY") == "" {
		return errors.New("neither WAYLAND_DISPLAY nor DISPLAY is set, so there is no desktop to open a terminal on")
	}
	for _, bin := range []string{"xdotool", "import"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("on X11 this needs %s", bin)
		}
	}
	return nil
}

// platformTerminals is the Linux matrix. Engines rather than brands: several
// popular terminals are VTE with a different menu bar, and testing two of
// them tests VTE twice.
func platformTerminals() []Terminal {
	return []Terminal{
		{
			Name: "foot", Bin: "foot", Engine: "foot",
			Args: func(c, r int, cmd []string) []string {
				return append([]string{"foot", "--app-id=" + appID,
					fmt.Sprintf("--window-size-chars=%dx%d", c, r), "--"}, cmd...)
			},
		},
		{
			Name: "alacritty", Bin: "alacritty", Engine: "alacritty",
			Args: func(c, r int, cmd []string) []string {
				return append([]string{"alacritty", "--class", appID,
					"-o", fmt.Sprintf("window.dimensions.columns=%d", c),
					"-o", fmt.Sprintf("window.dimensions.lines=%d", r),
					"-e"}, cmd...)
			},
		},
		{
			Name: "kitty", Bin: "kitty", Engine: "kitty",
			Args: func(c, r int, cmd []string) []string {
				return append([]string{"kitty", "--app-id=" + appID,
					"-o", fmt.Sprintf("initial_window_width=%dc", c),
					"-o", fmt.Sprintf("initial_window_height=%dc", r),
					"-o", "remember_window_size=no",
					"-o", "cursor_blink_interval=0"}, cmd...)
			},
			GetText: kittyGetText,
		},
		{
			Name: "ghostty", Bin: "ghostty", Engine: "ghostty",
			Args: func(c, r int, cmd []string) []string {
				return append([]string{"ghostty", "--class=" + appID,
					fmt.Sprintf("--window-width=%d", c),
					fmt.Sprintf("--window-height=%d", r),
					"--cursor-style-blink=false",
					"-e"}, cmd...)
			},
		},
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
			Name: "gnome-terminal", Bin: "dbus-run-session", Engine: "VTE",
			Args: func(c, r int, cmd []string) []string {
				// VTE is the engine most Linux desktops actually run, and
				// every other terminal in this list is a modern
				// GPU-accelerated outlier. --wait is needed because
				// gnome-terminal otherwise hands off to a server and returns
				// at once, and then nothing is left to kill.
				// Through a private message bus, which is not optional.
				// gnome-terminal does not open a window itself: it asks
				// gnome-terminal-server over D-Bus, and if the person's own
				// session bus is reachable the server already running on
				// their desktop takes the request and opens the window
				// there, outside the private compositor and on the desktop
				// this harness exists to stay away from. Measured: six
				// windows appeared on Ken's desktop from one run before this
				// was added, and none after.
				//
				// A private bus has no server on it, so one starts inside
				// this environment and the window opens where it should.
				return append([]string{"dbus-run-session", "--",
					"gnome-terminal", "--wait", "--class=" + appID,
					fmt.Sprintf("--geometry=%dx%d", c, r), "--"}, cmd...)
			},
		},
		{
			Name: "xterm", Bin: "xterm", Engine: "xterm",
			Args: func(c, r int, cmd []string) []string {
				// The reference implementation. Everything else is measured
				// against what xterm does, so when two terminals disagree
				// this is the one that says which is unusual.
				return append([]string{"xterm", "-class", appID, "-geometry", fmt.Sprintf("%dx%d", c, r),
					"-bg", "black", "-fg", "white", "-e"}, cmd...)
			},
		},
	}
}

// appID is what every terminal is told to call itself, so that a compositor
// rule can find the window this harness opened and leave every other terminal
// on the desktop alone.
//
// Reverse-DNS with dots, because GTK requires an application ID to look like
// one and silently ignores anything else: ghostty and gnome-terminal were
// handed "rline-uitest" and kept their own ids, so the harness could not find
// its window and refused to type. A dotted id is honoured by every terminal
// in the matrix that takes one at all.
const appID = "com.github.xo.rline"

// ensureFloating asks a tiling compositor to leave this harness's windows at
// the size they asked for.
//
// This is not cosmetic. Under sway a terminal opens at the size it requested
// and is then tiled: measured here, foot asked for 80x24, got it, and was
// resized to 47x174 a second later. Every resize is a SIGWINCH and a redraw,
// so the byte log grew by a different number of repaints on each run — three
// runs of one session gave 86, 66 and 42 lines. A floating window keeps the
// size it asked for, and the log settles.
//
// The rule is added at run time rather than written into anyone's config, and
// it names only this harness's app id, so a person's own rules for their own
// terminals are untouched. On a compositor that is not sway this does
// nothing, and the run still works; it is the determinism that suffers, which
// is why the size is checked rather than assumed.
func ensureFloating() {
	if _, err := exec.LookPath("swaymsg"); err != nil {
		return
	}
	_ = runTool("swaymsg", "for_window [app_id=\""+appID+"\"] floating enable")
}

// focusWindow waits for the window to appear and take focus.
//
// Both display servers focus a newly opened window, so this waits rather than
// hunting. A find-and-raise would need a title, and the terminals disagree
// about what they put in one.
func focusWindow(t Terminal) error {
	// Ask for focus rather than hope for it. A newly mapped window does not
	// reliably get the keyboard: under focus-follows-mouse it goes to
	// whatever the pointer is over, so the first run of this typed nothing
	// and correctly refused, because the focused window was the browser the
	// pointer happened to be sitting on.
	if wayland() {
		// Put it somewhere it will not fight for the screen. With two
		// monitors one of them often has a fullscreen video on it, and a
		// window opened there either lands behind it or takes it over.
		// Neither is what somebody watching wants, and a window that is
		// covered cannot be photographed.
		if out, err := freeOutput(); err == nil && out != "" {
			_ = runTool("swaymsg", fmt.Sprintf("[app_id=\"%s\"] move to output %s", appID, out))
			_ = runTool("swaymsg", fmt.Sprintf("[title=\"%s\"] move to output %s", appID, out))
		}
		_ = runTool("swaymsg", fmt.Sprintf("[app_id=\"%s\"] focus", appID))
		_ = runTool("swaymsg", fmt.Sprintf("[title=\"%s\"] focus", appID))
	} else {
		_ = runTool("xdotool", "search", "--class", appID, "windowactivate", "--sync")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if id, err := focusedAppID(); err == nil && id == appID {
			// Settled, but the window may still be mapping. A key sent into
			// a surface that has focus and has not finished its first frame
			// is dropped by some compositors.
			time.Sleep(250 * time.Millisecond)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("%s opened but never took keyboard focus, so nothing can be typed into it "+
		"safely; is another window holding focus?", t.Name)
}

// ensureFocused refuses to type unless this harness's own window has focus.
//
// Injected keys go wherever the compositor is sending keyboard input, not to
// a window of our choosing, so a harness that types without checking types
// into whatever the person is using. That is not a hypothetical: an early run
// of this code typed "select 1;" into Ken's chat window while he was writing
// in it, because the terminal it had opened never took focus.
//
// So this fails closed. If the focused window is not ours the run stops with
// an error rather than typing somewhere else, and a lost run is a far smaller
// cost than keystrokes landing in someone's editor.
func ensureFocused() error {
	name, err := focusedAppID()
	if err != nil {
		// The compositor cannot be asked. Refuse rather than guess, for the
		// same reason: the failure this guards against is silent and the
		// damage is somebody else's.
		return fmt.Errorf("cannot tell which window has focus, so refusing to type: %w", err)
	}
	if name != appID {
		return fmt.Errorf("the focused window is %q rather than the harness's own %q, "+
			"so refusing to type into it", name, appID)
	}
	return nil
}

// focusedAppID returns the app id of the window with keyboard focus.
func focusedAppID() (string, error) {
	if wayland() {
		out, err := swayQuery()
		if err != nil {
			return "", err
		}
		var tree any
		if err := json.Unmarshal(out, &tree); err != nil {
			return "", fmt.Errorf("reading the compositor's tree: %w", err)
		}
		if id, ok := findFocusedAppID(tree); ok {
			return id, nil
		}
		return "", errors.New("no focused window in the compositor's tree")
	}
	cmd := exec.Command("xdotool", "getactivewindow", "getwindowclassname")
	cmd.Env = current.env
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("asking xdotool which window is active: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// findFocusedAppID walks the tree for the focused node.
func findFocusedAppID(n any) (string, bool) {
	m, ok := n.(map[string]any)
	if !ok {
		return "", false
	}
	if f, _ := m["focused"].(bool); f {
		id, _ := m["app_id"].(string)
		// A title the program set itself counts as the identifier too, for
		// the terminals that will not take an app id. It is checked first
		// because it is the more specific of the two.
		if name, _ := m["name"].(string); strings.Contains(name, appID) {
			return appID, true
		}
		if id == "" {
			// An X11 window under XWayland has no app_id; its class is
			// reported under window_properties instead.
			if wp, ok := m["window_properties"].(map[string]any); ok {
				id, _ = wp["class"].(string)
			}
		}
		return id, true
	}
	for _, key := range []string{"nodes", "floating_nodes"} {
		kids, _ := m[key].([]any)
		for _, kid := range kids {
			if id, ok := findFocusedAppID(kid); ok {
				return id, true
			}
		}
	}
	return "", false
}

// typeText types literal text.
func typeText(s string) error {
	if err := ensureFocused(); err != nil {
		return err
	}
	if wayland() {
		// -s waits before the first keystroke and -d between them, and both
		// are needed. wtype defaults to no delay at all, which sends the
		// whole string faster than the compositor applies the keymap it
		// uploaded: the first characters then arrive mis-mapped or out of
		// order. Measured — "select 1;" typed with the defaults reached the
		// program as "tes aselect 1;".
		return runTool("wtype", "-s", "60", "-d", "12", "--", s)
	}
	return runTool("xdotool", "type", "--clearmodifiers", "--delay", "12", "--", s)
}

// typeKey presses a named key, with modifiers joined by a plus.
//
// The names are the X11 keysym names on both paths, because wtype takes those
// too. So "Return", "Up", "ctrl+a" and "alt+BackSpace" mean the same thing
// whichever display server is running, and a session does not have to know.
func typeKey(name string) error {
	if err := ensureFocused(); err != nil {
		return err
	}
	parts := strings.Split(name, "+")
	key := parts[len(parts)-1]
	mods := parts[:len(parts)-1]
	if !wayland() {
		return runTool("xdotool", "key", "--clearmodifiers", strings.Join(append(mods, key), "+"))
	}
	// wtype wants each modifier pressed and released around the key, in
	// order, rather than a single joined name.
	//
	// -s first, for the same reason typeText has it: wtype uploads a keymap
	// and sends the key, and with no pause the key can be sent before the
	// compositor has applied the keymap, so it is dropped. Measured — the
	// text path was fixed for this hours before the key path was, and the
	// symptom here was arrow keys that arrived on most runs and vanished on
	// some, which read as a flaky editor rather than as a flaky harness.
	args := []string{"-s", "60"}
	for _, m := range mods {
		args = append(args, "-M", m)
	}
	args = append(args, "-k", key)
	for i := len(mods) - 1; i >= 0; i-- {
		args = append(args, "-m", mods[i])
	}
	return runTool("wtype", args...)
}

// screenshot photographs the focused window.
func screenshot(_ Terminal, path string) error {
	if wayland() {
		// Cropped to the window where the compositor can say where it is.
		// A whole-screen picture is mostly the person's desktop, which makes
		// two runs hard to hold against each other and the file twenty times
		// larger than it needs to be.
		if geom, err := swayWindowGeometry(); err == nil {
			return runTool("grim", "-g", geom, path)
		}
		return runTool("grim", path)
	}
	return runTool("import", "-name", appID, path)
}

// kittyGetText asks kitty for its screen. Kitty's remote control has to be
// turned on for this, so a failure here is not an error: the caller treats
// the grid as a bonus and carries on with the byte log.
func kittyGetText() (string, error) {
	cmd := exec.Command("kitty", "@", "get-text", "--extent", "screen")
	cmd.Env = current.env
	out, err := cmd.Output()
	return string(out), err
}

// weztermGetText asks wezterm for its screen, with the escape sequences left
// in so that colour and attributes are visible rather than flattened away.
func weztermGetText() (string, error) {
	cmd := exec.Command("wezterm", "cli", "get-text", "--escapes")
	cmd.Env = current.env
	out, err := cmd.Output()
	return string(out), err
}

// runTool executes a helper against the compositor the tests are running on.
//
// The environment is the display's rather than this process's, which is what
// keeps wtype and grim pointed at the private compositor instead of the
// person's desktop. A helper run with the ambient environment would reach the
// desktop, which is the whole thing this design exists to prevent.
func runTool(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = current.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// swayWindowGeometry returns this harness's window as grim wants it, "X,Y WxH".
func swayWindowGeometry() (string, error) {
	rect, err := swayWindowRect()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d,%d %dx%d", rect[0], rect[1], rect[2], rect[3]), nil
}

// findWindowRect walks the compositor's tree for the node whose app id is
// ours and returns its rectangle.
func findWindowRect(n any) ([4]int, bool) {
	m, ok := n.(map[string]any)
	if !ok {
		return [4]int{}, false
	}
	name, _ := m["name"].(string)
	if id, _ := m["app_id"].(string); id == appID || strings.Contains(name, appID) {
		if r, ok := m["rect"].(map[string]any); ok {
			num := func(k string) int { v, _ := r[k].(float64); return int(v) }
			return [4]int{num("x"), num("y"), num("width"), num("height")}, true
		}
	}
	for _, key := range []string{"nodes", "floating_nodes"} {
		kids, _ := m[key].([]any)
		for _, kid := range kids {
			if rect, ok := findWindowRect(kid); ok {
				return rect, true
			}
		}
	}
	return [4]int{}, false
}

// resizeWindow resizes this harness's window to cols by rows.
//
// The compositor speaks pixels and the session speaks characters, so the cell
// size is measured from the window rather than assumed: the window was opened
// at a known character size, so its current rectangle divided by that size is
// what one cell is. Deterministic because the opening size is.
func resizeWindow(cols, rows int) error {
	if !wayland() {
		return runTool("xdotool", "search", "--class", appID, "windowsize", "--usehints",
			fmt.Sprint(cols), fmt.Sprint(rows))
	}
	geom, err := swayWindowRect()
	if err != nil {
		return err
	}
	cellW := float64(geom[2]) / float64(Cols)
	cellH := float64(geom[3]) / float64(Rows)
	w := int(cellW * float64(cols))
	h := int(cellH * float64(rows))
	if err := runTool("swaymsg", fmt.Sprintf("[app_id=\"%s\"] resize set width %d px height %d px", appID, w, h)); err != nil {
		// A terminal that took a title rather than an app id is addressed
		// that way instead.
		if err2 := runTool("swaymsg", fmt.Sprintf("[title=\"%s\"] resize set width %d px height %d px", appID, w, h)); err2 != nil {
			return err
		}
	}
	// Take focus back. Under focus-follows-mouse a window that shrinks out
	// from under the pointer loses the keyboard to whatever is now beneath
	// it, and the next keystroke would be refused — correctly, but the run
	// would fail for a reason that is this harness's own doing rather than
	// the port's.
	_ = runTool("swaymsg", fmt.Sprintf("[app_id=\"%s\"] focus", appID))
	_ = runTool("swaymsg", fmt.Sprintf("[title=\"%s\"] focus", appID))
	return nil
}

// swayWindowRect returns this harness's window rectangle.
func swayWindowRect() ([4]int, error) {
	out, err := swayQuery()
	if err != nil {
		return [4]int{}, err
	}
	var tree any
	if err := json.Unmarshal(out, &tree); err != nil {
		return [4]int{}, fmt.Errorf("reading the compositor's tree: %w", err)
	}
	rect, ok := findWindowRect(tree)
	if !ok {
		return [4]int{}, errors.New("the harness window is not in the tree")
	}
	return rect, nil
}

// freeOutput returns an output with nothing fullscreen on it, or "" when
// every output is busy or the question cannot be answered.
//
// Preferring an idle screen rather than demanding one: if both are busy the
// run still goes ahead on whichever the compositor chose, because a covered
// window makes for a poor screenshot but a refused run makes for none at all.
func freeOutput() (string, error) {
	out, err := swayQuery()
	if err != nil {
		return "", err
	}
	var tree any
	if err := json.Unmarshal(out, &tree); err != nil {
		return "", fmt.Errorf("reading the compositor's tree: %w", err)
	}
	m, ok := tree.(map[string]any)
	if !ok {
		return "", errors.New("the compositor's tree is not what was expected")
	}
	outputs, _ := m["nodes"].([]any)
	for _, o := range outputs {
		om, ok := o.(map[string]any)
		if !ok {
			continue
		}
		name, _ := om["name"].(string)
		if name == "" || name == "__i3" {
			continue
		}
		if !hasFullscreen(om) {
			return name, nil
		}
	}
	return "", nil
}

// hasFullscreen reports whether anything under this node is fullscreen.
func hasFullscreen(n any) bool {
	m, ok := n.(map[string]any)
	if !ok {
		return false
	}
	if mode, ok := m["fullscreen_mode"].(float64); ok && mode != 0 {
		return true
	}
	for _, key := range []string{"nodes", "floating_nodes"} {
		kids, _ := m[key].([]any)
		for _, kid := range kids {
			if hasFullscreen(kid) {
				return true
			}
		}
	}
	return false
}

// swayQuery asks the compositor in use for its tree.
func swayQuery() ([]byte, error) {
	cmd := exec.Command("swaymsg", "-t", "get_tree", "-r")
	cmd.Env = current.env
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("asking the compositor for its tree: %w", err)
	}
	return out, nil
}
