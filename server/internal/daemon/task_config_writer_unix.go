//go:build !windows

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// taskConfigWriter keeps directory descriptors open from the workdir root to
// the destination parent. All writes and the no-replace publish therefore use
// *at operations and cannot be redirected by a pathname symlink swap after the
// initial validation.
type taskConfigWriter struct {
	dir        *os.File
	file       *os.File
	tempName   string
	targetName string
}

func openTaskConfigWriter(envRoot, workDir, rel, target, tempPath string) (*taskConfigWriter, error) {
	envRoot, err := filepath.Abs(envRoot)
	if err != nil {
		return nil, errors.New("task_config: invalid environment root")
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return nil, errors.New("task_config: invalid workdir")
	}
	workRel, err := filepath.Rel(envRoot, workDir)
	if err != nil || !safeTaskConfigRelativePath(workRel) {
		return nil, errors.New("task_config: workdir escapes environment root")
	}
	rel = filepath.ToSlash(rel)
	if !safeTaskConfigRelativePath(rel) {
		return nil, errors.New("task_config: invalid destination")
	}
	expectedTarget := filepath.Join(workDir, filepath.FromSlash(rel))
	if absolute, absErr := filepath.Abs(target); absErr != nil || absolute != expectedTarget {
		return nil, errors.New("task_config: invalid destination")
	}
	parentRel := filepath.Dir(rel)
	expectedTemp := filepath.Join(filepath.Dir(expectedTarget), filepath.Base(tempPath))
	if absolute, absErr := filepath.Abs(tempPath); absErr != nil || absolute != expectedTemp {
		return nil, errors.New("task_config: invalid temporary destination")
	}

	fd, err := unix.Open(envRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("task_config: open environment root failed")
	}
	closeFD := true
	defer func() {
		if closeFD {
			_ = unix.Close(fd)
		}
	}()
	for _, part := range append(pathParts(workRel), pathParts(parentRel)...) {
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			if openErr != unix.ENOENT || unix.Mkdirat(fd, part, 0o700) != nil {
				return nil, errors.New("task_config: open destination parent failed")
			}
			next, openErr = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if openErr != nil {
				return nil, errors.New("task_config: open destination parent failed")
			}
		}
		_ = unix.Close(fd)
		fd = next
	}
	tempName := filepath.Base(tempPath)
	targetName := filepath.Base(expectedTarget)
	tempFD, err := unix.Openat(fd, tempName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, errors.New("task_config: create temporary destination failed")
	}
	closeFD = false
	return &taskConfigWriter{
		dir:        os.NewFile(uintptr(fd), filepath.Dir(expectedTarget)),
		tempName:   tempName,
		targetName: targetName,
		file:       os.NewFile(uintptr(tempFD), tempPath),
	}, nil
}

func pathParts(path string) []string {
	path = filepath.ToSlash(path)
	if path == "." || path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func (w *taskConfigWriter) Close() {
	if w != nil && w.dir != nil {
		_ = w.dir.Close()
		w.dir = nil
	}
}

func (w *taskConfigWriter) Publish() error {
	if w == nil || w.dir == nil || w.file != nil {
		return errors.New("task_config: writer is not ready")
	}
	defer w.Close()
	if err := unix.Linkat(int(w.dir.Fd()), w.tempName, int(w.dir.Fd()), w.targetName, 0); err != nil {
		return errors.New("task_config: publish destination failed")
	}
	if err := unix.Unlinkat(int(w.dir.Fd()), w.tempName, 0); err != nil {
		return errors.New("task_config: remove temporary destination failed")
	}
	return nil
}
