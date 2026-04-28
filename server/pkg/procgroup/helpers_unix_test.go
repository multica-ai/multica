//go:build !windows

package procgroup

import "os/exec"

// overrideForWindowsTrue is a Unix no-op; the cross-platform
// "true" command in test code already exists on POSIX systems.
func overrideForWindowsTrue(_ *exec.Cmd) error { return nil }
