// +build rline_all rline_replxx

package rline

/*
#cgo LDFLAGS: -L${SRCDIR}/replxx -lreplxx -lstdc++ -lc

#include <stdlib.h>
#include "replxx/replxx.h"
*/
import "C"
import "unsafe"

func init() {
	prompts[Replxx].init = NewReplxxPrompt
}

// ReplxxPrompt is a readline prompt.
type ReplxxPrompt struct {
	replxx *C.Replxx
}

// NewReplxxPrompt creates a new replxx line reader.
func NewReplxxPrompt(app string, opts ...Option) (Prompter, error) {
	ok, err := Initialized(Replxx)
	switch {
	case err != nil:
		return nil, err
	case ok:
		return nil, &ErrPromptAlreadyInitialized{Replxx.String()}
	}
	p := &ReplxxPrompt{}
	for _, o := range opts {
		if err := o(p); err != nil {
			return nil, err
		}
	}
	p.replxx = C.replxx_init()
	return p, nil
}

// Prompt prompts the user.
func (p *ReplxxPrompt) Prompt(s string) (string, error) {
	buf := C.CString(s)
	res, err := C.replxx_input(p.replxx, buf)
	if err != nil {
		return "", err
	}
	C.free(unsafe.Pointer(buf))
	return C.GoString(res), nil
}

// Password prompts a password.
func (p *ReplxxPrompt) Password(string) (string, error) {
	return "", nil
}

// Finalize finalizes the prompt.
func (p *ReplxxPrompt) Finalize() {
	C.replxx_end(p.replxx)
}
