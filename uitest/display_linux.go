//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The compositor the tests run on.
//
// By default that is a private one, started headless with no graphics card
// and its own runtime directory, and thrown away afterwards. This is a safety
// property rather than a tidiness one. Synthesised keys go to a compositor,
// not to a window: wtype types wherever the keyboard focus is at the instant
// it runs, and no amount of checking beforehand closes the gap between the
// check and the keystroke. Two of Ken's windows received test input through
// exactly that gap — a window closed between the check and the type, focus
// moved, and "select alpha, beta, gamma" went into a chat message.
//
// A private compositor removes the question instead of guarding it. wtype
// pointed at this socket cannot reach the desktop at all, because the desktop
// is a different compositor and nothing connects the two.
//
// It also makes the runs reproducible, which the desktop never was: a fixed
// output size, nothing else on screen, no tiling, no focus-follows-mouse, and
// no fullscreen video on the other monitor.

// display is a compositor to run the tests on and the environment that
// reaches it.
type display struct {
	env      []string
	dir      string
	sway     *exec.Cmd
	recorder *exec.Cmd
	vnc      *exec.Cmd
	private  bool
}

// current is the display in use. Package level because the platform hooks are
// plain functions, which is what the other platforms need.
var current = &display{env: os.Environ()}

// startDisplay brings up the compositor the tests will run on.
func startDisplay(visible bool, video, vnc bool, outDir string) (*display, error) {
	if visible {
		// The person asked to watch on their own desktop. Nothing is
		// isolated, so say what that means rather than doing it quietly.
		fmt.Println("!! -visible: typing on your real desktop.")
		fmt.Println("!! Keys go wherever focus is. Do not use the machine while this runs.")
		current = &display{env: os.Environ()}
		return current, nil
	}
	dir, err := os.MkdirTemp("", "uitest-rt")
	if err != nil {
		return nil, fmt.Errorf("making a runtime directory for the private compositor: %w", err)
	}
	conf := filepath.Join(dir, "sway.conf")
	// No borders, no tiling surprises, and focus that does not follow a
	// pointer there is no pointer for.
	if err := os.WriteFile(conf, []byte(
		"output HEADLESS-1 resolution 1920x1080\n"+
			"default_border none\n"+
			"default_floating_border none\n"+
			"focus_follows_mouse no\n"+
			"for_window [app_id=\".*\"] floating enable\n"+
			"for_window [title=\".*\"] floating enable\n"), 0o644); err != nil {
		return nil, fmt.Errorf("writing the private compositor's config: %w", err)
	}

	cmd := exec.Command("sway", "-c", conf)
	cmd.Env = append(envWithout(os.Environ(), "WAYLAND_DISPLAY", "SWAYSOCK", "DISPLAY"),
		"XDG_RUNTIME_DIR="+dir,
		"WLR_BACKENDS=headless",
		"WLR_LIBINPUT_NO_DEVICES=1",
	)
	logFile, err := os.Create(filepath.Join(dir, "sway.log"))
	if err != nil {
		return nil, fmt.Errorf("creating the private compositor's log: %w", err)
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting a private compositor: %w", err)
	}

	d := &display{dir: dir, sway: cmd, private: true}
	sock, err := waitForSocket(dir, 10*time.Second)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	d.env = append(envWithout(os.Environ(), "WAYLAND_DISPLAY", "SWAYSOCK", "DISPLAY"),
		"XDG_RUNTIME_DIR="+dir,
		"WAYLAND_DISPLAY="+filepath.Base(sock),
	)
	if ipc, err := filepath.Glob(filepath.Join(dir, "sway-ipc.*.sock")); err == nil && len(ipc) > 0 {
		d.env = append(d.env, "SWAYSOCK="+ipc[0])
	}
	current = d

	if video {
		d.startRecording(outDir)
	}
	if vnc {
		d.startVNC()
	}
	return d, nil
}

// startRecording records the whole run to one video.
//
// Worth more than it sounds for a harness whose point is visual checking: the
// screenshots show where each step ended up, and the video shows the redraw
// on the way there, which is where flicker and a cursor landing in the wrong
// column for one frame actually live.
func (d *display) startRecording(outDir string) {
	if _, err := exec.LookPath("wf-recorder"); err != nil {
		fmt.Println("   (no wf-recorder, so no video; pacman -S wf-recorder)")
		return
	}
	path := filepath.Join(outDir, "session.mp4")
	_ = os.MkdirAll(outDir, 0o755)
	cmd := exec.Command("wf-recorder", "-o", "HEADLESS-1", "-f", path, "-y")
	cmd.Env = d.env
	if err := cmd.Start(); err != nil {
		fmt.Printf("   (recording failed to start: %v)\n", err)
		return
	}
	d.recorder = cmd
	fmt.Printf("   recording to %s\n", path)
}

// startVNC serves the private compositor so a person can watch it live.
func (d *display) startVNC() {
	if _, err := exec.LookPath("wayvnc"); err != nil {
		fmt.Println("   (no wayvnc, so nothing to connect to; pacman -S wayvnc)")
		return
	}
	cmd := exec.Command("wayvnc", "127.0.0.1", "5900")
	cmd.Env = d.env
	if err := cmd.Start(); err != nil {
		fmt.Printf("   (wayvnc failed to start: %v)\n", err)
		return
	}
	d.vnc = cmd
	fmt.Println("   watch it live: connect a VNC client to 127.0.0.1:5900")
	fmt.Println("   waiting 10s so you can connect before the first session")
	time.Sleep(10 * time.Second)
}

// stop shuts the compositor and anything watching it down.
func (d *display) stop() {
	if d.recorder != nil && d.recorder.Process != nil {
		// Interrupt rather than kill: wf-recorder writes the file's index on
		// the way out, and a killed recording is a file nothing will play.
		_ = d.recorder.Process.Signal(os.Interrupt)
		_, _ = d.recorder.Process.Wait()
	}
	if d.vnc != nil && d.vnc.Process != nil {
		_ = d.vnc.Process.Kill()
		_, _ = d.vnc.Process.Wait()
	}
	if d.sway != nil && d.sway.Process != nil {
		_ = d.sway.Process.Kill()
		_, _ = d.sway.Process.Wait()
	}
	if d.dir != "" {
		_ = os.RemoveAll(d.dir)
	}
}

// waitForSocket waits for the compositor to be listening.
func waitForSocket(dir string, limit time.Duration) (string, error) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		socks, _ := filepath.Glob(filepath.Join(dir, "wayland-*"))
		for _, s := range socks {
			if !strings.HasSuffix(s, ".lock") {
				// The socket file appears before the compositor is ready to
				// serve on it.
				time.Sleep(500 * time.Millisecond)
				return s, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", errors.New("the private compositor never started listening; see its log in the temporary runtime directory")
}

// envWithout returns env with the named variables removed.
func envWithout(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		drop := false
		for _, n := range names {
			if strings.HasPrefix(e, n+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, e)
		}
	}
	return out
}
