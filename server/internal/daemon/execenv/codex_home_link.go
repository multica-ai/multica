//go:build !windows

package execenv

import (
	"log/slog"
	"os"
)

func createDirLink(src, dst string) error {
	return os.Symlink(src, dst)
}

// createFileLink symlinks src at dst. The logger is unused on this platform:
// only the Windows fallback chain has a copy degradation to report.
func createFileLink(src, dst string, _ *slog.Logger) error {
	return os.Symlink(src, dst)
}
