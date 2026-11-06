//go:build !windows

package filelock

import (
	"os"

	"golang.org/x/sys/unix"
)

// Open opens or creates a lock file.
func Open(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
}

// TryFile takes an exclusive advisory lock on f without
// waiting. ok is false when another process already holds it.
//
// The lock is released by the kernel when the file is closed OR when the
// holding process dies, which is the whole reason this is a lock and not
// another marker file: it answers "is the previous execution still alive?"
// without a heartbeat, a PID table, or a stale-state cleanup path.
func TryFile(f *os.File) (ok bool, err error) {
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if err == unix.EWOULDBLOCK {
		return false, nil
	}
	return false, err
}

// Unlock drops the advisory lock. Closing the file would do it too; this
// makes the release explicit at the call site.
func Unlock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
