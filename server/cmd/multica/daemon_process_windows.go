//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

const detachedProcess = 0x00000008

func setBackgroundProcessAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
}

func daemonNotifySignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func daemonShutdownSignal() os.Signal {
	return os.Kill
}
