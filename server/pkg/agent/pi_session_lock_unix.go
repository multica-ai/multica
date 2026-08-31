//go:build !windows

package agent

import (
	"os"

	"golang.org/x/sys/unix"
)

// tryLockPiSessionFile takes a non-blocking exclusive lock on the transcript.
// The daemon can launch more than one task at a time, and Pi appends directly
// to this JSONL, so the lock must cover the complete child-process lifetime.
// The kernel releases it if the daemon dies.
func tryLockPiSessionFile(path string) (*os.File, bool, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, false, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if err == unix.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return f, true, nil
}

func releasePiSessionFileLock(f *os.File) {
	if f == nil {
		return
	}
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	_ = f.Close()
}
