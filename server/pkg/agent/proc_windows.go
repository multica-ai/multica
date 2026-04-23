//go:build windows

package agent

import (
	"os/exec"
	"syscall"
)

// hideAgentWindow suppresses the console window that Windows creates when
// spawning a child process. Without this, every agent invocation (claude,
// codex, etc.) opens a visible cmd window that steals desktop focus.
func hideAgentWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
