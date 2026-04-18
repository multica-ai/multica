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

// AutopilotIssueCreateRequest is the write-through-agnostic shape autopilot
// passes to the IssueCreator. It mirrors the handler.CreateIssueRequest fields
// autopilot needs. Kept in the service package to avoid an import cycle
// (handler already depends on service).
type AutopilotIssueCreateRequest struct {
	Title        string
	Description  string
	Status       string
	Priority     string
	AssigneeType string
	AssigneeID   string
}

// IssueCreator is the narrow interface AutopilotService uses to create issues.
// In production the handler's CreateIssueInternal satisfies this via a tiny
// adapter wired from cmd/server. Keeping the interface narrow avoids pulling
// the whole handler surface into the service package and sidesteps the
// service → handler import that would create a cycle.
type IssueCreator interface {
	CreateIssueForAutopilot(ctx context.Context, workspaceID, actorType, actorID string, req AutopilotIssueCreateRequest) (db.Issue, error)
}

type AutopilotService struct {
	Queries      *db.Queries
	TxStarter    TxStarter
	Bus          *events.Bus
	TaskSvc      *TaskService
	IssueCreator IssueCreator
}

func NewAutopilotService(q *db.Queries, tx TxStarter, bus *events.Bus, taskSvc *TaskService) *AutopilotService {
	return &AutopilotService{Queries: q, TxStarter: tx, Bus: bus, TaskSvc: taskSvc}
}

// SetIssueCreator wires the HTTP-agnostic issue-creation entry point. Called
// from cmd/server once the handler exists. When nil, dispatchCreateIssue falls
// back to the legacy direct-DB path (preserves pre-Phase-4 behavior for tests
// that don't wire the handler).
func (s *AutopilotService) SetIssueCreator(ic IssueCreator) {
	s.IssueCreator = ic
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
//
// Phase 4 rewires this through the handler's CreateIssueInternal so connected
// workspaces see autopilot-generated issues on GitLab. The handler owns the
// full write-through path (GitLab POST → cache upsert → event publish →
// task enqueue), so this function's job is reduced to:
//
//  1. Build the create request from the autopilot template.
//  2. Use the agent as the acting identity (agent actor → service PAT
//     for the GitLab call).
//  3. After the handler returns, record the autopilot_issue mapping keyed
//     by workspace_id + gitlab_iid (so listeners can resolve the run from
//     a plain GitLab-synced cache row without origin_type/origin_id
//     markers). Mapping is skipped on non-GitLab workspaces — the legacy
//     autopilot_run.issue_id link stays authoritative there.
//
// Falls back to the legacy direct-DB path when IssueCreator isn't wired
// (production always wires it; some tests don't bother when they're not
// exercising the write path).
func (s *AutopilotService) dispatchCreateIssue(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun) error {
	if s.IssueCreator == nil {
		return s.dispatchCreateIssueLegacy(ctx, ap, run)
	}

	title := s.interpolateTemplate(ap)
	description := s.buildIssueDescription(ap)

	// Autopilot impersonates the assignee agent for the GitLab call —
	// the handler's resolveActor semantics ("agent" actor → service PAT)
	// match exactly what we want here. The creator-type field on the
	// underlying cache row is set from the actor, so "agent" is also the
	// accurate creator for autopilot-generated issues (human who set up
	// the autopilot is recorded as ap.CreatedByID on the autopilot row).
	req := AutopilotIssueCreateRequest{
		Title:        title,
		Description:  description.String,
		Status:       "todo",
		Priority:     ap.Priority,
		AssigneeType: "agent",
		AssigneeID:   util.UUIDToString(ap.AssigneeID),
	}

	issue, err := s.IssueCreator.CreateIssueForAutopilot(
		ctx,
		util.UUIDToString(ap.WorkspaceID),
		"agent",
		util.UUIDToString(ap.AssigneeID),
		req,
	)
	if err != nil {
		return fmt.Errorf("create issue: %w", err)
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

	// Record the autopilot_issue mapping for connected workspaces. For legacy
	// (non-GitLab) workspaces the cache row has no gitlab_iid and listeners
	// resolve the run via autopilot_run.issue_id — skip the mapping.
	if issue.GitlabIid.Valid {
		if _, err := s.Queries.UpsertAutopilotIssue(ctx, db.UpsertAutopilotIssueParams{
			AutopilotRunID: run.ID,
			WorkspaceID:    ap.WorkspaceID,
			GitlabIid:      issue.GitlabIid.Int32,
		}); err != nil {
			return fmt.Errorf("autopilot_issue mapping: %w", err)
		}
	}

	// Note: CreateIssueInternal already published issue:created AND enqueued
	// the agent task (shouldEnqueueAgentTask gates on status != backlog, and
	// autopilot always creates issues in "todo"). We deliberately do NOT
	// re-emit either — double-publishing breaks subscriber/notification
	// listeners that assume a single create event.

	slog.Info("autopilot dispatched (create_issue)",
		"autopilot_id", util.UUIDToString(ap.ID),
		"issue_id", util.UUIDToString(issue.ID),
		"run_id", util.UUIDToString(run.ID),
	)
	return nil
}

// dispatchCreateIssueLegacy is the pre-Phase-4 direct-DB path. Preserved as a
// fallback for tests that construct AutopilotService without wiring an
// IssueCreator. Production always wires it via cmd/server.
func (s *AutopilotService) dispatchCreateIssueLegacy(ctx context.Context, ap db.Autopilot, run *db.AutopilotRun) error {
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
		Priority:      ap.Priority,
		AssigneeType:  pgtype.Text{String: "agent", Valid: true},
		AssigneeID:    ap.AssigneeID,
		CreatorType:   pgtype.Text{String: ap.CreatedByType, Valid: ap.CreatedByType != ""},
		CreatorID:     ap.CreatedByID,
		ParentIssueID: pgtype.UUID{},
		Position:      0,
		DueDate:       pgtype.Timestamptz{},
		Number:        issueNumber,
		ProjectID:     ap.ProjectID,
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

	slog.Info("autopilot dispatched (create_issue, legacy direct-DB)",
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
		Priority:       priorityToInt(ap.Priority),
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
	run, ok := s.resolveActiveRunForIssue(ctx, issue)
	if !ok {
		return
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

// resolveActiveRunForIssue finds the active autopilot run that owns an issue.
// It prefers the Phase 4 autopilot_issue mapping (workspace_id + gitlab_iid),
// which is the forward-looking path — it works for issues created via GitLab
// write-through that do not carry an origin marker. It falls back to the legacy
// origin_type/origin_id path for rows created before the mapping existed.
// Phase 5 will drop the origin_type/origin_id columns and remove the fallback.
//
// Only returns runs in an active status (issue_created / running) — matches the
// existing GetAutopilotRunByIssue semantics, so terminal runs are never
// re-synced.
func (s *AutopilotService) resolveActiveRunForIssue(ctx context.Context, issue db.Issue) (db.AutopilotRun, bool) {
	// Phase 4 path: mapping keyed by workspace_id + gitlab_iid.
	if issue.GitlabIid.Valid {
		mapping, err := s.Queries.GetAutopilotIssueByIID(ctx, db.GetAutopilotIssueByIIDParams{
			WorkspaceID: issue.WorkspaceID,
			GitlabIid:   issue.GitlabIid.Int32,
		})
		if err == nil {
			run, err := s.Queries.GetAutopilotRun(ctx, mapping.AutopilotRunID)
			if err == nil && isActiveRunStatus(run.Status) {
				return run, true
			}
		}
	}

	// Legacy fallback: origin_type/origin_id. Remove in Phase 5.
	if issue.OriginType.Valid && issue.OriginType.String == "autopilot" {
		run, err := s.Queries.GetAutopilotRunByIssue(ctx, issue.ID)
		if err == nil {
			return run, true
		}
	}

	return db.AutopilotRun{}, false
}

// isActiveRunStatus reports whether an autopilot run is still open for
// status-transition syncing. Mirrors the filter on GetAutopilotRunByIssue.
func isActiveRunStatus(status string) bool {
	return status == "issue_created" || status == "running"
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
