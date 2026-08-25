// Package autopilot implements the autopilot background workers that live
// outside the request-serving dispatch path. Currently that is the stale-run
// sweeper, the defensive backstop for the lease-gated dispatch path.
package autopilot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/dispatch"
)

// sweeperDB is the narrow contract the sweeper needs from a database handle:
// a read of the in-flight run set plus a single-statement UPDATE.
// *pgxpool.Pool satisfies it; tests can substitute any fake with the same
// shape.
type sweeperDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
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
	// SlotInterval resolves an autopilot's scheduling cadence (the longest
	// gap between two consecutive schedule occurrences) — the SAME source
	// the dispatch lease gate uses via service.SlotIntervalFromCron. The
	// sweeper's per-autopilot reclaim deadline becomes
	// max(HardTimeout, SlotInterval), so a slow-cycle autopilot (e.g. daily)
	// is never reclaimed before its next slot could fire. Wired from
	// main.go to AutopilotService.SlotIntervalForAutopilot; nil keeps the
	// flat HardTimeout behavior for autopilots whose schedule cannot be
	// resolved (webhook/api/manual-only triggers).
	//
	// Choice rationale (ALL-235 BLOCKING 2, 方案 A over 方案 B): 方案 B would
	// cap the lease at an explicit MaxLeaseDuration, which clamps the
	// dispatch gate's lease for slow-cycle autopilots below their slot
	// interval — re-introducing the very early-reclaim defect 1 fixed. 方案 A
	// instead makes the sweeper per-autopilot aware with the SAME cron source
	// as the gate, so the sweeper stays a pure backstop ("兜底而非抢跑") and
	// the two layers can never disagree.
	SlotInterval func(ctx context.Context, autopilotID pgtype.UUID) (time.Duration, bool)
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
//
// Residual risk — the sweeper DELIBERATELY does NOT touch the agent task
// linked via autopilot_run.task_id (ALL-234 defect 2, architecture ruling
// 2026-08-25). Hard-timeout expiry is a wall-clock bound on the RUN row, not
// evidence the TASK is dead; the task's authoritative liveness is the runtime
// heartbeat, and task lifecycle is owned by the dedicated task reclaim layer
// (sweepStaleTasks -> FailStaleTasks in cmd/server/runtime_sweeper.go), which
// combines wall-clock age (dispatched 300s / running 9000s) with heartbeat
// freshness. Between this run's terminalization and the task's natural
// terminalisation (or the runtime sweeper reclaiming it), the SAME autopilot
// can briefly have two executing bodies — uq_autopilot_run_inflight
// constrains run rows, not tasks. The terminal-state guard in
// AutopilotService.SyncRunFromTask keeps a surviving task's late event from
// overwriting this failed/lease_expired row. Note also that terminalizing the
// run releases its quota reservation immediately while the task may still
// consume real compute — an accepted deviation.
type StaleRunSweeper struct {
	db           sweeperDB
	interval     time.Duration
	hardTimeout  time.Duration
	slotInterval func(ctx context.Context, autopilotID pgtype.UUID) (time.Duration, bool)
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
		slotInterval: config.SlotInterval,
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

// SweepOnce performs a single sweep: terminalize every in-flight run past
// its per-autopilot reclaim deadline as failed (reason_code=lease_expired).
// The reclaim deadline is max(HardTimeout, SlotInterval) where SlotInterval
// comes from the SAME cron source as the dispatch lease gate (ALL-235
// BLOCKING 2, 方案 A), so the sweeper never preempts a run the gate would
// still consider live: a daily-schedule autopilot's legitimate long-running
// run (and its linked agent task, deliberately left to the runtime sweeper —
// see the residual-risk note on StaleRunSweeper) survives a sweep.
//
// Each UPDATE is idempotent — the WHERE clause only matches rows still in an
// in-flight status, so concurrent sweepers (or a racing dispatch gate that
// already reclaimed the same row) never double-terminalize. It is exported
// so tests and callers can drive a sweep without the ticker loop.
func (s *StaleRunSweeper) SweepOnce(ctx context.Context) {
	// Load the in-flight run set with their autopilot so the deadline can be
	// computed per autopilot. In-flight runs are rare (the partial unique
	// index uq_autopilot_run_inflight admits at most one per autopilot), so
	// the extra read is negligible next to the correctness it buys.
	rows, err := s.db.Query(ctx, `
		SELECT id, autopilot_id, created_at
		FROM autopilot_run
		WHERE status IN ('issue_created', 'running')
	`)
	if err != nil {
		s.logger.Error("autopilot stale run sweeper: list in-flight runs failed",
			"error", err,
		)
		return
	}
	defer rows.Close()

	type inFlightRun struct {
		id          pgtype.UUID
		autopilotID pgtype.UUID
		createdAt   time.Time
	}
	var runs []inFlightRun
	for rows.Next() {
		var r inFlightRun
		if err := rows.Scan(&r.id, &r.autopilotID, &r.createdAt); err != nil {
			s.logger.Error("autopilot stale run sweeper: scan in-flight run failed",
				"error", err,
			)
			return
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("autopilot stale run sweeper: iterate in-flight runs failed",
			"error", err,
		)
		return
	}

	// Effective deadline per autopilot, computed once per sweep and cached so
	// a slow-cycle autopilot with several runs pays the trigger lookup once.
	type deadlineInfo struct {
		deadline time.Time
		timeout  time.Duration
	}
	deadlines := make(map[pgtype.UUID]deadlineInfo)
	now := time.Now()
	terminated := 0
	for _, r := range runs {
		info, ok := deadlines[r.autopilotID]
		if !ok {
			info.timeout = s.hardTimeout
			if s.slotInterval != nil {
				if interval, ok := s.slotInterval(ctx, r.autopilotID); ok && interval > info.timeout {
					info.timeout = interval
				}
			}
			info.deadline = now.Add(-info.timeout)
			deadlines[r.autopilotID] = info
		}
		if !r.createdAt.Before(info.deadline) {
			continue
		}
		affected, err := s.terminalize(ctx, r.id, info.timeout)
		if err != nil {
			s.logger.Error("autopilot stale run sweeper: terminalize run failed",
				"run_id", utilUUID(r.id),
				"error", err,
			)
			continue
		}
		terminated += affected
	}

	if terminated > 0 {
		s.logger.Warn("autopilot stale run sweeper: terminated stale runs",
			"count", terminated,
		)
	}
}

// terminalize marks a single in-flight run as failed with
// reason_code=lease_expired. The WHERE status IN (...) guard keeps the write
// idempotent under concurrency. Returns the number of rows actually updated
// (0 or 1).
func (s *StaleRunSweeper) terminalize(ctx context.Context, runID pgtype.UUID, effectiveTimeout time.Duration) (int, error) {
	result, err := s.db.Exec(ctx, `
		UPDATE autopilot_run
		SET status = 'failed',
		    failure_reason = $1,
		    reason_code = $2,
		    completed_at = NOW()
		WHERE id = $3
		  AND status IN ('issue_created', 'running')
	`,
		fmt.Sprintf("Run exceeded effective stale-run deadline (%s) and was terminated by sweeper", effectiveTimeout),
		string(dispatch.ReasonLeaseExpired),
		runID,
	)
	if err != nil {
		return 0, err
	}
	return int(result.RowsAffected()), nil
}

// utilUUID renders a pgtype.UUID for log output, tolerating the zero value.
func utilUUID(u pgtype.UUID) string {
	if !u.Valid {
		return "<invalid>"
	}
	return u.String()
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
