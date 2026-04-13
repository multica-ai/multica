//go:build windows

package execenv

import (
	"fmt"
	"os"
	"os/exec"
)

// createDirLink tries os.Symlink first (requires Developer Mode or admin on
// Windows). If that fails, it falls back to a directory junction (mklink /J)
// which works without elevated privileges.
func createDirLink(src, dst string) error {
	if err := os.Symlink(src, dst); err == nil {
		return nil
	}
	out, err := exec.Command("cmd", "/c", "mklink", "/J", dst, src).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %s %s: %s: %w", dst, src, out, err)
	}
	return nil
}

// createFileLink tries os.Symlink first. If that fails, it falls back to
// copying the file so the content is still available.
func createFileLink(src, dst string) error {
	if err := os.Symlink(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}
