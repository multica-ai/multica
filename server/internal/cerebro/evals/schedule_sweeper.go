package evals

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// evalDriftEnabled gates both drift sweepers (schedule + drift alarm) behind
// CEREBRO_EVAL_DRIFT_ENABLED (default OFF). Accepts the same on-values the
// workflow env flags accept.
func evalDriftEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CEREBRO_EVAL_DRIFT_ENABLED"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// ScheduleSweeper runs due eval schedules. Modeled on the workflow cron sweeper
// (workflows/cron.go): each tick claims due rows, runs each eval through the
// per-workspace gateway executor, records the run, and advances next_run_at.
// Idempotent — never backfills a missed window; a second sweep at the same
// instant claims nothing because MarkScheduleRan pushed next_run_at forward.
type ScheduleSweeper struct {
	store *Store
	now   func() time.Time
}

// NewScheduleSweeper builds a ScheduleSweeper with the same workspace-aware
// executor used by Run-now and eval.run.
func NewScheduleSweeper(store *Store, executor RunExecutor) *ScheduleSweeper {
	if store != nil {
		store.WithRunExecutor(executor)
	}
	return &ScheduleSweeper{store: store, now: time.Now}
}

// Run blocks on ctx and sweeps once per interval. It returns immediately when
// the drift feature is disabled, so an unset CEREBRO_EVAL_DRIFT_ENABLED costs
// nothing. Production wires interval = 1 minute.
func (s *ScheduleSweeper) Run(ctx context.Context, interval time.Duration) {
	if !evalDriftEnabled() {
		return
	}
	if interval <= 0 {
		interval = time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !evalDriftEnabled() {
				continue
			}
			if err := s.SweepOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("cerebro eval schedule sweep failed", "error", err)
			}
		}
	}
}

// SweepOnce runs one deterministic pass over the due schedules. Exported so
// tests and debug endpoints can advance the sweeper without the ticker.
func (s *ScheduleSweeper) SweepOnce(ctx context.Context) error {
	now := s.now()
	due, err := s.store.ClaimDueSchedules(ctx, now, 100)
	if err != nil {
		return fmt.Errorf("claim due schedules: %w", err)
	}
	for _, sched := range due {
		if err := s.runOne(ctx, sched, now); err != nil {
			// One bad schedule never blocks the rest of the sweep.
			slog.Warn("cerebro eval schedule sweep: schedule failed",
				"schedule_id", sched.ID.String(), "eval_id", sched.EvalID.String(), "error", err)
		}
	}
	return nil
}

// runOne executes a single due schedule and advances its next_run_at. When the
// workspace has no gateway executor the eval cannot run; the schedule is still
// advanced so the sweeper does not hot-loop on the same due row every tick.
func (s *ScheduleSweeper) runOne(ctx context.Context, sched EvalSchedule, now time.Time) error {
	next, nextErr := sched.NextRun(now)
	if nextErr != nil {
		return fmt.Errorf("compute next run: %w", nextErr)
	}

	_, runErr := s.store.CreateRun(ctx, sched.WorkspaceID, sched.CreatedByID, sched.EvalID, "member", EvalRunInput{})
	markErr := s.store.MarkScheduleRan(ctx, sched.ID, next)
	if runErr != nil {
		if markErr != nil {
			return errors.Join(fmt.Errorf("execute scheduled eval: %w", runErr), fmt.Errorf("advance failed schedule: %w", markErr))
		}
		return fmt.Errorf("execute scheduled eval: %w", runErr)
	}
	return markErr
}
