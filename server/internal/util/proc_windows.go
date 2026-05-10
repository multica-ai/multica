//go:build windows

package util

import (
	"os/exec"
	"syscall"
)

// createNewConsole allocates a fresh console for the child process. Combined
// with HideWindow=true (STARTF_USESHOWWINDOW + SW_HIDE) the console window
// stays off-screen, and any grandchildren inherit this hidden console instead
// of each allocating their own visible one.
const createNewConsole = 0x00000010

// HideConsoleWindow configures cmd to suppress the console window on Windows
// while still giving descendant processes a hidden console to inherit.
func HideConsoleWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNewConsole
}
