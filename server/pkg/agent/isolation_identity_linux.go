//go:build linux

package agent

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openPathIdentityLinux(path string, directory bool) (*os.File, os.FileInfo, error) {
	flags := unix.O_CLOEXEC | unix.O_NOFOLLOW
	if directory {
		flags |= unix.O_PATH | unix.O_DIRECTORY
	} else {
		flags |= unix.O_RDONLY
	}

	fd, err := openat2NoSymlinks(unix.AT_FDCWD, path, flags)
	if err != nil {
		// Fall back for kernels without openat2 while still rejecting final
		// symlink components via O_NOFOLLOW.
		fd, err = unix.Open(path, flags, 0)
		if err != nil {
			return nil, nil, err
		}
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, nil, fmt.Errorf("adopt path identity descriptor for %q", path)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if directory && !info.IsDir() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("path %q is not a directory", path)
	}
	if !directory && info.IsDir() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("path %q is a directory", path)
	}
	return file, info, nil
}

func openat2NoSymlinks(dirfd int, path string, flags int) (int, error) {
	how := &unix.OpenHow{
		Flags:   uint64(flags),
		Mode:    0,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	}
	return unix.Openat2(dirfd, path, how)
}
