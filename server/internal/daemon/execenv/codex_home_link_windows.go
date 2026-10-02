//go:build windows

package execenv

import (
	"fmt"
	"log/slog"
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

// createFileLink tries os.Symlink first. When symlink creation is denied (no
// Developer Mode, not elevated), it falls back to a hard link — zero-copy,
// needs no special privilege, same strategy as linkCodexRollout — which shares
// the source's bytes within one volume. Only when that fails too (typically
// source and task home on different volumes) does it copy the file, and the
// copy is logged so the per-task duplication stays visible to the operator.
func createFileLink(src, dst string, logger *slog.Logger) error {
	if err := os.Symlink(src, dst); err == nil {
		return nil
	}
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	if logger != nil {
		logger.Warn("execenv: file link degraded to a full copy (symlink denied, hard link unavailable)",
			"src", src,
			"dst", dst,
		)
	}
	return nil
}
