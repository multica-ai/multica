//go:build !windows

package agent

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// CheckPrimeAdmission is deliberately an operator attestation, not a sandbox
// detector. Prime executes model-generated code with its current OS identity.
func CheckPrimeAdmission() error {
	if !primePeerCredentialsSupported() {
		return errors.New("Prime Agent disabled: kernel-authenticated Unix peer PID credentials are unavailable on this platform")
	}
	if os.Getenv("MULTICA_PRIME_AGENT_ISOLATED") != "1" {
		return errors.New("Prime Agent disabled: set MULTICA_PRIME_AGENT_ISOLATED=1 only inside an isolated container or restricted OS identity")
	}
	if os.Geteuid() == 0 {
		return errors.New("Prime Agent disabled: the Multica daemon must run as a non-root OS identity")
	}
	return nil
}

func validatePrimeSocketOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("Prime daemon socket is not owned by the Multica OS identity")
	}
	return nil
}

func primeSupervisorIdentity(pid int) (int, error) { return syscall.Getpgid(pid) }

func primeSupervisorGone(pid, expectedPGID int) bool {
	pgid, err := syscall.Getpgid(pid)
	return errors.Is(err, syscall.ESRCH) || (err == nil && pgid != expectedPGID)
}

func forcePrimeSupervisor(pid, expectedPGID int) error {
	pgid, err := syscall.Getpgid(pid)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil || pgid != expectedPGID {
		return fmt.Errorf("supervisor process identity changed")
	}
	if pgid == pid {
		return syscall.Kill(-pid, syscall.SIGKILL)
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
