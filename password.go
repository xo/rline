package rline

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xo/rline/key"
)

// Reading something that must not be shown.
//
// isocline has nothing of this kind, so this is not a port. It is here because
// a program that asks for a password needs the terminal turned down in a way
// only the terminal layer can arrange.

// Password reads a line without showing it and without recording it.
//
// Nothing is echoed, nothing is added to the history, and no completion,
// hinting or highlighting runs. Backspace deletes the last character and
// Ctrl-U clears what has been typed. Enter finishes.
//
// It answers io.EOF when the user ends the input with Ctrl-D on an empty line,
// and ErrInterrupted when the user presses Ctrl-C, which a caller usually
// treats as "do not ask again" rather than as a failure.
//
// When there is no terminal to read from, the line is read plainly from the
// input, which is what a program driven by a script needs. Nothing is hidden
// in that case, because there is no terminal to hide it from, and a caller
// that must not read a password from a pipe should check Interactive first.
func (r *Session) Password(prompt string) (string, error) {
	switch {
	case r.closed:
		return "", ErrClosed
	case r.noEdit:
		return r.readPlainPassword(prompt)
	}
	r.env.term.write(prompt)
	r.env.term.flush()
	// Echo off with canonical mode left on, where the terminal can see it.
	// Where that is not available the line is still hidden, by reading it in
	// raw mode, but the terminal has no way to know it is a password.
	if dev, ok := r.env.tty.dev.(noEchoDevice); ok {
		return r.readNoEcho(dev)
	}
	if err := r.env.tty.startRaw(); err != nil {
		return "", fmt.Errorf("switching the terminal to raw mode: %w", err)
	}
	line, err := r.readHidden()
	r.env.tty.endRaw()
	// The line the user typed is invisible, so the cursor has to be moved on
	// by hand or the next thing written lands beside the prompt.
	r.env.term.writeln("")
	r.env.term.flush()
	return line, err
}

// noEchoDevice is a terminal that can turn off echo without turning off the
// editing the terminal driver does, which is what a password prompt looks like
// from outside.
type noEchoDevice interface {
	startNoEcho() error
	endNoEcho()
}

// readNoEcho reads one line with echo off and the terminal driver still doing
// the editing.
func (r *Session) readNoEcho(dev noEchoDevice) (string, error) {
	if err := dev.startNoEcho(); err != nil {
		return "", err
	}
	defer dev.endNoEcho()
	var sb strings.Builder
	for {
		c, ok := r.env.tty.dev.readByte(-1)
		switch {
		case !ok:
			// The input ended, which is Ctrl-D on an empty line.
			if sb.Len() == 0 {
				r.env.term.writeln("")
				r.env.term.flush()
				return "", io.EOF
			}
			return sb.String(), nil
		case c == '\n' || c == '\r':
			// The newline was not echoed either, so the cursor has to be moved
			// on by hand.
			r.env.term.writeln("")
			r.env.term.flush()
			return sb.String(), nil
		}
		sb.WriteByte(c)
	}
}

// readHidden reads keys until the line ends, showing nothing.
func (r *Session) readHidden() (string, error) {
	var sb strings.Builder
	for {
		switch c := r.env.tty.read(); c {
		case key.Enter, key.Linefeed:
			return sb.String(), nil
		case key.CtrlC:
			return "", ErrInterrupted
		case key.CtrlD:
			if sb.Len() == 0 {
				return "", io.EOF
			}
		case key.CtrlU:
			sb.Reset()
		case key.Backspace, key.Rubout:
			// Take off the last character rather than the last byte, so that
			// deleting works on text outside ASCII.
			s := sb.String()
			if s != "" {
				b := []byte(s)
				n, _ := prevOfs(b, len(b))
				sb.Reset()
				sb.Write(b[:len(b)-n])
			}
		case key.EventStop:
			return "", io.EOF
		default:
			if chr, ok := c.ASCIIChar(); ok {
				sb.WriteByte(chr)
				continue
			}
			if ru, ok := c.Unicode(); ok {
				sb.WriteRune(ru)
			}
			// Anything else is a key that has no place in a password.
		}
	}
}

// readPlainPassword reads a line when there is no terminal to hide it on.
func (r *Session) readPlainPassword(prompt string) (string, error) {
	if r.env != nil && r.env.tty != nil {
		r.env.term.write(prompt)
		r.env.term.flush()
	}
	line, err := r.plain.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		if errors.Is(err, io.EOF) {
			return "", io.EOF
		}
		return "", fmt.Errorf("reading a line: %w", err)
	}
	return trimNewline(line), nil
}
