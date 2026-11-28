// A terminal that the program gives, such as xterm.js in a web page, where
// there is no file descriptor to read keys from (D79).
//
// This file has no counterpart in isocline. It has no build tag, so that
// every system, js and wasm among them, compiles it and tests it.

package rline

import (
	"io"
	"sync"
	"time"

	"github.com/xo/rline/ansi"
)

// A Terminal is a terminal that the program gives to WithTerminal, where
// there is no file descriptor to read keys from: xterm.js in a web page, or a
// test (D79).
//
// Read gives the bytes that the user types, as a terminal in raw mode sends
// them: escape sequences and bracketed paste included. It returns io.EOF when
// the input ends. Write takes what rline draws, with the same escape
// sequences that rline writes to a real terminal. Size gives the width and the
// height in characters.
//
// A Terminal can have three more methods, which rline uses when they are
// there:
//
//   - Resized() <-chan struct{} sends when the size changes, as SIGWINCH does
//     on a real terminal. Without it, rline asks Size after each key.
//   - SetRaw(on bool) error is called when rline starts and stops reading
//     keys, where a real terminal goes into raw mode and out of it.
//   - Palette() ansi.Palette gives the colors that the terminal can show.
//     Without it, rline writes 256 colors.
type Terminal interface {
	io.Reader
	io.Writer
	Size() (cols, rows int)
}

// resizer is a Terminal that says when its size changes.
type resizer interface {
	Resized() <-chan struct{}
}

// rawSetter is a Terminal that wants to know when raw mode starts and stops.
type rawSetter interface {
	SetRaw(on bool) error
}

// paletteGiver is a Terminal that says which colors it can show.
type paletteGiver interface {
	Palette() ansi.Palette
}

// hostTerminal reads keys from a Terminal for the decoder. A goroutine reads
// the Terminal, because an io.Reader cannot wait for a while and give up,
// and the decoder waits that way for the rest of an escape sequence.
type hostTerminal struct {
	t Terminal

	// chunks carries what the goroutine read. It is closed when the input
	// ends. buf is what is left of the last chunk.
	chunks chan []byte
	buf    []byte
	once   sync.Once
	ended  bool

	// resized is the Terminal's signal for a change of size, or nil, and
	// cols and rows are the size seen last.
	resized    <-chan struct{}
	cols, rows int

	// stop makes a waiting read return, and so does woken, which RunAtPrompt
	// uses (D87).
	stop  chan struct{}
	woken chan struct{}

	// paste says whether startRaw turns on bracketed paste mode, and
	// pasting whether it is on.
	paste   bool
	pasting bool
}

// newHostTerminal returns a hostTerminal that reads t.
func newHostTerminal(t Terminal) *hostTerminal {
	h := &hostTerminal{t: t, stop: make(chan struct{}, 1), woken: make(chan struct{}, 1)}
	if r, ok := t.(resizer); ok {
		h.resized = r.Resized()
	}
	h.cols, h.rows = t.Size()
	return h
}

// newHostDecoder returns a decoder that reads keys from t.
func newHostDecoder(t Terminal) *keyDecoder {
	h := newHostTerminal(t)
	d := newDecoder(h)
	d.ctrl = h
	return d
}

// start begins reading the Terminal, the first time a key is wanted, so that
// a Session that reads nothing takes nothing from it.
func (h *hostTerminal) start() {
	h.once.Do(func() {
		h.chunks = make(chan []byte, 16)
		go func() {
			defer close(h.chunks)
			for {
				b := make([]byte, 256)
				n, err := h.t.Read(b)
				if n > 0 {
					h.chunks <- b[:n]
				}
				if err != nil {
					return
				}
			}
		}()
	})
}

// readByte returns the next byte that the user typed. It reports false when
// none arrived before timeout, when the size changed, when a stop was asked
// for, or when the input ended. A negative timeout waits for as long as it
// takes, and a zero one only looks.
func (h *hostTerminal) readByte(timeout time.Duration) (byte, bool) {
	if len(h.buf) > 0 {
		b := h.buf[0]
		h.buf = h.buf[1:]
		return b, true
	}
	if h.ended {
		return 0, false
	}
	h.start()
	var expire <-chan time.Time
	switch {
	case timeout == 0:
		select {
		case chunk, ok := <-h.chunks:
			return h.take(chunk, ok)
		default:
			return 0, false
		}
	case timeout > 0:
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		expire = timer.C
	}
	select {
	case chunk, ok := <-h.chunks:
		return h.take(chunk, ok)
	case <-h.resized:
		// The edit loop asks resizeEvent after the read, which finds the
		// new size.
	case <-h.stop:
	case <-h.woken:
	case <-expire:
	}
	return 0, false
}

// take keeps a chunk that the goroutine read, and returns its first byte.
// A closed channel means that the input ended.
func (h *hostTerminal) take(chunk []byte, ok bool) (byte, bool) {
	if !ok {
		h.ended = true
		return 0, false
	}
	h.buf = chunk[1:]
	return chunk[0], true
}

// inputEnded reports whether the Terminal's input ended.
func (h *hostTerminal) inputEnded() bool {
	return h.ended
}

// setPaste says whether startRaw turns on bracketed paste mode.
func (h *hostTerminal) setPaste(on bool) {
	h.paste = on
}

// startRaw tells the Terminal that keys are read now, and turns on bracketed
// paste mode when it is asked for, as raw mode does on a real terminal.
func (h *hostTerminal) startRaw() error {
	if r, ok := h.t.(rawSetter); ok {
		if err := r.SetRaw(true); err != nil {
			return err //nolint:wrapcheck // readLine wraps it, as it wraps the error of a real terminal
		}
	}
	if h.paste {
		_, _ = io.WriteString(h.t, pasteOn)
		h.pasting = true
	}
	return nil
}

// endRaw turns bracketed paste mode off again, and tells the Terminal that
// keys are no longer read.
func (h *hostTerminal) endRaw() {
	if h.pasting {
		_, _ = io.WriteString(h.t, pasteOff)
		h.pasting = false
	}
	if r, ok := h.t.(rawSetter); ok {
		_ = r.SetRaw(false)
	}
}

// resizeEvent reports whether the size changed since it last asked.
func (h *hostTerminal) resizeEvent() bool {
	cols, rows := h.t.Size()
	changed := cols != h.cols || rows != h.rows
	h.cols, h.rows = cols, rows
	return changed
}

// wake makes a read that is waiting for a key return with no key (D87).
func (h *hostTerminal) wake() bool {
	select {
	case h.woken <- struct{}{}:
	default:
	}
	return true
}

// asyncStop makes a read that is waiting for a key return.
func (h *hostTerminal) asyncStop() bool {
	select {
	case h.stop <- struct{}{}:
	default:
	}
	return true
}

// close turns bracketed paste mode off, if it is on. The goroutine that reads
// the Terminal ends when its Read returns, which no one can make it do.
func (h *hostTerminal) close() error {
	if h.pasting {
		h.endRaw()
	}
	return nil
}

// hostSizer gives the size of a Terminal to the term.
type hostSizer struct {
	t Terminal
}

// size satisfies sizer.
func (s hostSizer) size() (int, int, bool) {
	cols, rows := s.t.Size()
	return cols, rows, cols > 0 && rows > 0
}

// hostPalette returns the colors that t can show: what it says, or 256
// colors, which xterm.js draws.
func hostPalette(t Terminal) ansi.Palette {
	if p, ok := t.(paletteGiver); ok {
		return p.Palette()
	}
	return ansi.PaletteANSI256
}
