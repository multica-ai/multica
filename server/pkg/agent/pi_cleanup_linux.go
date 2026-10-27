//go:build linux

package agent

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

type piLinuxProcessIdentity struct{ fd int }

func piProcessMissing(_ int, err error) bool {
	return errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrNotExist)
}

func capturePiProcessIdentity(info cursorUnixProcessInfo) (piProcessIdentity, error) {
	fd, err := unix.PidfdOpen(info.pid, 0)
	if err != nil {
		return nil, err
	}
	current, err := readCursorUnixProcessInfo(info.pid)
	if err != nil || current.start != info.start {
		_ = unix.Close(fd)
		if piProcessMissing(info.pid, err) {
			return nil, syscall.ESRCH
		}
		return nil, fmt.Errorf("process identity changed during capture: %d", info.pid)
	}
	return &piLinuxProcessIdentity{fd: fd}, nil
}

func (p *piLinuxProcessIdentity) signal(sig syscall.Signal) error {
	return unix.PidfdSendSignal(p.fd, unix.Signal(sig), nil, 0)
}
func (p *piLinuxProcessIdentity) close() { _ = unix.Close(p.fd) }
