//go:build linux || darwin

package capture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// Record runs the program at path under a pseudo-terminal, sends the input of
// the session, and returns the transcript.
//
// Record sets HOME and the working directory to an empty directory, so that a
// history file from an earlier run does not change the output. It collects the
// output of the program before the first step, so that a session does not need
// an empty step to wait for a banner.
func Record(ctx context.Context, path string, s Session) (*Transcript, error) {
	if len(s.Steps) == 0 {
		return nil, fmt.Errorf("recording the session %s: %w", s.Name, ErrNoSteps)
	}
	s = s.withDefaults()
	// The recording runs in a temporary directory, so a relative path to the
	// program no longer resolves. Make it absolute first.
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", path, err)
	}
	ctx, cancel := context.WithTimeout(ctx, s.Limit)
	defer cancel()
	home, err := os.MkdirTemp("", "rline-capture-")
	if err != nil {
		return nil, fmt.Errorf("creating home directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(home) }()
	leader, follower, err := OpenPTY()
	if err != nil {
		return nil, err
	}
	defer func() { _ = leader.Close() }()
	if err := setWinsize(leader, s.Cols, s.Rows); err != nil {
		_ = follower.Close()
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = follower, follower, follower
	// The demo writes its history file to the working directory, so the
	// recording runs in the temporary home directory as well.
	cmd.Dir = home
	cmd.Env = []string{
		"TERM=" + s.Term,
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}
	if err := cmd.Start(); err != nil {
		_ = follower.Close()
		return nil, fmt.Errorf("starting %s: %w", path, err)
	}
	// The child owns the follower side now. The parent must let go of it, or
	// the reads below never see the end of the stream.
	_ = follower.Close()
	t := &Transcript{
		Session: s.Name,
		About:   s.About,
		Term:    s.Term,
		Cols:    s.Cols,
		Rows:    s.Rows,
	}
	recErr := run(leader, s, t)
	_ = cmd.Wait()
	return t, recErr
}

// run collects the startup output, then sends every step and collects its
// answer. It appends each exchange to the transcript as it goes, so that a
// recording that fails still shows how far it reached.
func run(leader *os.File, s Session, t *Transcript) error {
	// Wait for the program to say something before the quiet rule applies,
	// so that a program that has not started yet is not mistaken for one
	// that has finished writing.
	b, done, err := drain(leader, s.Quiet, s.Start)
	t.Exchanges = append(t.Exchanges, Exchange{Recv: b})
	if err != nil || done {
		return err
	}
	for i, step := range s.Steps {
		if step.Send != "" {
			if _, err := leader.WriteString(step.Send); err != nil {
				return fmt.Errorf("sending step %d: %w", i, err)
			}
		}
		quiet := step.Wait
		if quiet == 0 {
			quiet = s.Quiet
		}
		b, done, err := drain(leader, quiet, quiet)
		t.Exchanges = append(t.Exchanges, Exchange{Send: []byte(step.Send), Recv: b})
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return nil
}

// drain reads until the program writes nothing for the quiet period. It
// reports whether the stream ended.
//
// first is how long to wait for the first byte, which is longer than quiet
// when the program is still starting. Once anything has arrived, the quiet
// period decides when the program has finished.
func drain(leader *os.File, quiet, first time.Duration) ([]byte, bool, error) {
	var out []byte
	buf := make([]byte, 4096)
	for {
		wait := quiet
		if len(out) == 0 {
			wait = first
		}
		if err := leader.SetReadDeadline(time.Now().Add(wait)); err != nil {
			return out, false, fmt.Errorf("setting read deadline: %w", err)
		}
		n, err := leader.Read(buf)
		out = append(out, buf[:n]...)
		switch {
		case err == nil:
			continue
		case errors.Is(err, os.ErrDeadlineExceeded):
			return out, false, nil
		case isEnd(err):
			// The child closed the terminal, which Linux reports as EIO.
			return out, true, nil
		default:
			return out, false, fmt.Errorf("reading terminal: %w", err)
		}
	}
}

// isEnd reports whether err means the program closed the terminal.
//
// The two systems report it differently. Linux fails the read with EIO once
// the last follower is closed. macOS returns the end of the stream instead,
// which Go reports as io.EOF. Both are listed, because the test that records
// a session runs on either.
func isEnd(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, os.ErrClosed) ||
		errors.Is(err, syscall.EIO) ||
		errors.Is(err, syscall.EBADF)
}

// winsize matches the struct that the TIOCSWINSZ request expects.
type winsize struct {
	rows uint16
	cols uint16
	x    uint16
	y    uint16
}

// setWinsize fixes the terminal size, so that the output does not depend on
// the window of the person who runs the recording.
func setWinsize(leader *os.File, cols, rows int) error {
	ws := winsize{rows: uint16(rows), cols: uint16(cols)}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, leader.Fd(),
		syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); errno != 0 {
		return fmt.Errorf("setting terminal size: %w", errno)
	}
	return nil
}
