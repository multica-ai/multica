package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TxStarter abstracts transaction creation (satisfied by pgxpool.Pool).
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type AutopilotService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Bus       *events.Bus
	TaskSvc   *TaskService
}

func NewAutopilotService(q *db.Queries, tx TxStarter, bus *events.Bus, taskSvc *TaskService) *AutopilotService {
	return &AutopilotService{Queries: q, TxStarter: tx, Bus: bus, TaskSvc: taskSvc}
}

// DispatchAutopilot is the core execution entry point.
// It creates a run and either creates an issue or enqueues a direct agent task
// depending on execution_mode.
func (s *AutopilotService) DispatchAutopilot(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
) (*db.AutopilotRun, error) {
	// Determine initial status based on execution mode.
	initialStatus := "issue_created"
	if autopilot.ExecutionMode == "run_only" {
		initialStatus = "running"
	}

	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID:    autopilot.ID,
		TriggerID:      triggerID,
		Source:         source,
		Status:         initialStatus,
		TriggerPayload: payload,
	})
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	switch autopilot.ExecutionMode {
	case "create_issue":
		if err := s.dispatchCreateIssue(ctx, autopilot, &run); err != nil {
			s.failRun(ctx, run.ID, err.Error())
			return &run, fmt.Errorf("dispatch create_issue: %w", err)
		}
	case "run_only":
		if err := s.dispatchRunOnly(ctx, autopilot, &run); err != nil {
			s.failRun(ctx, run.ID, err.Error())
			return &run, fmt.Errorf("dispatch run_only: %w", err)
		}
	default:
		s.failRun(ctx, run.ID, "unknown execution_mode: "+autopilot.ExecutionMode)
		return &run, fmt.Errorf("unknown execution_mode: %s", autopilot.ExecutionMode)
	}

	// Update last_run_at on the autopilot.
	s.Queries.UpdateAutopilotLastRunAt(ctx, autopilot.ID)

	// Publish run start event.
	s.Bus.Publish(events.Event{
		Type:        protocol.EventAutopilotRunStart,
		WorkspaceID: util.UUIDToString(autopilot.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"run_id":       util.UUIDToString(run.ID),
			"autopilot_id": util.UUIDToString(autopilot.ID),
			"source":       source,
			"status":       run.Status,
		},
	})

	return &run, nil
}

// dispatchCreateIssue creates an issue and enqueues a task for the agent.
func (s *AutopilotService) dispatchCreateIssue(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)

	// Get next issue number.
	issueNumber, err := qtx.IncrementIssueCounter(ctx, ap.WorkspaceID)
	if err != nil {
		return fmt.Errorf("increment issue counter: %w", err)
	}

	title := s.interpolateTemplate(ap)
	description := s.buildIssueDescription(ap)

	issue, err := qtx.CreateIssueWithOrigin(ctx, db.CreateIssueWithOriginParams{
		WorkspaceID:   ap.WorkspaceID,
		Title:         title,
		Description:   description,
		Status:        "todo",
		Priority:      "none",
		AssigneeType:  pgtype.Text{String: "agent", Valid: true},
		AssigneeID:    ap.AssigneeID,
		CreatorType:   ap.CreatedByType,
		CreatorID:     ap.CreatedByID,
		ParentIssueID: pgtype.UUID{},
		Position:      0,
		DueDate:       pgtype.Timestamptz{},
		Number:        issueNumber,
		ProjectID:     pgtype.UUID{},
		OriginType:    pgtype.Text{String: "autopilot", Valid: true},
		OriginID:      ap.ID,
	})
	if err != nil {
		return fmt.Errorf("create issue: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	// Update run with the linked issue.
	updatedRun, err := s.Queries.UpdateAutopilotRunIssueCreated(ctx, db.UpdateAutopilotRunIssueCreatedParams{
		ID:      run.ID,
		IssueID: issue.ID,
	})
	if err != nil {
		return fmt.Errorf("link run to issue: %w", err)
	}
	*run = updatedRun

	// Publish issue:created so the existing event chain fires
	// (subscriber listeners, activity listeners, notification listeners).
	prefix := s.getIssuePrefix(ap.WorkspaceID)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: util.UUIDToString(ap.WorkspaceID),
		ActorType:   ap.CreatedByType,
		ActorID:     util.UUIDToString(ap.CreatedByID),
		Payload: map[string]any{
			"issue": issueToMap(issue, prefix),
		},
	})

	// Enqueue agent task via the existing flow.
	if _, err := s.TaskSvc.EnqueueTaskForIssue(ctx, issue); err != nil {
		return fmt.Errorf("enqueue task for issue: %w", err)
	}

	slog.Info("autopilot dispatched (create_issue)",
		"autopilot_id", util.UUIDToString(ap.ID),
		"issue_id", util.UUIDToString(issue.ID),
		"run_id", util.UUIDToString(run.ID),
	)
	return nil
}

// dispatchRunOnly enqueues a direct agent task without creating an issue.
func (s *AutopilotService) dispatchRunOnly(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun) error {
	agent, err := s.Queries.GetAgent(ctx, ap.AssigneeID)
	if err != nil {
		return fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return fmt.Errorf("agent has no runtime")
	}

	task, err := s.Queries.CreateAutopilotTask(ctx, db.CreateAutopilotTaskParams{
		AgentID:        ap.AssigneeID,
		RuntimeID:      agent.RuntimeID,
		Priority:       0,
		AutopilotRunID: run.ID,
	})
	if err != nil {
		return fmt.Errorf("create autopilot task: %w", err)
	}

	// Update run with task reference.
	updatedRun, err := s.Queries.UpdateAutopilotRunRunning(ctx, db.UpdateAutopilotRunRunningParams{
		ID:     run.ID,
		TaskID: task.ID,
	})
	if err != nil {
		slog.Warn("failed to update run with task_id", "run_id", util.UUIDToString(run.ID), "error", err)
	} else {
		*run = updatedRun
	}

	slog.Info("autopilot dispatched (run_only)",
		"autopilot_id", util.UUIDToString(ap.ID),
		"task_id", util.UUIDToString(task.ID),
		"run_id", util.UUIDToString(run.ID),
	)
	return nil
}

// SyncRunFromIssue updates the autopilot run when its linked issue reaches a terminal status.
func (s *AutopilotService) SyncRunFromIssue(ctx context.Context, issue db.Issue) {
	if !issue.OriginType.Valid || issue.OriginType.String != "autopilot" {
		return
	}

	run, err := s.Queries.GetAutopilotRunByIssue(ctx, issue.ID)
	if err != nil {
		return // no active run linked to this issue
	}

	wsID := util.UUIDToString(issue.WorkspaceID)

	switch issue.Status {
	case "done", "in_review":
		if _, err := s.Queries.UpdateAutopilotRunCompleted(ctx, db.UpdateAutopilotRunCompletedParams{
			ID: run.ID,
		}); err != nil {
			slog.Warn("failed to complete autopilot run", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.publishRunDone(wsID, run, "completed")
	case "cancelled", "blocked":
		reason := "issue " + issue.Status
		if _, err := s.Queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
			ID:            run.ID,
			FailureReason: pgtype.Text{String: reason, Valid: true},
		}); err != nil {
			slog.Warn("failed to fail autopilot run", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.publishRunDone(wsID, run, "failed")
	}
}

// SyncRunFromTask updates the autopilot run when a run_only task completes or fails.
func (s *AutopilotService) SyncRunFromTask(ctx context.Context, task db.AgentTaskQueue) {
	if !task.AutopilotRunID.Valid {
		return
	}

	run, err := s.Queries.GetAutopilotRun(ctx, task.AutopilotRunID)
	if err != nil {
		return
	}

	autopilot, err := s.Queries.GetAutopilot(ctx, run.AutopilotID)
	if err != nil {
		return
	}
	wsID := util.UUIDToString(autopilot.WorkspaceID)

	switch task.Status {
	case "completed":
		if _, err := s.Queries.UpdateAutopilotRunCompleted(ctx, db.UpdateAutopilotRunCompletedParams{
			ID:     run.ID,
			Result: task.Result,
		}); err != nil {
			slog.Warn("failed to complete autopilot run from task", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.publishRunDone(wsID, run, "completed")
	case "failed", "cancelled":
		reason := "task " + task.Status
		if task.Error.Valid {
			reason = task.Error.String
		}
		if _, err := s.Queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
			ID:            run.ID,
			FailureReason: pgtype.Text{String: reason, Valid: true},
		}); err != nil {
			slog.Warn("failed to fail autopilot run from task", "run_id", util.UUIDToString(run.ID), "error", err)
			return
		}
		s.publishRunDone(wsID, run, "failed")
	}
}


func (s *AutopilotService) failRun(ctx context.Context, runID pgtype.UUID, reason string) {
	if _, err := s.Queries.UpdateAutopilotRunFailed(ctx, db.UpdateAutopilotRunFailedParams{
		ID:            runID,
		FailureReason: pgtype.Text{String: reason, Valid: true},
	}); err != nil {
		slog.Warn("failed to mark autopilot run as failed", "run_id", util.UUIDToString(runID), "error", err)
	}
}

func (s *AutopilotService) publishRunDone(workspaceID string, run db.AutopilotRun, status string) {
	s.Bus.Publish(events.Event{
		Type:        protocol.EventAutopilotRunDone,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"run_id":       util.UUIDToString(run.ID),
			"autopilot_id": util.UUIDToString(run.AutopilotID),
			"status":       status,
		},
	})
}

// buildIssueDescription appends an autopilot system instruction to the
// user-provided description, asking the agent to rename the issue after
// it understands the actual work.
func (s *AutopilotService) buildIssueDescription(ap db.Autopilot) pgtype.Text {
	now := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	note := fmt.Sprintf("\n\n---\n*Autopilot run triggered at %s. After starting work, rename this issue to accurately reflect what you are doing.*", now)
	base := ap.Description.String
	return pgtype.Text{String: base + note, Valid: true}
}

// interpolateTemplate replaces {{date}} in the issue title template.
func (s *AutopilotService) interpolateTemplate(ap db.Autopilot) string {
	tmpl := ap.Title
	if ap.IssueTitleTemplate.Valid && ap.IssueTitleTemplate.String != "" {
		tmpl = ap.IssueTitleTemplate.String
	}
	now := time.Now().UTC().Format("2006-01-02")
	return strings.ReplaceAll(tmpl, "{{date}}", now)
}

func (s *AutopilotService) getIssuePrefix(workspaceID pgtype.UUID) string {
	ws, err := s.Queries.GetWorkspace(context.Background(), workspaceID)
	if err != nil {
		return ""
	}
	return ws.IssuePrefix
}
