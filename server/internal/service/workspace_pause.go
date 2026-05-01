package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// IsWorkspacePaused returns true when the workspace's kill switch is engaged
// (settings.tasks_paused = true). Errors are logged and treated as "not paused"
// so a transient DB hiccup does not silently block all task creation.
func (s *TaskService) IsWorkspacePaused(ctx context.Context, workspaceID pgtype.UUID) bool {
	if !workspaceID.Valid {
		return false
	}
	paused, err := s.Queries.IsWorkspacePaused(ctx, workspaceID)
	if err != nil {
		slog.Warn("is_workspace_paused lookup failed", "workspace_id", util.UUIDToString(workspaceID), "error", err)
		return false
	}
	return paused
}

// PauseWorkspaceTasks engages the kill switch: flips settings.tasks_paused to
// true and cancels every queued/dispatched/running task in the workspace.
// Returns the count of cancelled tasks. Broadcasts task:cancelled events for
// each task and a workspace:updated event so all clients reflect the new state.
func (s *TaskService) PauseWorkspaceTasks(ctx context.Context, workspaceID pgtype.UUID) (int, error) {
	if !workspaceID.Valid {
		return 0, fmt.Errorf("workspace id required")
	}

	// Cancel first, then flip the flag — if cancellation fails we don't want
	// the UI to claim the kill switch is engaged while tasks are still alive.
	cancelled, err := s.Queries.CancelAllActiveTasksByWorkspace(ctx, workspaceID)
	if err != nil {
		return 0, fmt.Errorf("cancel active tasks: %w", err)
	}

	wsIDStr := util.UUIDToString(workspaceID)
	for _, task := range cancelled {
		s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, task)
		s.ReconcileAgentStatus(ctx, task.AgentID)
		slog.Info("task cancelled by workspace pause", "task_id", util.UUIDToString(task.ID), "workspace_id", wsIDStr)
	}

	slog.Info("workspace tasks paused", "workspace_id", wsIDStr, "cancelled_count", len(cancelled))
	return len(cancelled), nil
}
