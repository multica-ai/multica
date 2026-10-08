//go:build windows

package agent

import "os"

func acpProcessExitSignal(*os.ProcessState) *string {
	return nil
}
