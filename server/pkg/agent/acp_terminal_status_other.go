//go:build !windows

package agent

import (
	"os"
	"syscall"
)

func acpProcessExitSignal(state *os.ProcessState) *string {
	if state == nil {
		return nil
	}
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return nil
	}
	signal := status.Signal().String()
	return &signal
}
