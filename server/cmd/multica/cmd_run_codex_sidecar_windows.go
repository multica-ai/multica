//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const codexCreateNewProcessGroup = 0x00000200

func prepareCodexSidecarCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= codexCreateNewProcessGroup
}

func stopCodexSidecarCommand(cmd *exec.Cmd) {
	stopCommand(cmd)
}
