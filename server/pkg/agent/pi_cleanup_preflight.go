package agent

import (
	"errors"
	"fmt"
	"syscall"
)

func probePiIdentitySignalling(signal func(int, uint32, syscall.Signal) error) error {
	// An impossible PID tests the kernel operation without signalling a process.
	err := signal(1<<31-1, 0, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err == nil {
		return errors.New("kernel process identity signalling is unavailable: probe unexpectedly succeeded")
	}
	return fmt.Errorf("kernel process identity signalling is unavailable: %w", err)
}
