//go:build !windows

package localenv

import (
	"errors"
	"os/exec"
	"syscall"
)

const supported = true

func shellCommand(script string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", script)
}

// setProcessGroup makes the command lead its own process group, so stopping
// the environment reaches every server it spawned and nothing else.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 0 {
		return errors.New("invalid process group")
	}
	return syscall.Kill(-pgid, sig)
}

// groupAlive reports whether any process is left in the group. A group id is
// not reused while one of its members exists, so a live group is still ours.
func groupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func processGroup(pid int) int {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return 0
	}
	return pgid
}
