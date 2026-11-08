//go:build windows

package agent

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Hermes gateway.status reserves one byte at 1 MiB on Windows. Probe the
// reservation rather than the PID record: stale records survive a killed job,
// and the reservation precedes control endpoint publication in Hermes 0.22.
func hermesGatewayReserved(home string) (bool, error) {
	path := filepath.Join(home, "gateway.lock")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("refusing non-local Hermes gateway lock")
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false, err
	}
	defer f.Close()
	position := windows.Overlapped{Offset: 1024 * 1024}
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &position)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &position)
}
