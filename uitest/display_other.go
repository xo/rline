//go:build !linux

package main

import (
	"fmt"
	"os"
)

// Everywhere but Linux, the tests run on the desktop the person is using.
//
// The Linux side runs them on a private headless compositor, which is what
// makes synthesised keys safe there: a key goes to a compositor rather than
// to a window, so a harness sharing the desktop can type into whatever has
// focus at that instant, and a check beforehand does not close the gap.
//
// macOS and Windows have no equivalent that costs nothing. macOS has no
// nested window server, and on Windows a separate desktop through
// CreateDesktop would need the terminals launched into it and screenshots
// taken from it, which is real work and not yet done. So on those systems the
// harness shares the desktop and relies on the focus check, and the person
// running it should not use the machine while it runs. That is a weaker
// promise than Linux's and it is stated rather than glossed.
type display struct{ env []string }

// current is the display in use.
var current = &display{env: os.Environ()}

// startDisplay reports what the person is about to get.
func startDisplay(visible, video, vnc bool, outDir string) (*display, error) {
	if video || vnc {
		fmt.Println("   (-video and -vnc need the private compositor, which is Linux only)")
	}
	fmt.Println("!! These tests type on your real desktop: there is no isolated")
	fmt.Println("!! display on this platform. Keys go wherever focus is, so do not")
	fmt.Println("!! use the machine while this runs.")
	current = &display{env: os.Environ()}
	return current, nil
}

// stop has nothing to shut down.
func (d *display) stop() {}
