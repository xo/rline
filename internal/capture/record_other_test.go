//go:build !linux && !darwin

package capture

import (
	"context"
	"errors"
	"testing"
)

// TestRecordIsUnsupported checks that a system with no recording support says
// so plainly rather than failing in some other way.
func TestRecordIsUnsupported(t *testing.T) {
	t.Parallel()
	_, err := Record(context.Background(), "unused", Sessions[0])
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("Record returned %v, want %v", err, ErrUnsupported)
	}
}
