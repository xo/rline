//go:build !linux && !darwin && !windows

package main

import "errors"

// Everywhere else.
//
// This harness needs a desktop, a way to synthesise a keystroke and a way to
// photograph a window, and the systems this file covers have at most one of
// the three. It exists so that the package still builds for them, which the
// cross-vet loop over every GOOS/GOARCH pair checks: a command that compiles
// nowhere but the three desktops would fail that loop and nobody would find
// out until a release.

const isWindows = false

// appID is the name a window would be given, if this platform could open one.
// It is here because the shared code names it when building the command that
// sets a window title, and that code is compiled everywhere.
const appID = "com.github.xo.rline"

// errNoDesktop says why this platform cannot run the harness.
var errNoDesktop = errors.New("uitest needs a desktop with synthetic input and window capture, " +
	"and this platform has no implementation; the byte-level tests in internal/capture run everywhere")

// setFontSize is accepted and ignored: there is no terminal here to ask.
func setFontSize(int) {}

func platformReady() error              { return errNoDesktop }
func platformTerminals() []Terminal     { return nil }
func focusWindow(Terminal) error        { return errNoDesktop }
func typeText(string) error             { return errNoDesktop }
func typeKey(string) error              { return errNoDesktop }
func resizeWindow(int, int) error       { return errNoDesktop }
func screenshot(Terminal, string) error { return errNoDesktop }
