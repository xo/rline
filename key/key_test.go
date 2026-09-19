package key_test

import (
	"testing"

	"github.com/xo/rline/key"
)

// TestF checks that the function keys are numbered from one.
func TestF(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		n    int
		want key.Code
	}{{1, key.F1}, {5, key.F5}, {12, key.F12}} {
		if got := key.F(test.n); got != test.want {
			t.Errorf("F(%d) = %08x, want %08x", test.n, got, test.want)
		}
	}
}

// TestEventsAreNotCharacters checks that the events sit outside the range of
// code points, so a caller cannot mistake one for typed text.
func TestEventsAreNotCharacters(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		code key.Code
	}{
		{"resize", key.EventResize},
		{"auto tab", key.EventAutoTab},
		{"stop", key.EventStop},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.code <= key.UnicodeMax {
				t.Errorf("%08x is inside the range of code points", test.code)
			}
			if _, ok := test.code.Unicode(); ok {
				t.Errorf("%08x reads as a code point", test.code)
			}
			if _, ok := test.code.ASCIIChar(); ok {
				t.Errorf("%08x reads as an ASCII character", test.code)
			}
			if !test.code.IsVirtKey() {
				t.Errorf("%08x is not a virtual key", test.code)
			}
			if test.code < key.EventBase {
				t.Errorf("%08x is below the first event code %08x", test.code, key.EventBase)
			}
		})
	}
}

// TestModsAndNoModsSplitTheCode checks that the two halves of a code come
// apart and go back together.
func TestModsAndNoModsSplitTheCode(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		code     key.Code
		wantKey  key.Code
		wantMods key.Code
	}{
		{"plain letter", 'a', 'a', 0},
		{"ctrl and up", key.Up | key.ModCtrl, key.Up, key.ModCtrl},
		{"every modifier", key.F1 | key.ModShift | key.ModAlt | key.ModCtrl,
			key.F1, key.ModShift | key.ModAlt | key.ModCtrl},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.code.NoMods(); got != test.wantKey {
				t.Errorf("NoMods = %08x, want %08x", got, test.wantKey)
			}
			if got := test.code.Mods(); got != test.wantMods {
				t.Errorf("Mods = %08x, want %08x", got, test.wantMods)
			}
			if got := test.code.NoMods() | test.code.Mods(); got != test.code {
				t.Errorf("the two halves rejoin as %08x, want %08x", got, test.code)
			}
		})
	}
}

// TestASCIIChar checks which codes count as a printable ASCII character.
func TestASCIIChar(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		code key.Code
		want bool
	}{
		{"space is the first one", key.Space, true},
		{"a letter", 'A', true},
		{"rubout is the last one", key.Rubout, true},
		{"a control character is not", key.CtrlA, false},
		{"a letter with ctrl is not", 'a' | key.ModCtrl, false},
		{"a virtual key is not", key.Up, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := test.code.ASCIIChar()
			if ok != test.want {
				t.Fatalf("ASCIIChar(%08x) reported %v, want %v", test.code, ok, test.want)
			}
			if ok && key.Code(got) != test.code {
				t.Errorf("ASCIIChar(%08x) gave %02x", test.code, got)
			}
		})
	}
}
