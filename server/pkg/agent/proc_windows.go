//go:build windows

package agent

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// hideAgentWindow configures cmd to suppress the console window on Windows.
// CREATE_NO_WINDOW prevents window creation while preserving stdio pipes.
func hideAgentWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
