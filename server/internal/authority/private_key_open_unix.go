//go:build darwin || linux

package authority

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openPrivateKeyFileNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, errors.New("authority private key file must not be a symlink")
		}
		return nil, fmt.Errorf("open authority private key file without following symlinks: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open authority private key file: invalid file descriptor")
	}
	return file, nil
}
