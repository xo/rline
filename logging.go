package rline

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Recording a session.
//
// A log holds every byte the reader wrote to the terminal and every byte it
// read from the keyboard, in a form that can be read by eye and turned back
// into the original bytes. It is what lets a session be reconstructed
// afterwards by someone who was not watching it.

// Directions, written at the start of every line of a log.
const (
	// logWrite marks bytes the reader wrote to the terminal.
	logWrite = "<"

	// logRead marks bytes the reader read from the keyboard.
	logRead = ">"

	// logLine marks a finished line, as it was handed back to the caller.
	logLine = "="
)

// sessionLog writes what a reader read and wrote.
//
// It is safe for use from more than one goroutine, because the reading and the
// writing can happen at the same time.
type sessionLog struct {
	mu sync.Mutex
	w  io.Writer
}

// record writes one entry.
func (l *sessionLog) record(direction string, b []byte) {
	if l == nil || len(b) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.w, "%s %s\n", direction, escapeBytes(b))
}

// note writes one entry for text that is not raw bytes, such as a finished
// line.
func (l *sessionLog) note(direction, s string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.w, "%s %s\n", direction, escapeBytes([]byte(s)))
}

// escapeBytes renders bytes so that a person can read them and a program can
// turn them back.
//
// The escape byte becomes "\e", which is what makes a log of terminal output
// readable at all, and the other control bytes become "\xNN". Bytes above 0x7f
// pass through, so that text outside ASCII stays legible.
func escapeBytes(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/4)
	for _, c := range b {
		switch c {
		case 0x1b:
			sb.WriteString(`\e`)
		case '\\':
			sb.WriteString(`\\`)
		case '\r':
			sb.WriteString(`\r`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
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

// logWriter writes to a terminal and records what it wrote.
type logWriter struct {
	w   io.Writer
	log *sessionLog
}

// Write satisfies io.Writer.
func (t logWriter) Write(p []byte) (int, error) {
	t.log.record(logWrite, p)
	n, err := t.w.Write(p)
	if err != nil {
		return n, fmt.Errorf("writing to the terminal: %w", err)
	}
	return n, nil
}

// logReader reads keys from a terminal and records what it read.
type logReader struct {
	src byteReader
	log *sessionLog
}

// readByte satisfies byteReader.
func (t logReader) readByte(timeout time.Duration) (byte, bool) {
	c, ok := t.src.readByte(timeout)
	if ok {
		t.log.record(logRead, []byte{c})
	}
	return c, ok
}
