package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

var errQuotaPoolHeld = errors.New("provider quota pool held")

// reserveQuotaProbeForClaim runs after ClaimAgentTask in the same transaction.
// The task and pool locks make the first claim the only probe across every
// agent and runtime mapped to the account. A losing claim rolls back its task
// transition and waits for the probe's terminal result.
func reserveQuotaProbeForClaim(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue) error {
	pool, err := qtx.GetProviderQuotaPoolForAgentForUpdate(ctx, task.AgentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock provider quota pool for claim: %w", err)
	}
	if pool.State == "open" {
		return nil
	}
	if pool.State != "probe_due" || pool.ProbeAttempts >= 3 || !pool.ProbeAgentID.Valid || pool.ProbeAgentID != task.AgentID {
		return errQuotaPoolHeld
	}
	_, err = qtx.SetProviderQuotaPoolState(ctx, db.SetProviderQuotaPoolStateParams{
		ID: pool.ID, ExpectedRevision: pool.Revision, State: "probing",
		ResetAt: pool.ResetAt, ResetDate: pool.ResetDate,
		SourceTaskID: pool.SourceTaskID, ObservedAt: pool.ObservedAt,
		ProbeStartedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		ProbeTaskID:    task.ID, ProbeAttempts: pool.ProbeAttempts + 1,
	})
	if err != nil {
		return fmt.Errorf("reserve provider quota probe: %w", err)
	}
	return qtx.CreateProviderQuotaPoolEvent(ctx, db.CreateProviderQuotaPoolEventParams{
		ID: dbid.NewV7(), PoolID: pool.ID, SourceTaskID: task.ID,
		EventType: "probe_started", Reason: "reset due",
		OldState: pool.State, NewState: "probing",
	})
}

// quotaHoldTransition computes the new state without storing provider output.
// An unknown reset never replaces a known one, and a duplicate task failure
// does not create another event or move the reset.
func quotaHoldTransition(pool db.ProviderQuotaPool, taskID pgtype.UUID, hint taskfailure.QuotaResetHint, observedAt time.Time) (db.SetProviderQuotaPoolStateParams, string, bool) {
	if pool.SourceTaskID == taskID && pool.State != "open" {
		return db.SetProviderQuotaPoolStateParams{}, "", false
	}
	arg := db.SetProviderQuotaPoolStateParams{
		ID: pool.ID, ExpectedRevision: pool.Revision,
		SourceTaskID: taskID,
		ObservedAt:   pgtype.Timestamptz{Time: observedAt.UTC(), Valid: true},
	}
	switch hint.Kind {
	case taskfailure.QuotaResetExact:
		arg.State = "held_exact"
		arg.ResetAt = pgtype.Timestamptz{Time: hint.At.UTC(), Valid: true}
	case taskfailure.QuotaResetDateOnly:
		date, err := time.Parse("2006-01-02", hint.Date)
		if err != nil {
			return db.SetProviderQuotaPoolStateParams{}, "", false
		}
		arg.State = "held_date"
		arg.ResetDate = pgtype.Date{Time: date, Valid: true}
	default:
		arg.State = "reset_unknown"
	}
	// A date-only reset can remain "today" after a failed probe. Re-entering
	// held_date would make the scheduler mark it due on every sweep and start
	// another task immediately. Keep the probe budget and back off instead.
	if pool.State == "probing" && arg.State == "held_date" {
		location, err := time.LoadLocation(pool.Timezone)
		if err != nil {
			arg.State = "reset_unknown"
			arg.ResetDate = pgtype.Date{}
		} else if arg.ResetDate.Time.Format("2006-01-02") <= observedAt.In(location).Format("2006-01-02") {
			arg.ResetDate = pgtype.Date{}
			arg.ProbeAttempts = pool.ProbeAttempts
			if pool.ProbeAttempts >= 3 {
				arg.State = "reset_unknown"
			} else {
				arg.State = "probe_backoff"
				arg.ResetAt = pgtype.Timestamptz{
					Time: observedAt.UTC().Add(time.Duration(pool.ProbeAttempts) * 5 * time.Minute), Valid: true,
				}
			}
		}
	}
	oldDeadline, oldKnown := quotaPoolDeadline(pool.State, pool.ResetAt, pool.ResetDate, pool.Timezone)
	newDeadline, newKnown := quotaPoolDeadline(arg.State, arg.ResetAt, arg.ResetDate, hint.Timezone)
	if oldKnown && oldDeadline.After(observedAt) && (!newKnown || !newDeadline.After(oldDeadline)) {
		return db.SetProviderQuotaPoolStateParams{}, "", false
	}
	if pool.State == "open" {
		return arg, "held", true
	}
	if pool.State == "probing" {
		return arg, "probe_failed", true
	}
	return arg, "extended", true
}

// Date-only deadlines are the first instant on the provider's stated date.
// They are only used to compare evidence; reaching that instant still requires
// a probe before the pool can open.
func quotaPoolDeadline(state string, at pgtype.Timestamptz, date pgtype.Date, zone string) (time.Time, bool) {
	if state == "held_exact" && at.Valid {
		return at.Time, true
	}
	if state != "held_date" || !date.Valid {
		return time.Time{}, false
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, false
	}
	year, month, day := date.Time.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location), true
}

// holdQuotaPoolForFailure is part of the task failure transaction. A verified
// quota failure blocks the account before the failed task becomes visible.
// The pool row lock serializes with new task starts; unmapped agents retain
// current behavior. No provider error text is persisted in the pool ledger.
func holdQuotaPoolForFailure(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue, reason, rawError string) error {
	if reason != taskfailure.ReasonAgentProviderQuotaLimit.String() {
		return nil
	}
	pool, err := qtx.GetProviderQuotaPoolForAgentForUpdate(ctx, task.AgentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock provider quota pool: %w", err)
	}
	// A malformed legacy zone cannot stop the task's terminal transition.
	// The parser treats a nil location as an unknown reset, which holds the
	// pool pending an audited correction instead of guessing an instant.
	location, _ := time.LoadLocation(pool.Timezone)
	observedAt := time.Now()
	hint := taskfailure.ParseQuotaResetHint(taskfailure.ReasonAgentProviderQuotaLimit, rawError, observedAt, location)
	arg, eventType, changed := quotaHoldTransition(pool, task.ID, hint, observedAt)
	if !changed {
		return nil
	}
	if _, err := qtx.SetProviderQuotaPoolState(ctx, arg); err != nil {
		return fmt.Errorf("hold provider quota pool: %w", err)
	}
	return qtx.CreateProviderQuotaPoolEvent(ctx, db.CreateProviderQuotaPoolEventParams{
		ID: dbid.NewV7(), PoolID: pool.ID, SourceTaskID: task.ID,
		EventType: eventType, Reason: reason,
		OldState: pool.State, NewState: arg.State,
	})
}

// settleQuotaProbeFailure keeps the account closed when the probe failed for a
// reason other than provider quota. A bounded delay permits transient failures
// to recover; after three attempts an operator must examine the pool.
func settleQuotaProbeFailure(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue, reason string) error {
	pool, err := qtx.GetProviderQuotaPoolForAgentForUpdate(ctx, task.AgentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock provider quota probe: %w", err)
	}
	if pool.State != "probing" || pool.ProbeTaskID != task.ID {
		return nil
	}
	arg := quotaProbeFailureState(pool, time.Now())
	if _, err := qtx.SetProviderQuotaPoolState(ctx, arg); err != nil {
		return fmt.Errorf("back off provider quota probe: %w", err)
	}
	return qtx.CreateProviderQuotaPoolEvent(ctx, db.CreateProviderQuotaPoolEventParams{
		ID: dbid.NewV7(), PoolID: pool.ID, SourceTaskID: task.ID,
		EventType: "probe_failed", Reason: reason,
		OldState: pool.State, NewState: arg.State,
	})
}

func quotaProbeFailureState(pool db.ProviderQuotaPool, now time.Time) db.SetProviderQuotaPoolStateParams {
	arg := db.SetProviderQuotaPoolStateParams{
		ID: pool.ID, ExpectedRevision: pool.Revision,
		SourceTaskID: pool.SourceTaskID, ObservedAt: pool.ObservedAt,
		ProbeAttempts: pool.ProbeAttempts,
	}
	if pool.ProbeAttempts >= 3 {
		arg.State = "reset_unknown"
	} else {
		arg.State = "probe_backoff"
		arg.ResetAt = pgtype.Timestamptz{Time: now.UTC().Add(time.Duration(pool.ProbeAttempts) * 5 * time.Minute), Valid: true}
	}
	return arg
}

// completeQuotaProbe releases a pool only when its reserved probe task
// completed. Concurrent completions from other agents in the pool cannot
// release it. The task and pool transitions commit together.
func completeQuotaProbe(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue) (pgtype.UUID, error) {
	pool, err := qtx.GetProviderQuotaPoolForAgentForUpdate(ctx, task.AgentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, nil
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("lock provider quota probe: %w", err)
	}
	if pool.State != "probing" || pool.ProbeTaskID != task.ID {
		return pgtype.UUID{}, nil
	}
	if _, err := qtx.SetProviderQuotaPoolState(ctx, db.SetProviderQuotaPoolStateParams{
		ID: pool.ID, ExpectedRevision: pool.Revision, State: "open",
	}); err != nil {
		return pgtype.UUID{}, fmt.Errorf("release provider quota pool after probe: %w", err)
	}
	if err := qtx.CreateProviderQuotaPoolEvent(ctx, db.CreateProviderQuotaPoolEventParams{
		ID: dbid.NewV7(), PoolID: pool.ID, SourceTaskID: task.ID,
		EventType: "probe_succeeded", Reason: "task completed",
		OldState: pool.State, NewState: "open",
	}); err != nil {
		return pgtype.UUID{}, err
	}
	return pool.ID, nil
}

func (s *TaskService) notifyQuotaPoolReleased(ctx context.Context, poolID pgtype.UUID) {
	runtimes, err := s.Queries.ListProviderQuotaPoolRuntimeIDs(ctx, poolID)
	if err != nil {
		// Claim polling still sees an open pool after the cache TTL. The
		// wakeup is a latency optimization, not part of the state transition.
		return
	}
	for _, runtimeID := range runtimes {
		s.notifyRuntimeMayHaveWork(runtimeID, "")
	}
}

// NotifyProviderQuotaPoolReleased wakes every runtime in an account pool after
// an audited manual release. The claim predicates still enforce the DB state.
func (s *TaskService) NotifyProviderQuotaPoolReleased(ctx context.Context, poolID pgtype.UUID) {
	s.notifyQuotaPoolReleased(ctx, poolID)
}

// ReconcileDueProviderQuotaPools is server-owned; it does not rely on any
// provider daemon being online. Every replica may run it: the locked pool row
// and revision compare-and-swap leave one durable transition and one event.
func (s *TaskService) ReconcileDueProviderQuotaPools(ctx context.Context, limit int32) error {
	due, err := s.Queries.ListDueProviderQuotaPools(ctx, limit)
	if err != nil {
		return fmt.Errorf("list due provider quota pools: %w", err)
	}
	for _, candidate := range due {
		var probeReady bool
		err := s.runInTx(ctx, func(qtx *db.Queries) error {
			pool, err := qtx.GetProviderQuotaPoolForUpdate(ctx, candidate.ID)
			if err != nil {
				return err
			}
			if !quotaPoolResetDue(pool, time.Now()) {
				return nil
			}
			arg := db.SetProviderQuotaPoolStateParams{
				ID: pool.ID, ExpectedRevision: pool.Revision,
				ResetAt: pool.ResetAt, ResetDate: pool.ResetDate,
				SourceTaskID: pool.SourceTaskID, ObservedAt: pool.ObservedAt,
				ProbeAttempts: pool.ProbeAttempts,
			}
			eventType := "probe_due"
			if pool.ProbeAttempts >= 3 {
				arg.State = "reset_unknown"
				arg.ResetAt = pgtype.Timestamptz{}
				arg.ResetDate = pgtype.Date{}
				eventType = "probe_failed"
			} else {
				arg.State = "probe_due"
				probeReady = true
			}
			if _, err := qtx.SetProviderQuotaPoolState(ctx, arg); err != nil {
				return err
			}
			return qtx.CreateProviderQuotaPoolEvent(ctx, db.CreateProviderQuotaPoolEventParams{
				ID: dbid.NewV7(), PoolID: pool.ID, SourceTaskID: pool.SourceTaskID,
				EventType: eventType, Reason: "reset due",
				OldState: pool.State, NewState: arg.State,
			})
		})
		if err != nil {
			return fmt.Errorf("reconcile provider quota pool: %w", err)
		}
		if probeReady {
			s.notifyQuotaPoolReleased(ctx, candidate.ID)
		}
	}
	orphaned, err := s.Queries.ListOrphanedProviderQuotaProbes(ctx, limit)
	if err != nil {
		return fmt.Errorf("list orphaned provider quota probes: %w", err)
	}
	for _, candidate := range orphaned {
		if err := s.reconcileOrphanedQuotaProbe(ctx, candidate.ID); err != nil {
			return fmt.Errorf("reconcile orphaned provider quota probe: %w", err)
		}
	}
	return nil
}

func (s *TaskService) reconcileOrphanedQuotaProbe(ctx context.Context, poolID pgtype.UUID) error {
	return s.runInTx(ctx, func(qtx *db.Queries) error {
		pool, err := qtx.GetProviderQuotaPoolForUpdate(ctx, poolID)
		if err != nil {
			return err
		}
		if pool.State != "probing" {
			return nil
		}
		reason := "probe task missing"
		if pool.ProbeTaskID.Valid {
			task, taskErr := qtx.GetAgentTask(ctx, pool.ProbeTaskID)
			if taskErr != nil && !errors.Is(taskErr, pgx.ErrNoRows) {
				return taskErr
			}
			if taskErr == nil {
				switch task.Status {
				case "completed", "failed", "cancelled":
					reason = task.Status
				default:
					return nil
				}
			}
		}
		arg := quotaProbeFailureState(pool, time.Now())
		if _, err := qtx.SetProviderQuotaPoolState(ctx, arg); err != nil {
			return err
		}
		return qtx.CreateProviderQuotaPoolEvent(ctx, db.CreateProviderQuotaPoolEventParams{
			ID: dbid.NewV7(), PoolID: pool.ID, SourceTaskID: pool.ProbeTaskID,
			EventType: "probe_failed", Reason: reason,
			OldState: pool.State, NewState: arg.State,
		})
	})
}

func quotaPoolResetDue(pool db.ProviderQuotaPool, now time.Time) bool {
	switch pool.State {
	case "held_exact", "probe_backoff":
		return pool.ResetAt.Valid && !pool.ResetAt.Time.After(now)
	case "held_date":
		if !pool.ResetDate.Valid {
			return false
		}
		location, err := time.LoadLocation(pool.Timezone)
		if err != nil {
			return false
		}
		resetYear, resetMonth, resetDay := pool.ResetDate.Time.Date()
		localYear, localMonth, localDay := now.In(location).Date()
		return localYear*10000+int(localMonth)*100+localDay >= resetYear*10000+int(resetMonth)*100+resetDay
	default:
		return false
	}
}
