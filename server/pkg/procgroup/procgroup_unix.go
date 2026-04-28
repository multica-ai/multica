//go:build !windows

package procgroup

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// platJob holds Unix-specific state. The pgid is captured at
// Attach time so Close can target it even after the leader exits.
type platJob struct {
	pgid int // 0 until Attach
}

// newPlatJob is a no-op on Unix; the actual group is created by
// the kernel at fork time when Setpgid is set.
func newPlatJob() (platJob, error) {
	return platJob{}, nil
}

// prepareCmd sets Setpgid so the child becomes its own pgroup leader.
func (p *platJob) prepareCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// attach captures the pgid (which equals the leader's pid) so close
// can SIGKILL the whole group later.
func (p *platJob) attach(proc *os.Process) error {
	p.pgid = proc.Pid
	return nil
}

// close sends SIGKILL to -pgid (the entire process group). ESRCH
// is silently ignored — it just means the group is already gone.
func (p *platJob) close() error {
	if p.pgid == 0 {
		return nil
	}
	pgid := p.pgid
	p.pgid = 0
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}
