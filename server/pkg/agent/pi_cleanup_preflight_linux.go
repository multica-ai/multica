//go:build linux

package agent

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func checkPiCleanupSupport() error {
	if err := probePiPidfd(unix.PidfdOpen, unix.PidfdSendSignal); err != nil {
		return fmt.Errorf("this Linux environment cannot safely clean up Pi/omp processes; requires Linux 5.3+ with pidfd_open and pidfd_send_signal permitted: %w", err)
	}
	return nil
}

// probePiPidfd exercises both syscalls cleanup relies on against the daemon
// itself; signal 0 performs only the permission check.
func probePiPidfd(
	open func(pid int, flags int) (int, error),
	send func(fd int, sig unix.Signal, info *unix.Siginfo, flags int) error,
) error {
	fd, err := open(os.Getpid(), 0)
	if err != nil {
		return fmt.Errorf("pidfd_open: %w", err)
	}
	defer unix.Close(fd)
	if err := send(fd, 0, nil, 0); err != nil {
		return fmt.Errorf("pidfd_send_signal: %w", err)
	}
	return nil
}
