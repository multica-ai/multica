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
	oldDeadline, oldKnown := quotaPoolDeadline(pool.State, pool.ResetAt, pool.ResetDate, pool.Timezone)
	newDeadline, newKnown := quotaPoolDeadline(arg.State, arg.ResetAt, arg.ResetDate, hint.Timezone)
	if oldKnown && oldDeadline.After(observedAt) && (!newKnown || !newDeadline.After(oldDeadline)) {
		return db.SetProviderQuotaPoolStateParams{}, "", false
	}
	if pool.State == "open" {
		return arg, "held", true
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
	location, err := time.LoadLocation(pool.Timezone)
	if err != nil {
		return fmt.Errorf("invalid provider quota pool timezone: %w", err)
	}
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
