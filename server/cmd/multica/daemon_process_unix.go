//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func setBackgroundProcessAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func daemonNotifySignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM}
}

func daemonShutdownSignal() os.Signal {
	return syscall.SIGTERM
}
