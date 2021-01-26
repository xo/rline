// _example/example.go
//
// Note: build with tags rline_readline and rline_replxx in order to make use
// of the readline and replxx prompts.
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
	var prompt rline.Prompt
	for _, p := range []rline.Prompt{rline.Readline, rline.Replxx} {
		if p.Available() {
			prompt = p
			break
		}
	}
	typ := flag.String("type", strings.ToLower(prompt.String()), "prompt type")
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
		cmd, param := s, ""
		if i := strings.IndexAny(cmd, " \t"); i != -1 {
			param = strings.TrimSpace(cmd[i:])
			cmd = strings.TrimSpace(cmd[:i])
		}
		switch {
		case cmd == `\setprompt`:
			if err := p.SetPromptFromString(param, opts...); err != nil {
				return err
			}
		case cmd == `\quit` || cmd == `\exit` || cmd == `\q` || cmd == `exit` || cmd == `quit`:
			break loop
		case cmd == "hello":
			u, err := user.Current()
			if err != nil {
				return err
			}
			fmt.Printf("hello %s!\n", u.Username)
		case blank > 10:
			fmt.Printf("blank > 10, exiting\n")
			break loop
		}
	}
	return nil
}
