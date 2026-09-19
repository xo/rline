package rline

import (
	"testing"
	"time"

	"github.com/xo/rline/key"
)

// TestTTYPushedCodeComesBackUnchanged checks that a key pushed back is
// returned exactly as it was given.
//
// A key read from the terminal goes through modifyCode, which rewrites
// several of them. A key pushed back has been through that already, so
// running it through a second time would change it again. key.Rubout shows
// the difference, because modifyCode turns it into key.Backspace.
func TestTTYPushedCodeComesBackUnchanged(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{})
	term.pushCode(key.Rubout)
	got, ok := term.readTimeout(0)
	if !ok {
		t.Fatal("reading a pushed back key found nothing")
	}
	if got != key.Rubout {
		t.Errorf("a pushed back key came back as %08x, want %08x", got, key.Rubout)
	}
	if _, ok := term.readTimeout(0); ok {
		t.Error("a second read found a key where there should be none")
	}
}

// TestTTYPushedCodesAreReadInReverse records that the code buffer is a stack,
// so the key pushed last is read first.
func TestTTYPushedCodesAreReadInReverse(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{})
	term.pushCode(key.Up)
	term.pushCode(key.Down)
	for i, want := range []key.Code{key.Down, key.Up} {
		got, ok := term.readTimeout(0)
		if !ok {
			t.Fatalf("read %d found nothing", i)
		}
		if got != want {
			t.Errorf("read %d gave %08x, want %08x", i, got, want)
		}
	}
}

// TestTTYPushbackStopsAtTheLimit checks that neither buffer grows without
// bound. Anything past the limit is dropped, as in the C code.
func TestTTYPushbackStopsAtTheLimit(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{})
	for range ttyPushMax + 10 {
		term.pushByte('x')
		term.pushCode(key.Up)
	}
	if got := len(term.pushedBytes); got != ttyPushMax {
		t.Errorf("the byte buffer holds %d, want %d", got, ttyPushMax)
	}
	if got := len(term.pushedCodes); got != ttyPushMax {
		t.Errorf("the code buffer holds %d, want %d", got, ttyPushMax)
	}
}

// TestTTYReadEndsAtKeyNone checks the blocking read, which returns key.None
// once the input is finished.
func TestTTYReadEndsAtKeyNone(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{bytes: []byte("hi")})
	for _, want := range []key.Code{'h', 'i', key.None} {
		if got := term.read(); got != want {
			t.Errorf("read gave %08x, want %08x", got, want)
		}
	}
}

// TestTTYSetEscDelay checks that the waits can be changed, which the timing
// tests will need once a clock can be injected.
func TestTTYSetEscDelay(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{})
	if term.escInitialTimeout != defaultEscInitialTimeout || term.escTimeout != defaultEscTimeout {
		t.Fatalf("a new tty waits %v and %v, want %v and %v",
			term.escInitialTimeout, term.escTimeout,
			defaultEscInitialTimeout, defaultEscTimeout)
	}
	term.setEscDelay(5*time.Millisecond, time.Millisecond)
	if term.escInitialTimeout != 5*time.Millisecond || term.escTimeout != time.Millisecond {
		t.Errorf("after setEscDelay the tty waits %v and %v, want 5ms and 1ms",
			term.escInitialTimeout, term.escTimeout)
	}
}

// TestTTYDecodesAcrossReads checks that a sequence split over several reads
// still decodes as one key. The terminal delivers bytes as they arrive, so
// the decoder cannot assume a whole sequence is ready at once.
func TestTTYDecodesAcrossReads(t *testing.T) {
	t.Parallel()
	term := newTTY(&idleReader{bytes: []byte("\x1b[1;5A")})
	term.setEscDelay(0, 0)
	got, ok := term.readTimeout(0)
	if !ok {
		t.Fatal("no key decoded")
	}
	if want := key.Up | key.ModCtrl; got != want {
		t.Errorf("decoded %08x, want %08x", got, want)
	}
}
