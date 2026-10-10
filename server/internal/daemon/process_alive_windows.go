//go:build windows

package daemon

import (
	"errors"

	"golang.org/x/sys/windows"
)

// processAlive opens a synchronizable process handle and checks whether it has
// exited without waiting. Access-denied and unexpected results fail safe to
// alive; ERROR_INVALID_PARAMETER is how OpenProcess reports a vanished pid.
func processAlive(pid int) bool {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer windows.CloseHandle(process)

	status, err := windows.WaitForSingleObject(process, 0)
	if err != nil {
		return true
	}
	return status != windows.WAIT_OBJECT_0
}
