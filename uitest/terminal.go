package main

import (
	"os/exec"
)

// A Terminal is one emulator this harness knows how to open.
//
// The registry is per platform, in the platform_*.go files, because the way a
// terminal is launched and the way it is asked for its size are both things
// that differ by system rather than by terminal.
type Terminal struct {
	// Name is what the person types after -terminal, and what names the
	// artifacts.
	Name string

	// Bin is the executable to look for. A terminal whose binary is missing
	// is skipped rather than failed: the matrix is what is installed here,
	// and no machine has all of them.
	Bin string

	// Engine names what the terminal actually is, where that differs from
	// the brand. Several terminals share one, and a matrix that lists both
	// gnome-terminal and xfce4-terminal is testing VTE twice.
	Engine string

	// Args returns the command line that opens this terminal at cols by rows
	// running cmd. The terminal is expected to exit when cmd does.
	Args func(cols, rows int, cmd []string) []string

	// GetText, when set, returns the visible screen as text. Only a few
	// terminals can answer this. It is a bonus rather than the mechanism:
	// the byte log is what every terminal gives us.
	GetText func() (string, error)
}

// found reports whether this terminal is installed.
func (t Terminal) found() bool {
	_, err := exec.LookPath(t.Bin)
	return err == nil
}

// terminals returns every terminal the current platform knows, installed or
// not. Callers filter with found.
func terminals() []Terminal { return platformTerminals() }

// lookup returns the named terminal.
func lookup(name string) (Terminal, bool) {
	for _, t := range terminals() {
		if t.Name == name {
			return t, true
		}
	}
	return Terminal{}, false
}
