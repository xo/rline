// Command example reads lines with rline until the input ends.
//
// It is the smallest program that exercises the package end to end, and
// TestExampleRunsUnderATerminal drives it through a pseudo-terminal.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/xo/rline"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// run reads lines until the input ends, writing each one back.
func run() error {
	r, err := rline.New(
		rline.WithPrompt("> ", "| "),
		rline.WithHistory("", 0),
	)
	if err != nil {
		return fmt.Errorf("starting the reader: %w", err)
	}
	defer func() { _ = r.Close() }()

	r.Println("[b]rline[/b] example. Type [ic-emphasis]exit[/] to leave.")
	for {
		line, err := r.ReadLine("")
		if errors.Is(err, io.EOF) {
			r.Println("")
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading a line: %w", err)
		}
		if line == "exit" {
			return nil
		}
		r.Printf("you said: %s", line)
		r.Println("")
	}
}
