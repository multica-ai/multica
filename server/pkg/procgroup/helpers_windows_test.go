//go:build windows

package procgroup

import "os/exec"

// overrideForWindowsTrue rewrites cmd to be the Windows equivalent
// of POSIX 'true' (immediate successful exit) so cross-platform
// tests can be written using cmd := exec.Command("true").
func overrideForWindowsTrue(cmd *exec.Cmd) error {
	cmd.Path = "cmd.exe"
	cmd.Args = []string{"cmd.exe", "/c", "exit 0"}
	return nil
}
