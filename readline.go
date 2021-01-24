// +build rline_readline rline_readline_dynamic

package rline

/*
#include <stdio.h>
#include <stdlib.h>
#include "readline/readline.h"
#include "readline/history.h"
#include "readline/keymaps.h"
*/
import "C"

import (
	"os"
	"os/signal"
	"syscall"
	"unsafe"
)

func init() {
	prompts[Readline].init = NewReadlinePrompt
}

// NewReadlinePrompt creates a new readline line reader.
func NewReadlinePrompt(app string, opts ...Option) (Prompter, error) {
	ok, err := Initialized(Readline)
	switch {
	case err != nil:
		return nil, err
	case ok:
		return nil, &ErrPromptAlreadyInitialized{Readline.String()}
	}
	p := &ReadlinePrompt{
		ignore: []os.Signal{syscall.SIGINT, syscall.SIGTERM},
	}
	for _, o := range opts {
		if err := o(p); err != nil {
			return nil, err
		}
	}
	C.rl_readline_name = C.CString(app)
	C.rl_initialize()
	// C.rl_attempted_completion_function = C.readline_completion_function(p.completion)
	return p, nil
}

// ReadlinePrompt is a readline prompt.
type ReadlinePrompt struct {
	ignore []os.Signal
}

// Prompt prompts the user.
func (p *ReadlinePrompt) Prompt(s string) (string, error) {
	// capture ignore state
	var restore []os.Signal
	for _, sig := range p.ignore {
		if signal.Ignored(sig) {
			continue
		}
		restore = append(restore, sig)
	}
	signal.Ignore(p.ignore...)
	C.rl_reset_screen_size()
	prompt := C.CString(s)
	buf, err := C.readline(prompt)
	if err != nil {
		return "", err
	}
	res := C.GoString(buf)
	C.rl_free(unsafe.Pointer(buf))
	C.free(unsafe.Pointer(prompt))
	// restore signals
	signal.Ignore(restore...)
	return res, nil
}

// Password prompts a password.
func (p *ReadlinePrompt) Password(string) (string, error) {
	return "", nil
}
