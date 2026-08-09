//go:build windows

package terminal

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

var errPTYUnsupported = errors.New("web PTY is not supported on Windows")

func startPTY(_ *exec.Cmd, _, _ uint16) (ptyHandle, error) { return nil, errPTYUnsupported }

func terminateProcessTree(process *os.Process, _ time.Duration) error {
	if process == nil {
		return nil
	}
	return process.Kill()
}

func isPTYClosedError(err error) bool { return errors.Is(err, os.ErrClosed) }
