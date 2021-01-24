package rline

import (
	"fmt"
	"strings"
)

// Error is line reader error.
type Error string

// Error satisfies the error interface.
func (err Error) Error() string {
	return string(err)
}

// Error values.
const ()

// ErrInvalidPromptType is the invalid prompt type error.
type ErrInvalidPromptType struct {
	Type string
}

// Error satisfies the error interface.
func (err *ErrInvalidPromptType) Error() string {
	return fmt.Sprintf("invalid prompt type %q", err.Type)
}

// ErrPromptNotAvailable is the prompt not available error.
type ErrPromptNotAvailable struct {
	Name string
}

// Error satisfies the error interface.
func (err *ErrPromptNotAvailable) Error() string {
	return fmt.Sprintf("%s prompt not available: try building with -tags 'rline_%s'", err.Name, strings.ToLower(err.Name))
}

// ErrPromptAlreadyInitialized is the prompt already initialized error.
type ErrPromptAlreadyInitialized struct {
	Type string
}

// Error satisfies the error interface.
func (err *ErrPromptAlreadyInitialized) Error() string {
	return fmt.Sprintf("%s prompt already initalized", err.Type)
}
