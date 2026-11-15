//go:build windows

package agent

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

const piCanObserveRuntimeExit = true

func piRuntimeExited(cmd *exec.Cmd) bool {
	// exec.Cmd still holds the process handle, preventing PID reuse until Wait.
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	state, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && state == windows.WAIT_OBJECT_0
}
