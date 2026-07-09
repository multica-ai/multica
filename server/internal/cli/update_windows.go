//go:build windows

package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

// oldBinarySuffix is appended to the previous executable while a new one is
// being installed. Windows refuses to overwrite a running .exe but allows
// renaming it, so we shuffle the running binary out of the way first.
const oldBinarySuffix = ".old"

// replaceBinary swaps the running executable for the freshly-downloaded one.
// Windows holds an exclusive handle on a running .exe, so the rename-over
// pattern used on Unix fails with "Access is denied". Instead:
//  1. Clear any stale leftover from a previous update.
//  2. Move the running executable aside to exePath+".old".
//  3. Rename the new binary into place.
//  4. If step 3 fails, restore the original so the user isn't stranded.
//
// The leftover .old file is cleaned up on next startup via
// CleanupStaleUpdateArtifacts.
func replaceBinary(tmpPath, exePath string) error {
	oldPath := exePath + oldBinarySuffix

	// Best-effort cleanup; if this fails (file still locked) the next Rename
	// will surface a useful error.
	_ = os.Remove(oldPath)

	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("move running binary aside: %w", err)
	}

	if err := os.Rename(tmpPath, exePath); err != nil {
		// Restore so the user isn't left without a multica.exe.
		if rerr := os.Rename(oldPath, exePath); rerr != nil {
			return fmt.Errorf("install new binary: %w (and failed to restore: %v)", err, rerr)
		}
		return fmt.Errorf("install new binary: %w", err)
	}

	return nil
}

// CleanupStaleUpdateArtifacts removes leftover `.old` binaries from previous
// updates. Windows can't delete a running .exe, so a prior update may have
// left one behind; once the user restarts, this call reclaims the space.
func CleanupStaleUpdateArtifacts() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	_ = os.Remove(exePath + oldBinarySuffix)
}
