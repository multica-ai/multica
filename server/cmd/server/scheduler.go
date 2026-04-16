package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// workflowSchedulerInterval is how often we check for due schedules.
	workflowSchedulerInterval = 30 * time.Second
)

// runScheduler periodically checks for due schedules and triggers their workflows.
// It follows the same pattern as runRuntimeSweeper.
func runScheduler(ctx context.Context, queries *db.Queries, workflowSvc *service.WorkflowService) {
	ticker := time.NewTicker(workflowSchedulerInterval)
	defer ticker.Stop()

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			processDueSchedules(ctx, queries, workflowSvc, parser)
		}
	}
}

func processDueSchedules(ctx context.Context, queries *db.Queries, workflowSvc *service.WorkflowService, parser cron.Parser) {
	schedules, err := queries.GetDueSchedules(ctx)
	if err != nil {
		slog.Warn("scheduler: failed to get due schedules", "error", err)
		return
	}

	if len(schedules) == 0 {
		return
	}

	slog.Info("scheduler: processing due schedules", "count", len(schedules))

	for _, sched := range schedules {
		processSchedule(ctx, queries, workflowSvc, parser, sched)
	}
}

func processSchedule(ctx context.Context, queries *db.Queries, workflowSvc *service.WorkflowService, parser cron.Parser, sched db.Schedule) {
	schedID := util.UUIDToString(sched.ID)

	// Start the workflow
	_, err := workflowSvc.StartWorkflow(ctx, sched.WorkflowID, "schedule", sched.ID)
	if err != nil {
		slog.Error("scheduler: failed to start workflow",
			"schedule_id", schedID,
			"workflow_id", util.UUIDToString(sched.WorkflowID),
			"error", err,
		)
		return
	}

	slog.Info("scheduler: triggered workflow",
		"schedule_id", schedID,
		"workflow_id", util.UUIDToString(sched.WorkflowID),
	)

	// Compute next run time and advance the schedule
	switch sched.ScheduleType {
	case "once":
		// One-time schedule: disable after firing
		if _, err := queries.DisableSchedule(ctx, sched.ID); err != nil {
			slog.Error("scheduler: failed to disable one-time schedule", "schedule_id", schedID, "error", err)
		}

	case "recurring":
		if !sched.CronExpr.Valid || sched.CronExpr.String == "" {
			slog.Warn("scheduler: recurring schedule has no cron expression", "schedule_id", schedID)
			return
		}
		schedule, err := parser.Parse(sched.CronExpr.String)
		if err != nil {
			slog.Error("scheduler: invalid cron expression", "schedule_id", schedID, "cron_expr", sched.CronExpr.String, "error", err)
			return
		}
		// Use the schedule's timezone so cron hours match local time
		loc, locErr := time.LoadLocation(sched.Timezone)
		if locErr != nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		nextRun := schedule.Next(now).UTC()
		if _, err := queries.AdvanceScheduleNextRun(ctx, db.AdvanceScheduleNextRunParams{
			ID:        sched.ID,
			NextRunAt: pgtype.Timestamptz{Time: nextRun, Valid: true},
		}); err != nil {
			slog.Error("scheduler: failed to advance next_run_at", "schedule_id", schedID, "error", err)
		}
	}
}
