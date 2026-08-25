// Package autopilot implements the autopilot background workers that live
// outside the request-serving dispatch path. Currently that is the stale-run
// sweeper, the defensive backstop for the lease-gated dispatch path.
package autopilot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// sweeperDB is the narrow contract the sweeper needs from a database handle:
// a single-statement UPDATE. *pgxpool.Pool satisfies it; tests can substitute
// any fake with the same shape.
type sweeperDB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// SweeperConfig is the tunable knob set for the stale-run sweeper. All values
// are wired from env vars in cmd/server/main.go; defaults live there too.
type SweeperConfig struct {
	// Interval is the sweep cadence. A value <= 0 disables the sweeper.
	Interval time.Duration
	// HardTimeout is the age at which an in-flight run is considered
	// permanently stuck and terminalized as failed. Should be a multiple
	// of the dispatch lease timeout (AUTOPILOT_RUN_LEASE_TIMEOUT) so the
	// sweeper only reclaims runs the lease gate has already had plenty of
	// chances to reclaim on its own.
	HardTimeout time.Duration
	// Enabled gates the whole worker. Useful on small self-hosted
	// deployments that want the lease gate but not another goroutine.
	Enabled bool
	// Logger receives the worker's operational logs. Nil is tolerated and
	// falls back to slog.Default().
	Logger *slog.Logger
}

// StaleRunSweeper periodically terminalizes autopilot_run rows that are
// still in an in-flight status (issue_created / running) past the hard
// timeout. It is the second layer of the lease defence (ALL-211 BLOCKING 1):
//
//   - Layer 1 is the dispatch-path lease gate: when a new slot arrives and
//     the in-flight run's lease has expired, the gate terminalizes it and
//     admits the slot. But a slot may simply never arrive (paused autopilot,
//     disabled trigger), so the stuck run would sit forever — and the failure
//     monitor only ever sees completed/failed runs, so a permanently-stuck
//     run is invisible to auto-pause.
//   - Layer 2 (this sweeper) reclaims such runs on a cadence regardless of
//     dispatch activity. The terminalization makes them visible to the
//     failure monitor (reason_code=lease_expired), so an autopilot whose
//     runs never finish can still be auto-paused by the operator.
type StaleRunSweeper struct {
	db           sweeperDB
	interval     time.Duration
	hardTimeout  time.Duration
	enabled      bool
	logger       *slog.Logger
	shutdownChan chan struct{}
}

// NewStaleRunSweeper builds the sweeper. See SweeperConfig for the fields.
func NewStaleRunSweeper(db sweeperDB, config *SweeperConfig) *StaleRunSweeper {
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &StaleRunSweeper{
		db:           db,
		interval:     config.Interval,
		hardTimeout:  config.HardTimeout,
		enabled:      config.Enabled,
		logger:       logger,
		shutdownChan: make(chan struct{}),
	}
}

// Start runs the periodic sweep loop until ctx is cancelled or Stop is
// called. When disabled (Enabled=false or Interval<=0) it logs and returns
// immediately, so a misconfigured deployment fails safe.
func (s *StaleRunSweeper) Start(ctx context.Context) {
	if !s.enabled || s.interval <= 0 {
		s.logger.Info("autopilot stale run sweeper: disabled",
			"enabled", s.enabled, "interval", s.interval.String())
		return
	}

	s.logger.Info("autopilot stale run sweeper: starting",
		"interval", s.interval.String(),
		"hard_timeout", s.hardTimeout.String())

	// Run once immediately so a freshly-deployed node reclaims pre-existing
	// stale runs without waiting a full interval.
	s.SweepOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("autopilot stale run sweeper: stopped")
			return
		case <-s.shutdownChan:
			s.logger.Info("autopilot stale run sweeper: shutdown requested")
			return
		case <-ticker.C:
			s.SweepOnce(ctx)
		}
	}
}

// SweepOnce performs a single sweep: terminalize every in-flight run older
// than the hard timeout. Idempotent — the WHERE clause only matches rows
// still in an in-flight status, so concurrent sweepers (or a racing dispatch
// gate that already reclaimed the same row) never double-terminalize. It is
// exported so tests and callers can drive a sweep without the ticker loop.
func (s *StaleRunSweeper) SweepOnce(ctx context.Context) {
	deadline := time.Now().Add(-s.hardTimeout)

	result, err := s.db.Exec(ctx, `
		UPDATE autopilot_run
		SET status = 'failed',
		    failure_reason = $1,
		    reason_code = 'lease_expired',
		    completed_at = NOW()
		WHERE status IN ('issue_created', 'running')
		  AND created_at < $2
	`,
		fmt.Sprintf("Run exceeded hard timeout (%s) and was terminated by sweeper", s.hardTimeout),
		deadline,
	)
	if err != nil {
		s.logger.Error("autopilot stale run sweeper: sweep failed",
			"hard_timeout", s.hardTimeout.String(),
			"error", err,
		)
		return
	}

	affected := result.RowsAffected()
	if affected > 0 {
		s.logger.Warn("autopilot stale run sweeper: terminated stale runs",
			"count", affected,
			"deadline", deadline.UTC().Format(time.RFC3339),
			"hard_timeout", s.hardTimeout.String(),
		)
	}
}

// Stop requests a graceful shutdown of the sweep loop. Idempotent.
func (s *StaleRunSweeper) Stop() {
	select {
	case <-s.shutdownChan:
		// already closed
	default:
		close(s.shutdownChan)
	}
}
