//go:build windows

package managedruntime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func switchCurrent(providerRoot, versionDir string) error {
	current := filepath.Join(providerRoot, "current")
	temporary := filepath.Join(providerRoot, ".current-next")
	backup := filepath.Join(providerRoot, ".current-previous")
	_ = os.Remove(temporary)
	_ = os.Remove(backup)
	if err := createDirectoryLink(versionDir, temporary); err != nil {
		return err
	}
	hadCurrent := false
	if _, err := os.Lstat(current); err == nil {
		hadCurrent = true
		if err := os.Rename(current, backup); err != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("preserve previous current junction: %w", err)
		}
	} else if !os.IsNotExist(err) {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, current); err != nil {
		if hadCurrent {
			_ = os.Rename(backup, current)
		}
		_ = os.Remove(temporary)
		return fmt.Errorf("activate current junction: %w", err)
	}
	if hadCurrent {
		_ = os.Remove(backup)
	}
	return nil
}

func createDirectoryLink(target, link string) error {
	if err := os.Symlink(target, link); err == nil {
		return nil
	}
	// Directory junctions work without Developer Mode or administrator access.
	// Both paths are locally derived and quoted so cmd metacharacters in a user
	// profile path remain literal.
	command := `mklink /J "` + link + `" "` + target + `"`
	out, err := exec.Command("cmd", "/d", "/s", "/c", command).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create directory junction: %s: %w", out, err)
	}
	return nil
}
