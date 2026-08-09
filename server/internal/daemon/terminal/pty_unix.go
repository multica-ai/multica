//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package terminal

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
)

type unixPTY struct{ *os.File }

func (p unixPTY) Resize(cols, rows uint16) error {
	return pty.Setsize(p.File, &pty.Winsize{Cols: cols, Rows: rows})
}

func startPTY(cmd *exec.Cmd, cols, rows uint16) (ptyHandle, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	return unixPTY{File: f}, nil
}

func terminateProcessTree(process *os.Process, grace time.Duration) error {
	if process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(process.Pid)
	if err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return process.Kill()
	}
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if grace <= 0 {
		return nil
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	<-timer.C
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func isPTYClosedError(err error) bool {
	return errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed)
}
