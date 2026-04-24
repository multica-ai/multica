//go:build windows

package agent

import (
	"os/exec"
	"syscall"
)

// CREATE_NEW_CONSOLE allocates a fresh console for the child. Combined with
// HideWindow=true (STARTF_USESHOWWINDOW + SW_HIDE) the console window stays
// off-screen, and — critically — any grandchildren the agent spawns (tool
// subprocesses like bash, cmd, netstat, findstr) inherit this hidden console
// instead of each allocating their own visible one.
//
// Using CREATE_NO_WINDOW here instead would strip the console entirely, which
// forces Windows to allocate a new console per grandchild — the popup storm
// tracked in #1521.
const createNewConsole = 0x00000010

// hideAgentWindow configures cmd so neither the agent nor its grandchildren
// flash a console window on Windows, while preserving stdio pipes.
func hideAgentWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNewConsole
}
