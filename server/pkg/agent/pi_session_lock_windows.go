//go:build windows

package agent

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryLockPiSessionFile is the Windows equivalent of flock(LOCK_EX|LOCK_NB).
// Windows byte-range locks are mandatory, so locking the transcript itself
// would prevent Pi from reading the file it is supposed to append to. Lock a
// sidecar instead. FILE_SHARE_DELETE keeps a held lock from pinning the
// session directory when cleanup races a cancelled execution.
func tryLockPiSessionFile(path string) (*os.File, bool, error) {
	lockPath := path + ".lock"
	p, err := windows.UTF16PtrFromString(lockPath)
	if err != nil {
		return nil, false, err
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
		return nil, false, err
	}
	f := os.NewFile(uintptr(h), lockPath)
	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, overlapped,
	)
	if err == nil {
		return f, true, nil
	}
	_ = f.Close()
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return nil, false, nil
	}
	return nil, false, err
}

func releasePiSessionFileLock(f *os.File) {
	if f == nil {
		return
	}
	overlapped := new(windows.Overlapped)
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, overlapped)
	_ = f.Close()
}
