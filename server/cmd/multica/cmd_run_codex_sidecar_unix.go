//go:build !windows

package main

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func prepareCodexSidecarCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func stopCodexSidecarCommand(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return
	}
	signalCodexSidecar(cmd, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		signalCodexSidecar(cmd, syscall.SIGKILL)
		<-done
	}
}

func signalCodexSidecar(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid > 0 {
		if err := syscall.Kill(-pid, sig); err == nil || errors.Is(err, syscall.ESRCH) {
			return
		}
	}
	_ = cmd.Process.Signal(sig)
}
