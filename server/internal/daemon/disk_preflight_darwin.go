//go:build darwin

package daemon

import "syscall"

func filesystemFreeGiB(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize) / (1 << 30), nil
}
