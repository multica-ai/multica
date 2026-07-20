//go:build windows

package execenv

import "os/exec"

// CommandContext terminates the helper process on cancellation on Windows.
// Preparation only spawns bounded read-only OpenClaw discovery commands; the
// filesystem mutations themselves remain inside this directly-owned helper.
func configurePreparationCommand(_ *exec.Cmd) {}
