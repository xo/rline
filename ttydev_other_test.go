//go:build (!unix || aix) && !windows

package rline

import (
	"errors"
	"testing"
)

// TestOpenTTYIsUnsupported checks what a platform with no terminal support
// answers.
func TestOpenTTYIsUnsupported(t *testing.T) {
	t.Parallel()
	if _, err := openTTY(0); !errors.Is(err, errUnsupported) {
		t.Errorf("openTTY gave %v, want %v", err, errUnsupported)
	}
	if isTerminal(0) {
		t.Error("isTerminal said yes on a platform that cannot tell")
	}
}
