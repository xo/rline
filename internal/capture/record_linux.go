//go:build linux

package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// Record runs the program at path under a pseudo-terminal, sends the input of
// the session, and returns every byte that the program wrote.
//
// Record sets HOME to an empty directory, so that a history file from an
// earlier run does not change the output.
func Record(ctx context.Context, path string, s Session) ([]byte, error) {
	if len(s.Steps) == 0 {
		return nil, ErrNoSteps
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
	defer os.RemoveAll(home)
	leader, follower, err := openPTY()
	if err != nil {
		return nil, err
	}
	defer leader.Close()
	if err := setWinsize(leader, s.Cols, s.Rows); err != nil {
		follower.Close()
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
		follower.Close()
		return nil, fmt.Errorf("starting %s: %w", path, err)
	}
	// The child owns the follower side now. The parent must let go of it, or
	// the reads below never see the end of the stream.
	follower.Close()
	out, recErr := run(leader, s)
	_ = cmd.Wait()
	return out, recErr
}

// run sends every step and collects the output.
func run(leader *os.File, s Session) ([]byte, error) {
	var out []byte
	for i, step := range s.Steps {
		if step.Send != "" {
			if _, err := leader.WriteString(step.Send); err != nil {
				return out, fmt.Errorf("sending step %d: %w", i, err)
			}
		}
		quiet := step.Wait
		if quiet == 0 {
			quiet = s.Quiet
		}
		b, done, err := drain(leader, quiet)
		out = append(out, b...)
		if err != nil {
			return out, err
		}
		if done {
			return out, nil
		}
	}
	// Collect whatever the program writes as it exits.
	b, _, err := drain(leader, s.Quiet)
	out = append(out, b...)
	return out, err
}

// drain reads until the program writes nothing for the quiet period. It
// reports whether the stream ended.
func drain(leader *os.File, quiet time.Duration) ([]byte, bool, error) {
	var out []byte
	buf := make([]byte, 4096)
	for {
		if err := leader.SetReadDeadline(time.Now().Add(quiet)); err != nil {
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
func isEnd(err error) bool {
	return errors.Is(err, os.ErrClosed) ||
		errors.Is(err, syscall.EIO) ||
		errors.Is(err, syscall.EBADF)
}

// openPTY opens a pseudo-terminal. It returns the leader side, which this
// process drives, and the follower side, which the child uses as its terminal.
//
// The leader fd is put in non-blocking mode before os.NewFile wraps it. A
// blocking terminal fd does not reach the Go poller, and then SetReadDeadline
// accepts a deadline but never interrupts a read.
func openPTY() (*os.File, *os.File, error) {
	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("opening /dev/ptmx: %w", err)
	}
	var unlock int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		syscall.Close(fd)
		return nil, nil, fmt.Errorf("unlocking pseudo-terminal: %w", errno)
	}
	var num uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		syscall.TIOCGPTN, uintptr(unsafe.Pointer(&num))); errno != 0 {
		syscall.Close(fd)
		return nil, nil, fmt.Errorf("reading pseudo-terminal number: %w", errno)
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return nil, nil, fmt.Errorf("setting non-blocking mode: %w", err)
	}
	leader := os.NewFile(uintptr(fd), "/dev/ptmx")
	name := fmt.Sprintf("/dev/pts/%d", num)
	follower, err := os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		leader.Close()
		return nil, nil, fmt.Errorf("opening %s: %w", name, err)
	}
	return leader, follower, nil
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
