//go:build !windows && !linux && !darwin

package agent

import (
	"errors"
	"os/exec"
)

func captureCursorBackgroundProcess(_ *exec.Cmd, _ int) (*cursorBackgroundProcess, error) {
	return nil, errors.New("cursor background process ownership is unsupported on this platform")
}
