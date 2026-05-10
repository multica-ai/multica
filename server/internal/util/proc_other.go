//go:build !windows

package util

import "os/exec"

// HideConsoleWindow is a no-op on non-Windows platforms.
func HideConsoleWindow(cmd *exec.Cmd) {}
