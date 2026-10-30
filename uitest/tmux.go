//go:build linux || darwin

package main

import "os"

// tmuxArgs returns the command line that runs cmd in tmux, on a server of
// its own with no configuration and no status line, so that cmd gets every
// row. tmux is in the matrix because it rewrites escape sequences on their
// way to the terminal (D60). The server exits when cmd does.
func tmuxArgs(cmd []string) []string {
	argv := append([]string{"tmux", "-L", "rline-uitest", "-f", os.DevNull, "new-session"}, cmd...)
	return append(argv, ";", "set-option", "status", "off")
}
