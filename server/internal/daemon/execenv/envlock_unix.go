//go:build !windows

package execenv

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFileExclusiveNonBlocking takes an exclusive advisory lock on f without
// waiting. ok is false when another process already holds it.
//
// The lock is released by the kernel when the file is closed OR when the
// holding process dies, which is the whole reason this is a lock and not
// another marker file: it answers "is the previous execution still alive?"
// without a heartbeat, a PID table, or a stale-state cleanup path.
func openLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
}

func lockFileExclusiveNonBlocking(f *os.File) (ok bool, err error) {
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if err == unix.EWOULDBLOCK {
		return false, nil
	}
	return false, err
}

// unlockFile drops the advisory lock. Closing the file would do it too; this
// makes the release explicit at the call site.
func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
