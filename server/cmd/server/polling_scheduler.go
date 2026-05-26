package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const pollingSchedulerInterval = 30 * time.Second

// runPollingScheduler polls for due polling issues and enqueues tasks for them.
func runPollingScheduler(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService) {
	// Recover polling issues that have status='polling' but no next_run set
	// (e.g. after a crash or migration).
	recoverPollingIssues(ctx, queries)

	ticker := time.NewTicker(pollingSchedulerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickPollingIssues(ctx, queries, taskSvc)
		}
	}
}

// recoverPollingIssues finds polling issues without a next_run schedule
// and recomputes it based on their interval configuration.
func recoverPollingIssues(ctx context.Context, queries *db.Queries) {
	issues, err := queries.ListPollingIssuesWithoutSchedule(ctx, 100)
	if err != nil {
		slog.Warn("polling scheduler: failed to recover issues without schedule", "error", err)
		return
	}
	if len(issues) == 0 {
		return
	}

	slog.Info("polling scheduler: recovering issues without schedule", "count", len(issues))
	now := time.Now().UTC()
	for _, issue := range issues {
		if !issue.PollIntervalMinutes.Valid {
			slog.Warn("polling scheduler: issue has no interval, skipping",
				"issue_id", util.UUIDToString(issue.ID))
			continue
		}

		var startAt time.Time
		if issue.PollStartAt.Valid {
			startAt = issue.PollStartAt.Time
		}
		nextRun := service.InitialPollingNextRun(startAt, issue.PollIntervalMinutes.Int32, now)

		_, err := queries.UpdateIssue(ctx, db.UpdateIssueParams{
			ID:          issue.ID,
			PollNextRun: pgtype.Timestamptz{Time: nextRun, Valid: true},
			// Preserve all other fields.
			Title:               pgtype.Text{String: issue.Title, Valid: true},
			Description:         issue.Description,
			Status:              pgtype.Text{String: issue.Status, Valid: true},
			Priority:            pgtype.Text{String: issue.Priority, Valid: true},
			AssigneeType:        issue.AssigneeType,
			AssigneeID:          issue.AssigneeID,
			StartDate:           issue.StartDate,
			DueDate:             issue.DueDate,
			ParentIssueID:       issue.ParentIssueID,
			ProjectID:           issue.ProjectID,
			PollStartAt:         issue.PollStartAt,
			PollIntervalMinutes: issue.PollIntervalMinutes,
			PollLastRun:         issue.PollLastRun,
			PollRunCount:        pgtype.Int4{Int32: issue.PollRunCount, Valid: true},
		})
		if err != nil {
			slog.Warn("polling scheduler: failed to recover issue",
				"issue_id", util.UUIDToString(issue.ID), "error", err)
		}
	}
}

// tickPollingIssues claims all due polling issues and enqueues tasks.
func tickPollingIssues(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService) {
	issues, err := queries.ListPollingIssuesDue(ctx, 100)
	if err != nil {
		slog.Warn("polling scheduler: failed to list due issues", "error", err)
		return
	}
	if len(issues) == 0 {
		return
	}

	slog.Info("polling scheduler: found due issues", "count", len(issues))
	now := time.Now().UTC()

	for _, issue := range issues {
		// Double-check the issue is still polling (could have changed
		// between the query and now).
		if issue.Status != "polling" {
			continue
		}

		// Skip if no assignee — polling issues need an agent to execute.
		if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
			slog.Warn("polling scheduler: issue has no assignee, skipping",
				"issue_id", util.UUIDToString(issue.ID))
			continue
		}

		// Enqueue task — the task service will handle the rest.
		// For polling issues, we don't change the issue status to in_progress.
		taskSvc.EnqueueTaskForPollingIssue(ctx, issue)

		// Advance the next run time.
		interval := int32(30) // default
		if issue.PollIntervalMinutes.Valid {
			interval = issue.PollIntervalMinutes.Int32
		}

		var lastRun time.Time
		if issue.PollLastRun.Valid {
			lastRun = issue.PollLastRun.Time
		} else {
			lastRun = now
		}

		nextRun := service.AdvancePollingNextRun(lastRun, interval, now)
		runCount := issue.PollRunCount + 1

		_, err := queries.UpdateIssue(ctx, db.UpdateIssueParams{
			ID:     issue.ID,
			Status: pgtype.Text{String: "polling", Valid: true},
			// Preserve all other fields.
			Title:               pgtype.Text{String: issue.Title, Valid: true},
			Description:         issue.Description,
			Priority:            pgtype.Text{String: issue.Priority, Valid: true},
			AssigneeType:        issue.AssigneeType,
			AssigneeID:          issue.AssigneeID,
			StartDate:           issue.StartDate,
			DueDate:             issue.DueDate,
			ParentIssueID:       issue.ParentIssueID,
			ProjectID:           issue.ProjectID,
			PollStartAt:         issue.PollStartAt,
			PollIntervalMinutes: issue.PollIntervalMinutes,
			PollNextRun:         pgtype.Timestamptz{Time: nextRun, Valid: true},
			PollLastRun:         pgtype.Timestamptz{Time: now, Valid: true},
			PollRunCount:        pgtype.Int4{Int32: runCount, Valid: true},
		})
		if err != nil {
			slog.Warn("polling scheduler: failed to advance issue next run",
				"issue_id", util.UUIDToString(issue.ID), "error", err)
		}
	}
}
