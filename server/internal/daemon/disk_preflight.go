package daemon

import (
	"fmt"
	"log/slog"
)

type diskPreflightState uint8

// Every state except normal parks new claims. warning and critical differ only
// in how loudly they are reported: warning means "below the admission floor",
// critical means "far enough below it to deserve a louder line". Neither admits
// work, so there is exactly one admission boundary (the warning/recovery
// threshold), not two.
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
	if free < p.criticalGiB {
		next = diskPreflightCritical
		allow = false
	} else if free < p.warningGiB {
		allow = false
		if p.state == diskPreflightCritical || p.state == diskPreflightError {
			next = diskPreflightCritical
		} else {
			next = diskPreflightWarning
		}
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
		p.logger.Warn("disk preflight: new task claims parked, free space critically below admission floor", fields...)
	case diskPreflightError:
		fields = append(fields, "error", cause)
		p.logger.Error("disk preflight failed closed: new task claims parked", fields...)
	case diskPreflightWarning:
		p.logger.Warn("disk preflight: new task claims parked, free space below admission floor", fields...)
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
