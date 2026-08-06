package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CreateFreshSessionTaskTx creates the task half of a Handoff inside the
// caller's transaction. The caller publishes it only after commit, so the new
// root comment, Handoff metadata, closed old thread, and queued task are atomic.
func (s *TaskService) CreateFreshSessionTaskTx(
	ctx context.Context,
	tx pgx.Tx,
	issue db.Issue,
	triggerCommentID pgtype.UUID,
	triggerContent string,
	delegation TaskDelegationContext,
) (db.AgentTaskQueue, error) {
	if s == nil || tx == nil {
		return db.AgentTaskQueue{}, fmt.Errorf("task service is unavailable")
	}
	if !delegation.OriginalUserID.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("agent delegation denied: missing original user")
	}
	if s.IsWorkspacePaused(ctx, issue.WorkspaceID) {
		return db.AgentTaskQueue{}, fmt.Errorf("workspace tasks are paused")
	}
	if !issue.AssigneeID.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("issue has no assignee")
	}

	queries := s.Queries.WithTx(tx)
	agent, err := queries.GetAgent(ctx, issue.AssigneeID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("agent has no runtime")
	}

	summary := truncateForSummary(triggerContent, triggerSummaryMaxLen)
	task, err := queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		AgentID:           issue.AssigneeID,
		RuntimeID:         agent.RuntimeID,
		IssueID:           issue.ID,
		Priority:          priorityToInt(issue.Priority),
		TriggerCommentID:  triggerCommentID,
		TriggerSummary:    pgtype.Text{String: summary, Valid: summary != ""},
		Title:             pgtype.Text{String: truncateForSummary(issue.Title, triggerSummaryMaxLen), Valid: issue.Title != ""},
		ForceFreshSession: pgtype.Bool{Bool: true, Valid: true},
		OriginalUserID:    delegation.OriginalUserID,
		DelegatingAgentID: delegation.DelegatingAgentID,
		SourceTaskID:      delegation.SourceTaskID,
		DelegationSource:  delegation.Source,
	})
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("create task: %w", err)
	}
	return task, nil
}

// PublishCreatedTask emits the normal queued event and daemon notification for
// a task that has already committed with a larger Cerebro transaction.
func (s *TaskService) PublishCreatedTask(ctx context.Context, task db.AgentTaskQueue) {
	if s == nil {
		return
	}
	slog.Info("task enqueued",
		"task_id", util.UUIDToString(task.ID),
		"issue_id", util.UUIDToString(task.IssueID),
		"agent_id", util.UUIDToString(task.AgentID),
		"force_fresh_session", true,
	)
	if s.Bus != nil {
		s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	}
	s.NotifyTaskEnqueued(ctx, task)
}
