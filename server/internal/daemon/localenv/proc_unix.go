//go:build !windows

package localenv

import (
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

const supported = true

func shellCommand(script string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", script)
}

// setProcessGroup makes the command lead its own session, and with it its own
// process group. The group is what stopping signals, so it reaches every
// server the command spawned and nothing else. The session is what a server
// keeps when a script moves it into a process group of its own to detach it,
// so detection still knows whose it is.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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

func signalProcess(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return errors.New("invalid process")
	}
	return syscall.Kill(pid, sig)
}

func processGroup(pid int) int {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return 0
	}
	return pgid
}

func processSession(pid int) int {
	sid, err := unix.Getsid(pid)
	if err != nil {
		return 0
	}
	return sid
}
