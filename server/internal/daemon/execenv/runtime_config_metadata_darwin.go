package execenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// Existing-file metadata stays attached to the target inode because Darwin's
// exchangedata replacement swaps only file contents. Missing files retain the
// deliberately private 0600 mode.
func preserveRuntimeConfigFileMetadata(staged *os.File, sourcePath string, mode fs.FileMode) error {
	if sourcePath == "" {
		return staged.Chmod(mode)
	}
	// exchangedata swaps every data fork. Seed the staged file with the
	// original resource fork so the target receives an identical fork while
	// its ACLs, xattrs, flags, owner, and inode stay attached in place.
	const resourceFork = "com.apple.ResourceFork"
	for {
		size, err := unix.Getxattr(sourcePath, resourceFork, nil)
		if errors.Is(err, unix.ENOATTR) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect resource fork: %w", err)
		}
		value := make([]byte, size)
		n, err := unix.Getxattr(sourcePath, resourceFork, value)
		if err == unix.ERANGE {
			continue
		}
		if err != nil {
			return fmt.Errorf("read resource fork: %w", err)
		}
		if err := unix.Setxattr(staged.Name(), resourceFork, value[:n], 0); err != nil {
			return fmt.Errorf("copy resource fork: %w", err)
		}
		return nil
	}
}
