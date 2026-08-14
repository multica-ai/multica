//go:build windows

package agent

import (
	"errors"
	"os"
)

func CheckPrimeAdmission() error {
	return errors.New("Prime Agent disabled: managed isolation admission is not implemented on Windows")
}

func validatePrimeSocketOwner(os.FileInfo) error {
	return errors.New("Prime daemon sockets are unsupported on Windows")
}
func primeSupervisorIdentity(int) (int, error) {
	return 0, errors.New("Prime daemon supervisors are unsupported on Windows")
}
func primeSupervisorGone(int, int) bool { return false }
func forcePrimeSupervisor(int, int) error {
	return errors.New("Prime daemon supervisors are unsupported on Windows")
}
