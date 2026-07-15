//go:build linux

package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type piSessionFile struct {
	file *os.File
}

func (s *piSessionFile) Close() error {
	if s == nil || s.file == nil {
		return nil
	}
	return s.file.Close()
}

func (s *piSessionFile) childPath() string {
	return "/proc/self/fd/3"
}

func createPiSessionFile(taskTempDir, sessionID string) (*piSessionFile, error) {
	rootFD, err := unix.Open(taskTempDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open task temp directory: %w", err)
	}
	defer unix.Close(rootFD)

	const sessionDir = "pi-sessions"
	if err := unix.Mkdirat(rootFD, sessionDir, 0o700); err != nil && err != unix.EEXIST {
		return nil, fmt.Errorf("create session directory: %w", err)
	}
	sessionDirFD, err := unix.Openat(rootFD, sessionDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open session directory: %w", err)
	}
	defer unix.Close(sessionDirFD)

	name := sessionID + ".jsonl"
	fileFD, err := unix.Openat(sessionDirFD, name, unix.O_RDWR|unix.O_APPEND|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open session file: %w", err)
	}
	owned := true
	defer func() {
		if owned {
			_ = unix.Close(fileFD)
		}
	}()

	var stat unix.Stat_t
	if err := unix.Fstat(fileFD, &stat); err != nil {
		return nil, fmt.Errorf("stat session file: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, fmt.Errorf("session file is not a regular file")
	}
	if stat.Nlink != 1 {
		return nil, fmt.Errorf("session file link count is %d, want 1", stat.Nlink)
	}

	file := os.NewFile(uintptr(fileFD), filepath.Join(taskTempDir, sessionDir, name))
	if file == nil {
		return nil, fmt.Errorf("adopt session file descriptor")
	}
	owned = false
	return &piSessionFile{file: file}, nil
}
