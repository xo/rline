package rline

import (
	"strings"
	"sync"
)

//go:generate stringer -type=Prompt

// Prompt is the prompt implementation type.
type Prompt int

// Prompt implementation values.
const (
	Default Prompt = iota
	Readline
	Replxx
)

// PromptFromString returns a prompt based on the type given.
func PromptFromString(typ string) (Prompt, error) {
	switch strings.ToLower(typ) {
	case "", "default":
		return Default, nil
	case "readline":
		return Readline, nil
	case "replxx":
		return Replxx, nil
	}
	return Prompt(-1), &ErrInvalidPromptType{typ}
}

// prompt contains initializer for a prompt.
type prompt struct {
	init     func(string, ...Option) (Prompter, error)
	prompter Prompter
	once     sync.Once
}

// prompts are the set of prompts.
var prompts = [...]prompt{
	Default:  {init: NewDefaultPrompt},
	Readline: {},
	Replxx:   {},
}

// Init initializes the prompt with the specified options.
func (prompt Prompt) Init(app string, opts ...Option) (Prompter, error) {
	p := prompts[prompt]
	if p.init == nil {
		return nil, &ErrPromptNotAvailable{prompt.String()}
	}
	var err error
	(&p.once).Do(func() {
		p.prompter, err = p.init(app, opts...)
	})
	if err != nil {
		return nil, err
	}
	if p.prompter == nil {
		return nil, &ErrPromptNotAvailable{prompt.String()}
	}
	return p.prompter, nil
}

// Initialized returns whether or not the specified prompt has already been
// initialized.
func Initialized(prompt Prompt) (bool, error) {
	switch {
	case int(prompt) >= len(prompts):
		return false, &ErrInvalidPromptType{prompt.String()}
	case prompts[prompt].init == nil:
		return false, &ErrPromptNotAvailable{prompt.String()}
	}
	return prompts[prompt].prompter != nil, nil
}
