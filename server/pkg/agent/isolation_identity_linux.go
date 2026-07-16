//go:build linux

package agent

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func openPathIdentityLinux(path string, directory bool) (*os.File, os.FileInfo, error) {
	flags := unix.O_CLOEXEC | unix.O_NOFOLLOW
	if directory {
		flags |= unix.O_PATH | unix.O_DIRECTORY
	} else {
		flags |= unix.O_RDONLY | unix.O_NONBLOCK
	}

	fd, err := openat2NoSymlinks(unix.AT_FDCWD, path, flags)
	if err != nil {
		if !errors.Is(err, unix.ENOSYS) {
			return nil, nil, err
		}
		fd, err = openPathNoSymlinksFallback(path, flags)
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
	if !directory && !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("path %q is not a regular file", path)
	}
	return file, info, nil
}

// openPathNoSymlinksFallback provides the same no-symlink property on kernels
// without openat2 by resolving every component relative to an already-open
// directory descriptor. A single unix.Open(path, O_NOFOLLOW) would leave all
// ancestor components exposed to replacement and symlink traversal.
func openPathNoSymlinksFallback(path string, finalFlags int) (int, error) {
	if !strings.HasPrefix(path, "/") {
		return -1, unix.EINVAL
	}
	current, err := unix.Open("/", unix.O_CLOEXEC|unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		return -1, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return current, nil
	}
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			_ = unix.Close(current)
			return -1, unix.EINVAL
		}
		flags := unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_PATH | unix.O_DIRECTORY
		if i == len(parts)-1 {
			flags = finalFlags
		}
		next, openErr := unix.Openat(current, part, flags, 0)
		_ = unix.Close(current)
		if openErr != nil {
			return -1, openErr
		}
		current = next
	}
	return current, nil
}

func openat2NoSymlinks(dirfd int, path string, flags int) (int, error) {
	how := &unix.OpenHow{
		Flags:   uint64(flags),
		Mode:    0,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	}
	return unix.Openat2(dirfd, path, how)
}
