package qoderruntime

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
)

func lockState(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{}); err != nil {
		f.Close()
		return nil, fmt.Errorf("bridge state is already locked: %w", err)
	}
	return f, nil
}

// Windows does not support fsync on directory handles opened by os.Open.
func syncStateDir(string) error { return nil }
