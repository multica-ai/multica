//go:build windows

package daemon

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockRegionOffsetHigh places the single locked byte at a very high file
// offset (0x40000000_00000000) that no lock-file content ever reaches, so the
// byte-range lock acts as a pure ownership token and never blocks another
// process from reading the JSON body in the low bytes (readOwnerInfo). This
// mirrors the whole-file, read-transparent semantics flock gives on Unix.
const lockRegionOffsetHigh = 0x40000000

// tryLockExclusive takes a non-blocking exclusive byte-range lock via
// LockFileEx. Windows releases LockFileEx locks when the handle closes or the
// process dies, so — like flock — a crashed owner leaves no lingering lock.
//
// Returns (true, nil) on success, (false, nil) when another process holds it
// (ERROR_LOCK_VIOLATION, the immediate-fail result of LOCKFILE_FAIL_IMMEDIATELY),
// and (false, err) on any other failure.
func tryLockExclusive(f *os.File) (bool, error) {
	ol := &windows.Overlapped{OffsetHigh: lockRegionOffsetHigh}
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, ol,
	)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return false, err
}

// unlock releases the byte-range lock held on f.
func unlock(f *os.File) error {
	ol := &windows.Overlapped{OffsetHigh: lockRegionOffsetHigh}
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
