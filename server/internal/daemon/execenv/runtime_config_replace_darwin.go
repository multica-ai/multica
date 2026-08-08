package execenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// replaceRuntimeConfigFile uses exchangedata for an existing target. Darwin
// swaps all data forks atomically while leaving ownership, mode, ACLs, xattrs,
// flags, and object identity attached to the original inode. That is the native
// safe-save primitive and avoids trying to reconstruct protected ACL metadata.
func replaceRuntimeConfigFile(stagedPath, targetPath string) error {
	if _, err := os.Lstat(targetPath); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(stagedPath, targetPath)
	} else if err != nil {
		return fmt.Errorf("stat replace target: %w", err)
	}

	if err := unix.Exchangedata(stagedPath, targetPath, 0); err != nil {
		return fmt.Errorf("exchange staged runtime config data: %w", err)
	}
	if err := os.Remove(stagedPath); err != nil {
		removeErr := fmt.Errorf("remove exchanged runtime config staging file: %w", err)
		if rollbackErr := unix.Exchangedata(stagedPath, targetPath, 0); rollbackErr != nil {
			return errors.Join(removeErr, fmt.Errorf("restore original runtime config data: %w", rollbackErr))
		}
		return removeErr
	}
	return nil
}
