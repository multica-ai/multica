//go:build darwin

package agent

import (
	"errors"
	"fmt"
	"syscall"
)

type piDarwinProcessIdentity struct {
	pid    int
	unique uint64
}

func checkPiCleanupSupport() error {
	if err := probePiIdentitySignalling(signalCursorDarwinIdentity); err != nil {
		return fmt.Errorf("this macOS kernel cannot safely clean up Pi/omp processes; use a macOS kernel with process identity signalling support: %w", err)
	}
	return nil
}

func piProcessMissing(pid int, err error) bool {
	if errors.Is(err, syscall.ESRCH) {
		return true
	}
	// sysctl returns EIO for a PID that exited between enumeration and lookup.
	if errors.Is(err, syscall.EIO) {
		_, identityErr := readCursorDarwinIdentity(pid)
		return errors.Is(identityErr, syscall.ESRCH)
	}
	return false
}

func capturePiProcessIdentity(info cursorUnixProcessInfo) (piProcessIdentity, error) {
	identity, err := readCursorDarwinIdentity(info.pid)
	if err != nil {
		return nil, err
	}
	current, err := readCursorUnixProcessInfo(info.pid)
	if piProcessMissing(info.pid, err) {
		return nil, syscall.ESRCH
	}
	if err != nil || current.start != info.start {
		return nil, fmt.Errorf("process identity changed during capture: %d", info.pid)
	}
	// Probe the kernel-checked signalling operation; no numeric-PID fallback.
	if err := probePiIdentitySignalling(signalCursorDarwinIdentity); err != nil {
		return nil, err
	}
	return &piDarwinProcessIdentity{pid: info.pid, unique: identity.unique}, nil
}

func (p *piDarwinProcessIdentity) signal(sig syscall.Signal) error {
	identity, err := readCursorDarwinIdentity(p.pid)
	if err != nil {
		return err
	}
	if identity.unique != p.unique {
		return syscall.ESRCH
	}
	// exec changes pidversion but preserves the birth identity. Use the current
	// version only after proving it is still the process we originally owned.
	return signalCursorDarwinIdentity(p.pid, identity.version, sig)
}
func (*piDarwinProcessIdentity) close() {}
