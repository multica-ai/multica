//go:build !windows

package execenv

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type preparationProcessController struct{}

func newPreparationProcessController(cmd *exec.Cmd) (*preparationProcessController, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return &preparationProcessController{}, nil
}

func (*preparationProcessController) attach(_ *exec.Cmd) error { return nil }

func (*preparationProcessController) stop(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	// Kill the helper and any CLI it spawned. After SIGKILL is pending, a
	// helper blocked in a kernel filesystem call cannot return to Go and
	// perform another write when that call eventually unblocks.
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (*preparationProcessController) finish() error { return nil }
func (*preparationProcessController) close()        {}
