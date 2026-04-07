package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// agentflowScheduleInterval is how often we check for due triggers.
	agentflowScheduleInterval = 30 * time.Second
)

// runAgentflowScheduler periodically checks for schedule triggers that are due
// and dispatches agentflow runs. Modeled after runRuntimeSweeper.
func runAgentflowScheduler(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	taskSvc := service.NewTaskService(queries, nil, bus)
	ticker := time.NewTicker(agentflowScheduleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickScheduledTriggers(ctx, queries, taskSvc)
		}
	}
}

// tickScheduledTriggers atomically claims all due schedule triggers and
// dispatches agentflow runs for each.
func tickScheduledTriggers(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService) {
	dueTriggers, err := queries.ClaimDueScheduleTriggers(ctx)
	if err != nil {
		slog.Warn("agentflow scheduler: failed to claim due triggers", "error", err)
		return
	}
	if len(dueTriggers) == 0 {
		return
	}

	slog.Info("agentflow scheduler: processing due triggers", "count", len(dueTriggers))

	for _, t := range dueTriggers {
		dispatchScheduledTrigger(ctx, queries, taskSvc, t)
	}
}

// dispatchScheduledTrigger creates an agentflow run, enqueues a task,
// and recalculates the trigger's next_run_at.
func dispatchScheduledTrigger(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, t db.ClaimDueScheduleTriggersRow) {
	afID := util.UUIDToString(t.AgentflowID)
	triggerID := util.UUIDToString(t.ID)

	// Check concurrency policy
	if t.ConcurrencyPolicy == "skip_if_active" || t.ConcurrencyPolicy == "coalesce" {
		hasActive, err := queries.HasActiveAgentflowRun(ctx, t.AgentflowID)
		if err == nil && hasActive {
			status := "skipped"
			if t.ConcurrencyPolicy == "coalesce" {
				status = "coalesced"
			}
			slog.Info("agentflow scheduler: "+status+" due to concurrency policy",
				"agentflow_id", afID, "trigger_id", triggerID, "policy", t.ConcurrencyPolicy)

			queries.CreateAgentflowRun(ctx, db.CreateAgentflowRunParams{
				AgentflowID: t.AgentflowID,
				TriggerID:   t.ID,
				SourceKind:  "schedule",
				Status:      status,
			})
			// Still recalculate next_run_at
			recalcNextRun(ctx, queries, t)
			return
		}
	}

	// Create the run
	run, err := queries.CreateAgentflowRun(ctx, db.CreateAgentflowRunParams{
		AgentflowID: t.AgentflowID,
		TriggerID:   t.ID,
		SourceKind:  "schedule",
		Status:      "received",
	})
	if err != nil {
		slog.Error("agentflow scheduler: failed to create run", "agentflow_id", afID, "error", err)
		recalcNextRun(ctx, queries, t)
		return
	}

	// Build a minimal Agentflow struct for the task service
	af := db.Agentflow{
		ID:                t.AgentflowID,
		WorkspaceID:       t.WorkspaceID,
		AgentID:           t.AgentID,
		Title:             t.AgentflowTitle,
		ConcurrencyPolicy: t.ConcurrencyPolicy,
	}
	if t.AgentflowDescription.Valid {
		af.Description = t.AgentflowDescription
	}

	if err := taskSvc.EnqueueTaskForAgentflow(ctx, af, run); err != nil {
		slog.Error("agentflow scheduler: failed to enqueue task",
			"agentflow_id", afID, "run_id", util.UUIDToString(run.ID), "error", err)
		queries.UpdateAgentflowRunStatus(ctx, db.UpdateAgentflowRunStatusParams{
			ID:     run.ID,
			Status: "failed",
		})
	}

	// Recalculate next_run_at
	recalcNextRun(ctx, queries, t)
}

// recalcNextRun parses the cron expression and sets the next run time.
func recalcNextRun(ctx context.Context, queries *db.Queries, t db.ClaimDueScheduleTriggersRow) {
	if !t.CronExpression.Valid || t.CronExpression.String == "" {
		return
	}

	// Parse timezone
	loc := time.UTC
	if t.Timezone.Valid && t.Timezone.String != "" {
		if parsed, err := time.LoadLocation(t.Timezone.String); err == nil {
			loc = parsed
		}
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(t.CronExpression.String)
	if err != nil {
		slog.Error("agentflow scheduler: invalid cron expression",
			"trigger_id", util.UUIDToString(t.ID),
			"cron", t.CronExpression.String,
			"error", err)
		return
	}

	now := time.Now().In(loc)
	next := schedule.Next(now)

	queries.SetTriggerNextRunAt(ctx, db.SetTriggerNextRunAtParams{
		ID:        t.ID,
		NextRunAt: pgtype.Timestamptz{Time: next, Valid: true},
	})
}
