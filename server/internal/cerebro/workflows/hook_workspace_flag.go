package workflows

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// WorkspaceFlagResolver answers whether a workspace still runs Workflow
// decisions. Every seam that can block, modify, or require work consults it
// per event so a workspace can switch the whole engine off without a deploy.
type WorkspaceFlagResolver interface {
	WorkflowHooksEnabledForWorkspace(ctx context.Context, workspaceID string) (bool, error)
}

// PostgresWorkspaceFlagResolver reads the cerebro_workflow_hooks workspace
// override written by the feature-flag surface.
type PostgresWorkspaceFlagResolver struct {
	db cerebrodb.DBTX
}

func NewPostgresWorkspaceFlagResolver(db cerebrodb.DBTX) *PostgresWorkspaceFlagResolver {
	return &PostgresWorkspaceFlagResolver{db: db}
}

func (r *PostgresWorkspaceFlagResolver) WorkflowHooksEnabledForWorkspace(ctx context.Context, workspaceID string) (bool, error) {
	if r == nil || r.db == nil {
		return true, nil
	}
	parsed, err := util.ParseUUID(workspaceID)
	if err != nil {
		return true, nil
	}
	return workflowHooksFlagForWorkspace(ctx, r.db, parsed)
}

var _ WorkspaceFlagResolver = (*PostgresWorkspaceFlagResolver)(nil)

// workflowHooksFlagForWorkspace resolves the workspace-level value of the
// cerebro_workflow_hooks flag. No row means the workspace never overrode the
// registry default, which is ON — the gates are the only decision path, so
// they stay on until a workspace turns them off.
func workflowHooksFlagForWorkspace(ctx context.Context, db cerebrodb.DBTX, workspaceID pgtype.UUID) (bool, error) {
	if !workspaceID.Valid {
		return true, nil
	}
	var enabled bool
	err := db.QueryRow(ctx, `
SELECT enabled FROM cerebro_feature_flags
WHERE workspace_id = $1
  AND user_id = '00000000-0000-0000-0000-000000000000'
  AND flag_key = 'cerebro_workflow_hooks'`, workspaceID).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return enabled, nil
}

// workflowHooksFlagForTask resolves the same flag from a task, whose workspace
// comes from its issue and falls back to its agent. A missing task keeps the
// old "unknown task, no gate" answer.
func workflowHooksFlagForTask(ctx context.Context, db cerebrodb.DBTX, taskID pgtype.UUID) (bool, error) {
	var enabled bool
	err := db.QueryRow(ctx, `
SELECT COALESCE((
    SELECT f.enabled FROM cerebro_feature_flags f
    WHERE f.workspace_id = COALESCE(i.workspace_id, a.workspace_id)
      AND f.user_id = '00000000-0000-0000-0000-000000000000'
      AND f.flag_key = 'cerebro_workflow_hooks'), TRUE)
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
LEFT JOIN issue i ON i.id = t.issue_id
WHERE t.id = $1`, taskID).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return enabled, nil
}
