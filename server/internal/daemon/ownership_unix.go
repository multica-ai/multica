//go:build !windows

package daemon

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockExclusive takes a non-blocking exclusive advisory lock on the whole
// file via flock(2). flock is inherited across fork and, crucially, released
// by the kernel when the last descriptor closes — including on process death —
// so a crashed owner never leaves a lock behind. flock is purely advisory
// among flock callers and does not block read(2)/write(2), so readOwnerInfo can
// still read the body while another process holds the lock.
//
// Returns (true, nil) on success, (false, nil) when another process holds it
// (EWOULDBLOCK), and (false, err) on any other failure.
func tryLockExclusive(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}

// unlock releases the flock held on f.
func unlock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
