package rline

// Prompter is the shared interfaces for prompts.
type Prompter interface {
	Prompt(string) (string, error)
	Password(string) (string, error)
}

// Rline is a line reader.
type Rline struct {
	prompt   Prompt
	app      string
	prompter Prompter
}

// New creates a new line reader for the specified prompt and app name.
func New(prompt Prompt, app string, opts ...Option) (*Rline, error) {
	r := &Rline{
		app: app,
	}
	if err := r.SetPrompt(prompt, opts...); err != nil {
		return nil, err
	}
	return r, nil
}

// FromString creates a new line reader for the specified prompt and app name.
func FromString(typ, app string, opts ...Option) (*Rline, error) {
	prompt, err := PromptFromString(typ)
	if err != nil {
		return nil, err
	}
	return New(prompt, app, opts...)
}

// SetPrompt changes the prompt.
//
// Can be used to dynamically change the line reader prompt.
//
// Options will need to be passed again if a prompt is changed, as individual
// options apply only to the actual line reader implementations.
func (r *Rline) SetPrompt(prompt Prompt, opts ...Option) error {
	var err error
	r.prompter, err = prompt.Init(r.app, opts...)
	if err != nil {
		return err
	}
	r.prompt = prompt
	return nil
}

// SetPromptFromString changes the prompt to the named prompt type.
func (r *Rline) SetPromptFromString(typ string, opts ...Option) error {
	prompt, err := PromptFromString(typ)
	if err != nil {
		return err
	}
	return r.SetPrompt(prompt, opts...)
}

// Type returns the prompt's type.
func (r *Rline) Type() string {
	return r.prompt.String()
}

// Prompt prompts the user for a line of text.
func (r *Rline) Prompt(s string) (string, error) {
	return r.prompter.Prompt(s)
}

// Password prompts the user for a password.
func (r *Rline) Password(s string) (string, error) {
	return r.prompter.Password(s)
}
