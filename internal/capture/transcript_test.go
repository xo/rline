package capture

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestEscapeRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"empty", nil, ""},
		{"plain", []byte("hello"), "hello"},
		{"escape", []byte{0x1b, '[', '3', '1', 'm'}, `\e[31m`},
		{"backslash", []byte(`a\b`), `a\\b`},
		{"tab", []byte("a\tb"), `a\tb`},
		{"newline", []byte("a\nb"), "a\\n\nb"},
		{"carriage return", []byte("a\rb"), "a\\r\nb"},
		{"other control", []byte{0x00, 0x07, 0x7f}, `\x00\x07\x7f`},
		{"utf8", []byte("isoclinε"), "isoclinε"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := Escape(test.in)
			if got != test.want {
				t.Errorf("Escape(%q) = %q, want %q", test.in, got, test.want)
			}
			back, err := Unescape(got)
			if err != nil {
				t.Fatalf("Unescape(%q): %v", got, err)
			}
			if !bytes.Equal(back, test.in) {
				t.Errorf("round trip of %q gave %q", test.in, back)
			}
		})
	}
}

func TestEscapeAllBytes(t *testing.T) {
	t.Parallel()
	in := make([]byte, 256)
	for i := range in {
		in[i] = byte(i)
	}
	back, err := Unescape(Escape(in))
	if err != nil {
		t.Fatalf("Unescape: %v", err)
	}
	if !bytes.Equal(back, in) {
		t.Errorf("round trip changed the bytes")
	}
}

func TestUnescapeErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"trailing backslash", `abc\`, `text ends after "\"`},
		{"short hex", `\x0`, `text ends inside "\x"`},
		{"bad hex", `\xzz`, "reading hex"},
		{"unknown escape", `\q`, `unknown escape "\q"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Unescape(test.in)
			if err == nil {
				t.Fatalf("Unescape(%q) returned no error", test.in)
			}
			var bad *ErrBadEscape
			if !errors.As(err, &bad) {
				t.Fatalf("Unescape(%q) returned %T, want *ErrBadEscape", test.in, err)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("Unescape(%q) said %q, want it to contain %q", test.in, err, test.want)
			}
		})
	}
}

func FuzzEscapeRoundTrip(f *testing.F) {
	f.Add([]byte("hello\r\n"))
	f.Add([]byte{0x1b, '[', 'A'})
	f.Fuzz(func(t *testing.T, in []byte) {
		back, err := Unescape(Escape(in))
		if err != nil {
			t.Fatalf("Unescape after Escape of %q: %v", in, err)
		}
		if !bytes.Equal(back, in) {
			t.Errorf("round trip of %q gave %q", in, back)
		}
	})
}
