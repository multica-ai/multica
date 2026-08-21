//go:build windows

package execenv

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockFileExclusiveNonBlocking takes an exclusive lock on f without waiting.
// ok is false when another process already holds it.
//
// Windows releases the lock when the handle closes, including on abnormal
// process termination, giving the same crash-safe liveness answer as flock on
// unix — see the unix build of this file for why that property matters.
// openEnvRootLockFile opens (creating if needed) the lock file with
// FILE_SHARE_DELETE, which os.OpenFile does not request.
//
// Without it Windows refuses to delete a file that anyone still has open, so a
// held lock would pin the whole env root: the GC could not reclaim the
// directory, and neither could a test's temp-dir teardown. Unix already allows
// unlinking an open file, and this is what makes Windows behave the same. The
// lock coordinates executions; it is not meant to pin the directory.
func openEnvRootLockFile(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(
		p,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

func lockFileExclusiveNonBlocking(f *os.File) (ok bool, err error) {
	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, overlapped,
	)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return false, nil
	}
	return false, err
}

// unlockFile drops the lock.
func unlockFile(f *os.File) error {
	overlapped := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, overlapped)
}
