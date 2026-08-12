//go:build !windows

package corpustransfer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func walkSourceRoot(root string, visit func(string, os.FileInfo, *os.File) error) error {
	before, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !before.IsDir() {
		return fmt.Errorf("source root is not a directory: %s", root)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open source root without following links: %w", err)
	}
	dir := os.NewFile(uintptr(fd), root)
	if dir == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open source root: invalid file descriptor")
	}
	defer dir.Close()
	opened, err := dir.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, opened) || !sameFileSnapshot(before, opened) {
		return fmt.Errorf("source root changed before traversal: %s", root)
	}
	return walkOpenedSourceDir(root, "", dir, visit)
}

func walkOpenedSourceDir(root, rel string, dir *os.File, visit func(string, os.FileInfo, *os.File) error) error {
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		childRel := filepath.Join(rel, entry.Name())
		filename := filepath.Join(root, childRel)
		fd, err := unix.Openat(int(dir.Fd()), entry.Name(), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			if entry.Type()&os.ModeSymlink != 0 || errors.Is(err, unix.ELOOP) {
				return fmt.Errorf("source contains symlink %s", filename)
			}
			return fmt.Errorf("open source entry %s without following links: %w", filename, err)
		}
		child := os.NewFile(uintptr(fd), filename)
		if child == nil {
			_ = unix.Close(fd)
			return fmt.Errorf("open source entry %s: invalid file descriptor", filename)
		}
		info, statErr := child.Stat()
		if statErr != nil {
			_ = child.Close()
			return statErr
		}
		if info.IsDir() {
			err = walkOpenedSourceDir(root, childRel, child, visit)
		} else if info.Mode().IsRegular() {
			err = visit(childRel, info, child)
		} else if info.Mode()&os.ModeSymlink != 0 {
			err = fmt.Errorf("source contains symlink %s", filename)
		} else {
			err = fmt.Errorf("source contains non-regular file %s", filename)
		}
		closeErr := child.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func openSourceNoFollow(filename string) (*os.File, error) {
	fd, err := unix.Open(filename, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filename)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("invalid file descriptor")
	}
	return file, nil
}
