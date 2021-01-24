package rline

// DefaultPrompt
type DefaultPrompt struct {
}

// NewDefaultPrompt creates a default line reader prompt.
func NewDefaultPrompt(app string, opts ...Option) (Prompter, error) {
	p := &DefaultPrompt{}
	for _, o := range opts {
		if err := o(p); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Prompt prompts a line of text.
func (p *DefaultPrompt) Prompt(string) (string, error) {
	return "", nil
}

// Password prompts a password.
func (p *DefaultPrompt) Password(string) (string, error) {
	return "", nil
}
