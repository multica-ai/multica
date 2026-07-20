//go:build !windows

package execenv

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configurePreparationCommand(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		// Kill the helper and any CLI it spawned. After SIGKILL is pending, a
		// helper blocked in a kernel filesystem call cannot return to Go and
		// perform another write when that call eventually unblocks.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
