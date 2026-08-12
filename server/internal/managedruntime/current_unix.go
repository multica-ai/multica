//go:build !windows

package managedruntime

import (
	"fmt"
	"os"
	"path/filepath"
)

func switchCurrent(providerRoot, versionDir string) error {
	current := filepath.Join(providerRoot, "current")
	temporary := filepath.Join(providerRoot, ".current-next")
	if err := os.Remove(temporary); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(versionDir, temporary); err != nil {
		return fmt.Errorf("create current symlink: %w", err)
	}
	if err := os.Rename(temporary, current); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace current symlink: %w", err)
	}
	return nil
}
