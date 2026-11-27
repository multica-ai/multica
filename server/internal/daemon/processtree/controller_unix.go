//go:build !windows

package processtree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

const processTreeFinishTimeout = 5 * time.Second

type controller struct{}

func newController(cmd *exec.Cmd) (*controller, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return &controller{}, nil
}

func (*controller) attach(_ *exec.Cmd) error { return nil }

func (*controller) interrupt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) || onlyExitingMembers(err) {
			return nil
		}
		return err
	}
	return nil
}

func (*controller) stop(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) || onlyExitingMembers(err) {
			return nil
		}
		return cmd.Process.Kill()
	}
	return nil
}

func (c *controller) finish(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
		return nil
	}
	// A normally-exited leader can still leave a descendant holding inherited
	// pipes or repository locks. Kill the remaining group before returning
	// ownership of the repository to another operation.
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) && !onlyExitingMembers(err) {
		return err
	}
	deadline := time.Now().Add(processTreeFinishTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = syscall.Kill(-pid, 0)
		if errors.Is(lastErr, syscall.ESRCH) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("process group %d still active after %s: %w", pid, processTreeFinishTimeout, lastErr)
	}
	return fmt.Errorf("process group %d still active after %s", pid, processTreeFinishTimeout)
}

// onlyExitingMembers reports whether a kill(-pgid, ...) error means the group
// still exists but none of its members can take a signal any more. Darwin
// skips members that are exiting or zombies and then returns EPERM rather than
// ESRCH while the group lingers; the group disappears once they are reaped.
func onlyExitingMembers(err error) bool {
	return runtime.GOOS == "darwin" && errors.Is(err, syscall.EPERM)
}

func (*controller) close() {}
