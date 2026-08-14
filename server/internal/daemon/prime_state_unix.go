//go:build !windows

package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func securePrimeStateDir(profileDir string, components ...string) (string, error) {
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return "", err
	}
	fd, err := unix.Open(profileDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", fmt.Errorf("open Prime profile state root safely: %w", err)
	}
	defer unix.Close(fd)
	path := profileDir
	for _, component := range components {
		if err := unix.Mkdirat(fd, component, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return "", fmt.Errorf("create Prime state component: %w", err)
		}
		next, err := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return "", fmt.Errorf("open Prime state component safely: %w", err)
		}
		var stat unix.Stat_t
		if err := unix.Fstat(next, &stat); err != nil {
			unix.Close(next)
			return "", err
		}
		if int(stat.Uid) != os.Geteuid() || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o077 != 0 {
			unix.Close(next)
			return "", errors.New("Prime state component must be a current-owner private 0700 directory")
		}
		unix.Close(fd)
		fd = next
		path = filepath.Join(path, component)
	}
	return path, nil
}
