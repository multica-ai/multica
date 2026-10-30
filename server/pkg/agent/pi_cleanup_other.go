//go:build !linux && !darwin

package agent

import (
	"fmt"
	"os/exec"
	"syscall"
)

type piGroupProcessTracker struct{ cmd *exec.Cmd }

func (*piGroupProcessTracker) unobservedTools() bool { return false }

func newPiProcessTracker(cmd *exec.Cmd) piProcessTracker { return &piGroupProcessTracker{cmd: cmd} }
func (*piGroupProcessTracker) snapshot(bool) error       { return nil }
func (*piGroupProcessTracker) close()                    {}
func (p *piGroupProcessTracker) signal(sig syscall.Signal) error {
	signalProcessGroup(p.cmd, sig)
	return nil
}
func (p *piGroupProcessTracker) remaining() ([]int, error) {
	if waitProcessGroupGone(p.cmd, 0) {
		return nil, nil
	}
	return nil, fmt.Errorf("runtime process-tree cleanup could not be confirmed")
}
