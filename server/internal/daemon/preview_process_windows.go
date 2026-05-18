//go:build windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	windowsDetachedProcess  = 0x00000008
	windowsNewProcessGroup  = 0x00000200
	windowsBreakawayFromJob = 0x01000000
)

func makePreviewDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windowsDetachedProcess | windowsNewProcessGroup | windowsBreakawayFromJob,
	}
}

func previewProcessExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func killPreviewProcessGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
