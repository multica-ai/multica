package daemon

import (
	"fmt"
	"log/slog"
)

type diskPreflightState uint8

const (
	diskPreflightUnknown diskPreflightState = iota
	diskPreflightNormal
	diskPreflightWarning
	diskPreflightCritical
	diskPreflightError
)

type diskPreflight struct {
	path        string
	warningGiB  uint64
	criticalGiB uint64
	recoveryGiB uint64
	freeGiB     func(string) (uint64, error)
	logger      *slog.Logger
	state       diskPreflightState
}

func newDiskPreflight(cfg Config, logger *slog.Logger) *diskPreflight {
	return &diskPreflight{
		path:        cfg.WorkspacesRoot,
		warningGiB:  uint64(cfg.DiskWarningGiB),
		criticalGiB: uint64(cfg.DiskCriticalGiB),
		recoveryGiB: uint64(cfg.DiskRecoveryGiB),
		freeGiB:     filesystemFreeGiB,
		logger:      logger,
	}
}

// allowTaskClaim runs before the daemon asks the server for work. A denied
// claim leaves tasks queued server-side, so no task is terminal-failed and no
// execution environment or workdir can be created.
func (p *diskPreflight) allowTaskClaim() bool {
	free, err := p.freeGiB(p.path)
	if err != nil {
		p.transition(diskPreflightError, 0, fmt.Errorf("read free disk space: %w", err))
		return false
	}

	next := diskPreflightNormal
	allow := true
	if p.state == diskPreflightCritical || p.state == diskPreflightError {
		if free < p.recoveryGiB {
			next = diskPreflightCritical
			allow = false
		}
	} else if free < p.criticalGiB {
		next = diskPreflightCritical
		allow = false
	} else if free < p.warningGiB {
		next = diskPreflightWarning
	}
	p.transition(next, free, nil)
	return allow
}

func (p *diskPreflight) transition(next diskPreflightState, free uint64, cause error) {
	if next == p.state {
		return
	}
	previous := p.state
	p.state = next
	fields := []any{
		"previous", previous.String(),
		"state", next.String(),
		"free_gib", free,
		"warning_gib", p.warningGiB,
		"critical_gib", p.criticalGiB,
		"recovery_gib", p.recoveryGiB,
	}
	switch next {
	case diskPreflightCritical:
		p.logger.Warn("disk preflight parked new task claims", fields...)
	case diskPreflightError:
		fields = append(fields, "error", cause)
		p.logger.Error("disk preflight failed closed", fields...)
	case diskPreflightWarning:
		p.logger.Warn("disk preflight warning; task claims remain enabled", fields...)
	default:
		p.logger.Info("disk preflight recovered; task claims enabled", fields...)
	}
}

func (s diskPreflightState) String() string {
	switch s {
	case diskPreflightNormal:
		return "normal"
	case diskPreflightWarning:
		return "warning"
	case diskPreflightCritical:
		return "critical"
	case diskPreflightError:
		return "error"
	default:
		return "unknown"
	}
}
