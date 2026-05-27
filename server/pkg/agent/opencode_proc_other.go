//go:build !unix

package agent

import "os/exec"

func prepareOpenCodeCommand(cmd *exec.Cmd) {}

func openCodeProcessGroupAlive(cmd *exec.Cmd) bool {
	return false
}

func terminateOpenCodeProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
