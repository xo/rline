package rline

// Option is a line reader option.
type Option func(interface{}) error

// opt wraps an option.
type opt struct {
	Default  func(interface{}) error
	Readline func(interface{}) error
	Replxx   func(interface{}) error
}

// WithHistory is a line reader option to set a history handler.
func WithHistory(h History) Option {
	return func(interface{}) error {
		return nil
	}
}

// WithHistoryFile is a line reader option to create a simple history handler
// from the provided file.
func WithHistoryFile() Option {
	return func(interface{}) error {
		return nil
	}
}
