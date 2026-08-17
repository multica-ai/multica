//go:build windows

package agent

import (
	"errors"
	"os"
)

var ErrPrimeUpstreamSchedulerUnsafe = errors.New("Prime Agent v0.7.2 is disabled: upstream provides no startup hard-disable for persisted schedules/heartbeats")

func CheckPrimeAdmission() error {
	return errors.New("Prime Agent disabled: managed isolation admission is not implemented on Windows")
}

func validatePrimeSocketOwner(os.FileInfo) error {
	return errors.New("Prime daemon sockets are unsupported on Windows")
}
func primeSupervisorIdentity(int) (int, error) {
	return 0, errors.New("Prime daemon supervisors are unsupported on Windows")
}
func primeSupervisorGone(int, int, string) bool { return false }
func forcePrimeSupervisor(int, int, string) error {
	return errors.New("Prime daemon supervisors are unsupported on Windows")
}
