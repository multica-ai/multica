package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const schedulerInterval = 30 * time.Second

// runAutopilotScheduler polls for due schedule triggers and dispatches them.
func runAutopilotScheduler(ctx context.Context, queries *db.Queries, svc *service.AutopilotService) {
	// Recover triggers that were claimed but never advanced (e.g. after a crash).
	recoverLostTriggers(ctx, queries)

	ticker := time.NewTicker(schedulerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickScheduledAutopilots(ctx, queries, svc)
		}
	}
}

// recoverLostTriggers finds schedule triggers whose next_run_at is NULL
// (claimed but never advanced, typically after a crash) and recomputes it.
func recoverLostTriggers(ctx context.Context, queries *db.Queries) {
	triggers, err := queries.RecoverLostTriggers(ctx)
	if err != nil {
		slog.Warn("autopilot scheduler: failed to recover lost triggers", "error", err)
		return
	}
	if len(triggers) == 0 {
		return
	}

	slog.Info("autopilot scheduler: recovering lost triggers", "count", len(triggers))
	for _, t := range triggers {
		if !t.CronExpression.Valid || t.CronExpression.String == "" {
			continue
		}
		tz := service.DefaultAutopilotTriggerTimezone
		if t.Timezone.Valid && t.Timezone.String != "" {
			tz = t.Timezone.String
		}
		next, err := service.ComputeNextRun(t.CronExpression.String, tz)
		if err != nil {
			slog.Warn("autopilot scheduler: failed to compute next run for recovery",
				"trigger_id", util.UUIDToString(t.ID), "error", err)
			continue
		}
		if err := queries.AdvanceTriggerNextRun(ctx, db.AdvanceTriggerNextRunParams{
			ID:        t.ID,
			NextRunAt: pgtype.Timestamptz{Time: next, Valid: true},
		}); err != nil {
			slog.Warn("autopilot scheduler: failed to recover trigger",
				"trigger_id", util.UUIDToString(t.ID), "error", err)
		}
	}
}

// tickScheduledAutopilots claims all due triggers and dispatches each one.
func tickScheduledAutopilots(ctx context.Context, queries *db.Queries, svc *service.AutopilotService) {
	triggers, err := queries.ClaimDueScheduleTriggers(ctx)
	if err != nil {
		slog.Warn("autopilot scheduler: failed to claim due triggers", "error", err)
		return
	}
	if len(triggers) == 0 {
		return
	}

	slog.Info("autopilot scheduler: claimed due triggers", "count", len(triggers))

	for _, t := range triggers {
		autopilot, err := queries.GetAutopilot(ctx, t.AutopilotID)
		if err != nil {
			slog.Warn("autopilot scheduler: failed to load autopilot",
				"trigger_id", util.UUIDToString(t.ID),
				"autopilot_id", util.UUIDToString(t.AutopilotID),
				"error", err,
			)
			restoreClaimedTrigger(ctx, queries, t)
			continue
		}

		// Dispatch the claimed schedule occurrence.
		run, err := svc.DispatchScheduledAutopilot(ctx, autopilot, t.ID, t.ScheduledFireAt)
		if err != nil {
			slog.Warn("autopilot scheduler: dispatch failed",
				"autopilot_id", util.UUIDToString(autopilot.ID),
				"trigger_id", util.UUIDToString(t.ID),
				"error", err,
			)
			if !shouldAdvanceAfterScheduleDispatch(run, err) {
				restoreClaimedTrigger(ctx, queries, t)
				continue
			}
		}

		// Advance next_run_at for this trigger.
		advanceNextRun(ctx, queries, t)
	}
}

func shouldAdvanceAfterScheduleDispatch(run *db.AutopilotRun, err error) bool {
	if err == nil || run != nil {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "idx_autopilot_run_schedule_occurrence"
}

func restoreClaimedTrigger(ctx context.Context, queries *db.Queries, t db.ClaimDueScheduleTriggersRow) {
	if !t.ScheduledFireAt.Valid {
		slog.Warn("autopilot scheduler: cannot restore claimed trigger without scheduled fire time",
			"trigger_id", util.UUIDToString(t.ID),
		)
		return
	}
	if err := queries.RestoreClaimedTriggerNextRun(ctx, db.RestoreClaimedTriggerNextRunParams{
		ID:        t.ID,
		NextRunAt: t.ScheduledFireAt,
	}); err != nil {
		slog.Warn("autopilot scheduler: failed to restore claimed trigger",
			"trigger_id", util.UUIDToString(t.ID),
			"error", err,
		)
	}
}

// advanceNextRun computes the next fire time and updates the trigger.
func advanceNextRun(ctx context.Context, queries *db.Queries, t db.ClaimDueScheduleTriggersRow) {
	if !t.CronExpression.Valid || t.CronExpression.String == "" {
		return
	}
	if !t.ScheduledFireAt.Valid {
		slog.Warn("autopilot scheduler: claimed trigger missing scheduled fire time",
			"trigger_id", util.UUIDToString(t.ID),
		)
		return
	}
	if !t.ClaimedAt.Valid {
		slog.Warn("autopilot scheduler: claimed trigger missing claim time",
			"trigger_id", util.UUIDToString(t.ID),
		)
		return
	}

	tz := service.DefaultAutopilotTriggerTimezone
	if t.Timezone.Valid && t.Timezone.String != "" {
		tz = t.Timezone.String
	}

	next, err := service.ComputeNextRunAfter(t.CronExpression.String, tz, t.ClaimedAt.Time)
	if err != nil {
		slog.Warn("autopilot scheduler: failed to compute next run",
			"trigger_id", util.UUIDToString(t.ID),
			"cron", t.CronExpression.String,
			"error", err,
		)
		return
	}

	if err := queries.AdvanceTriggerNextRun(ctx, db.AdvanceTriggerNextRunParams{
		ID:        t.ID,
		NextRunAt: pgtype.Timestamptz{Time: next, Valid: true},
	}); err != nil {
		slog.Warn("autopilot scheduler: failed to advance next_run_at",
			"trigger_id", util.UUIDToString(t.ID),
			"error", err,
		)
	}
}
