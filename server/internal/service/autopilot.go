package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/redact"
)

type autopilotTxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type AutopilotService struct {
	Queries     *db.Queries
	TxStarter   autopilotTxStarter
	TaskService *TaskService
}

func NewAutopilotService(q *db.Queries, txStarter autopilotTxStarter, taskService *TaskService) *AutopilotService {
	return &AutopilotService{
		Queries:     q,
		TxStarter:   txStarter,
		TaskService: taskService,
	}
}

func (s *AutopilotService) TriggerCreateIssue(ctx context.Context, autopilot db.Autopilot, actorType string, actorID pgtype.UUID) (db.AutopilotRun, error) {
	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		WorkspaceID:  autopilot.WorkspaceID,
		AutopilotID:  autopilot.ID,
		TriggerID:    pgtype.UUID{},
		Source:       "manual",
		ScheduledFor: pgtype.Timestamptz{},
	})
	if err != nil {
		return db.AutopilotRun{}, fmt.Errorf("create autopilot run: %w", err)
	}

	return s.executeCreateIssueRun(ctx, autopilot, run, actorType, actorID)
}

type ClaimedAutopilotSchedule struct {
	Autopilot db.Autopilot
	Trigger   db.AutopilotTrigger
	Run       db.AutopilotRun
}

func (s *AutopilotService) ProcessDueSchedules(ctx context.Context, now time.Time, limit int32) ([]db.AutopilotRun, error) {
	claimed, err := s.ClaimDueSchedules(ctx, now, limit)
	if err != nil {
		return nil, err
	}

	runs := make([]db.AutopilotRun, 0, len(claimed))
	runErrs := make([]error, 0)
	for _, claim := range claimed {
		actorType, actorID := scheduledRunActor(claim.Autopilot)
		run, err := s.executeCreateIssueRun(ctx, claim.Autopilot, claim.Run, actorType, actorID)
		runs = append(runs, run)
		if err != nil {
			runErrs = append(runErrs, err)
		}
	}

	return runs, errors.Join(runErrs...)
}

func (s *AutopilotService) ClaimDueSchedules(ctx context.Context, now time.Time, limit int32) ([]ClaimedAutopilotSchedule, error) {
	if limit <= 0 {
		limit = 20
	}

	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin schedule claim transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)
	rows, err := qtx.ClaimDueAutopilotSchedules(ctx, db.ClaimDueAutopilotSchedulesParams{
		Now:        pgtype.Timestamptz{Time: now, Valid: true},
		ClaimLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("claim due autopilot schedules: %w", err)
	}

	claimed := make([]ClaimedAutopilotSchedule, 0, len(rows))
	for _, row := range rows {
		autopilot := autopilotFromScheduleClaim(row)
		trigger := triggerFromScheduleClaim(row)
		scheduledFor := trigger.NextRunAt
		if !scheduledFor.Valid {
			continue
		}

		nextRunAt, err := NextScheduleRunAt(trigger.Cron.String, trigger.Timezone, now)
		if err != nil {
			return nil, fmt.Errorf("compute next schedule for trigger %s: %w", util.UUIDToString(trigger.ID), err)
		}

		idempotencyKey := ScheduleIdempotencyKey(trigger.ID, scheduledFor.Time)
		run, err := qtx.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
			WorkspaceID:    autopilot.WorkspaceID,
			AutopilotID:    autopilot.ID,
			TriggerID:      trigger.ID,
			Source:         "schedule",
			ScheduledFor:   scheduledFor,
			IdempotencyKey: util.StrToText(idempotencyKey),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				if _, advanceErr := qtx.AdvanceAutopilotTrigger(ctx, db.AdvanceAutopilotTriggerParams{
					ID:        trigger.ID,
					LastRunAt: scheduledFor,
					NextRunAt: pgtype.Timestamptz{Time: nextRunAt, Valid: true},
				}); advanceErr != nil {
					return nil, fmt.Errorf("advance duplicate autopilot trigger: %w", advanceErr)
				}
				continue
			}
			return nil, fmt.Errorf("create scheduled autopilot run: %w", err)
		}

		if _, err := qtx.AdvanceAutopilotTrigger(ctx, db.AdvanceAutopilotTriggerParams{
			ID:        trigger.ID,
			LastRunAt: scheduledFor,
			NextRunAt: pgtype.Timestamptz{Time: nextRunAt, Valid: true},
		}); err != nil {
			return nil, fmt.Errorf("advance autopilot trigger: %w", err)
		}

		claimed = append(claimed, ClaimedAutopilotSchedule{
			Autopilot: autopilot,
			Trigger:   trigger,
			Run:       run,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit schedule claim transaction: %w", err)
	}

	return claimed, nil
}

func (s *AutopilotService) executeCreateIssueRun(ctx context.Context, autopilot db.Autopilot, run db.AutopilotRun, actorType string, actorID pgtype.UUID) (db.AutopilotRun, error) {
	issue, err := s.createIssueForRun(ctx, autopilot, run, actorType, actorID)
	if err != nil {
		failed := s.markRunFailed(ctx, run.ID, pgtype.UUID{}, err)
		s.recordActivity(ctx, autopilot.WorkspaceID, pgtype.UUID{}, actorType, actorID, "autopilot_run_failed", map[string]any{
			"autopilot_id": util.UUIDToString(autopilot.ID),
			"run_id":       util.UUIDToString(run.ID),
			"source":       run.Source,
			"error":        sanitizeRunError(err),
		})
		return failed, err
	}

	task, err := s.TaskService.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		failed := s.markRunFailed(ctx, run.ID, issue.ID, err)
		s.recordActivity(ctx, autopilot.WorkspaceID, issue.ID, actorType, actorID, "autopilot_run_failed", map[string]any{
			"autopilot_id": util.UUIDToString(autopilot.ID),
			"run_id":       util.UUIDToString(run.ID),
			"issue_id":     util.UUIDToString(issue.ID),
			"source":       run.Source,
			"error":        sanitizeRunError(err),
		})
		return failed, err
	}

	completed, err := s.Queries.CompleteAutopilotRunSucceeded(ctx, db.CompleteAutopilotRunSucceededParams{
		ID:             run.ID,
		CreatedIssueID: issue.ID,
		CreatedTaskID:  task.ID,
	})
	if err != nil {
		return run, fmt.Errorf("complete autopilot run: %w", err)
	}

	s.recordActivity(ctx, autopilot.WorkspaceID, issue.ID, actorType, actorID, "autopilot_run_succeeded", map[string]any{
		"autopilot_id": util.UUIDToString(autopilot.ID),
		"run_id":       util.UUIDToString(completed.ID),
		"issue_id":     util.UUIDToString(issue.ID),
		"task_id":      util.UUIDToString(task.ID),
		"source":       completed.Source,
	})

	return completed, nil
}

func autopilotFromScheduleClaim(row db.ClaimDueAutopilotSchedulesRow) db.Autopilot {
	return db.Autopilot{
		ID:                 row.AutopilotID,
		WorkspaceID:        row.AutopilotWorkspaceID,
		Title:              row.AutopilotTitle,
		Description:        row.AutopilotDescription,
		Status:             row.AutopilotStatus,
		Mode:               row.AutopilotMode,
		AgentID:            row.AutopilotAgentID,
		ProjectID:          row.AutopilotProjectID,
		Priority:           row.AutopilotPriority,
		IssueTitleTemplate: row.AutopilotIssueTitleTemplate,
		CreatedBy:          row.AutopilotCreatedBy,
		CreatedAt:          row.AutopilotCreatedAt,
		UpdatedAt:          row.AutopilotUpdatedAt,
		DeletedAt:          row.AutopilotDeletedAt,
	}
}

func triggerFromScheduleClaim(row db.ClaimDueAutopilotSchedulesRow) db.AutopilotTrigger {
	return db.AutopilotTrigger{
		ID:          row.TriggerID,
		AutopilotID: row.TriggerAutopilotID,
		Type:        row.TriggerType,
		Label:       row.TriggerLabel,
		Cron:        row.TriggerCron,
		Timezone:    row.TriggerTimezone,
		Status:      row.TriggerStatus,
		NextRunAt:   row.TriggerNextRunAt,
		LastRunAt:   row.TriggerLastRunAt,
		CreatedAt:   row.TriggerCreatedAt,
		UpdatedAt:   row.TriggerUpdatedAt,
	}
}

func scheduledRunActor(autopilot db.Autopilot) (string, pgtype.UUID) {
	if autopilot.CreatedBy.Valid {
		return "member", autopilot.CreatedBy
	}
	return "agent", autopilot.AgentID
}

func (s *AutopilotService) createIssueForRun(ctx context.Context, autopilot db.Autopilot, run db.AutopilotRun, actorType string, actorID pgtype.UUID) (db.Issue, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Issue{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)
	issueNumber, err := qtx.IncrementIssueCounter(ctx, autopilot.WorkspaceID)
	if err != nil {
		return db.Issue{}, fmt.Errorf("increment issue counter: %w", err)
	}

	description := buildAutopilotIssueDescription(autopilot, run)
	issue, err := qtx.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:   autopilot.WorkspaceID,
		Title:         renderAutopilotIssueTitle(autopilot, run),
		Description:   util.PtrToText(&description),
		Status:        "todo",
		Priority:      autopilot.Priority,
		AssigneeType:  util.StrToText("agent"),
		AssigneeID:    autopilot.AgentID,
		CreatorType:   actorType,
		CreatorID:     actorID,
		Position:      0,
		Number:        issueNumber,
		ProjectID:     autopilot.ProjectID,
		ParentIssueID: pgtype.UUID{},
		DueDate:       pgtype.Timestamptz{},
	})
	if err != nil {
		return db.Issue{}, fmt.Errorf("create issue: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return db.Issue{}, fmt.Errorf("commit issue: %w", err)
	}

	return issue, nil
}

func (s *AutopilotService) markRunFailed(ctx context.Context, runID, issueID pgtype.UUID, runErr error) db.AutopilotRun {
	run, err := s.Queries.CompleteAutopilotRunFailed(ctx, db.CompleteAutopilotRunFailedParams{
		ID:             runID,
		Error:          util.StrToText(sanitizeRunError(runErr)),
		CreatedIssueID: issueID,
	})
	if err != nil {
		return db.AutopilotRun{ID: runID, Status: "failed", CreatedIssueID: issueID}
	}
	return run
}

func (s *AutopilotService) recordActivity(ctx context.Context, workspaceID, issueID pgtype.UUID, actorType string, actorID pgtype.UUID, action string, details map[string]any) {
	if actorType == "" {
		actorType = "system"
	}
	encoded, err := json.Marshal(redact.InputMap(details))
	if err != nil {
		encoded = []byte("{}")
	}
	_, _ = s.Queries.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		ActorType:   util.StrToText(actorType),
		ActorID:     actorID,
		Action:      action,
		Details:     encoded,
	})
}

func renderAutopilotIssueTitle(autopilot db.Autopilot, run db.AutopilotRun) string {
	template := strings.TrimSpace(autopilot.IssueTitleTemplate)
	if template == "" {
		return autopilot.Title
	}
	replacer := strings.NewReplacer(
		"{{autopilot.title}}", autopilot.Title,
		"{{run.id}}", util.UUIDToString(run.ID),
		"{{source}}", run.Source,
	)
	return strings.TrimSpace(replacer.Replace(template))
}

func buildAutopilotIssueDescription(autopilot db.Autopilot, run db.AutopilotRun) string {
	var b strings.Builder
	b.WriteString("Autopilot: ")
	b.WriteString(autopilot.Title)
	b.WriteString("\nAutopilot ID: ")
	b.WriteString(util.UUIDToString(autopilot.ID))
	b.WriteString("\nRun ID: ")
	b.WriteString(util.UUIDToString(run.ID))
	b.WriteString("\nSource: ")
	b.WriteString(run.Source)

	if autopilot.Description.Valid && strings.TrimSpace(autopilot.Description.String) != "" {
		b.WriteString("\n\n")
		b.WriteString(autopilot.Description.String)
	}

	return b.String()
}

func sanitizeRunError(err error) string {
	msg := redact.Text(err.Error())
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	return msg
}
