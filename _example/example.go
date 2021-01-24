// _example/example.go
//
// Note: build with tags rline_all in order to make use of the readline and
// replxx prompts.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"
	"syscall"

	"github.com/xo/rline"
)

func main() {
	typ := flag.String("type", "", "prompt type")
	flag.Parse()
	if err := run(*typ); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(typ string) error {
	var opts []rline.Option
	p, err := rline.FromString(typ, "example", opts...)
	if err != nil {
		return err
	}
	var blank int
loop:
	for {
		s, err := p.Prompt(fmt.Sprintf("%s> ", p.Type()))
		switch {
		case err != nil && err == io.EOF:
			break loop
		case err != nil && err == syscall.EAGAIN:
			// ignore ctrl-c
		case err != nil:
			return err
		}
		fmt.Printf("> %q\n", s)
		s = strings.TrimSpace(s)
		if s == "" {
			blank++
		} else {
			blank = 0
		}
		switch {
		case strings.HasPrefix(s, `\setprompt `):
			if err := p.SetPromptFromString(strings.TrimSpace(strings.TrimPrefix(s, `\setprompt `)), opts...); err != nil {
				return err
			}
		case strings.HasPrefix(s, `\quit`) || strings.HasPrefix(s, `\exit`) || strings.HasPrefix(s, `\q`):
			break loop
		case s == "hello":
			u, err := user.Current()
			if err != nil {
				return err
			}
			fmt.Printf("hello %s!\n", u.Username)
		case blank > 10:
			fmt.Printf("blank > 10, quitting\n")
			break loop
		}
	}
	return nil
}
