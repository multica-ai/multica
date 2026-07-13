//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func freeDiskBytes(path string) (freeBytes, totalBytes int64, err error) {
	if path == "" {
		return 0, 0, fmt.Errorf("disk pressure: path is required")
	}
	statPath := path
	if _, statErr := os.Stat(statPath); statErr != nil {
		statPath = filepath.Dir(statPath)
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(statPath, &stat); err != nil {
		return 0, 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), int64(stat.Blocks) * int64(stat.Bsize), nil
}
