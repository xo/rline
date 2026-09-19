package capture

import (
	"fmt"
	"strconv"
	"strings"
)

// ErrBadEscape reports text that Unescape cannot read.
type ErrBadEscape struct {
	// Offset is the position of the fault in the text.
	Offset int

	// Reason says what is wrong.
	Reason string
}

// Error satisfies the error interface.
func (err *ErrBadEscape) Error() string {
	return fmt.Sprintf("bad escape at offset %d: %s", err.Offset, err.Reason)
}

// Escape renders b as printable text for a golden file.
//
// The escape byte becomes the two characters "\e", and other control bytes
// become "\xNN". Bytes above 0x7f pass through, so that UTF-8 text stays
// readable. A real newline follows every "\r" and "\n", which breaks the text
// into lines. Unescape ignores those newlines, so Escape and Unescape return
// the original bytes.
func Escape(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) * 2)
	for _, c := range b {
		switch c {
		case 0x1b:
			sb.WriteString(`\e`)
		case '\\':
			sb.WriteString(`\\`)
		case '\t':
			sb.WriteString(`\t`)
		case '\r':
			sb.WriteString(`\r`)
			sb.WriteByte('\n')
		case '\n':
			sb.WriteString(`\n`)
			sb.WriteByte('\n')
		default:
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&sb, `\x%02x`, c)
				continue
			}
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// Unescape reads text that Escape produced and returns the original bytes.
func Unescape(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == '\n' {
			// Layout only. Escape adds these to break the text into lines.
			i++
			continue
		}
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		if i+1 >= len(s) {
			return nil, &ErrBadEscape{Offset: i, Reason: `text ends after "\"`}
		}
		switch s[i+1] {
		case 'e':
			out = append(out, 0x1b)
		case '\\':
			out = append(out, '\\')
		case 't':
			out = append(out, '\t')
		case 'r':
			out = append(out, '\r')
		case 'n':
			out = append(out, '\n')
		case 'x':
			if i+4 > len(s) {
				return nil, &ErrBadEscape{Offset: i, Reason: `text ends inside "\x"`}
			}
			v, err := strconv.ParseUint(s[i+2:i+4], 16, 8)
			if err != nil {
				return nil, &ErrBadEscape{Offset: i, Reason: fmt.Sprintf("reading hex %q: %v", s[i+2:i+4], err)}
			}
			out = append(out, byte(v))
			i += 4
			continue
		default:
			return nil, &ErrBadEscape{Offset: i, Reason: fmt.Sprintf(`unknown escape "\%c"`, s[i+1])}
		}
		i += 2
	}
	return out, nil
}
