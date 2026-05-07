//go:build !windows

package agent

import "os/exec"

// hideAgentWindow is a no-op on non-Windows platforms.
func hideAgentWindow(cmd *exec.Cmd) {}
