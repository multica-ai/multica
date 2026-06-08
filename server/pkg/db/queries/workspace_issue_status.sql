-- name: ListWorkspaceIssueStatuses :many
SELECT id, workspace_id, name, label, color, category, position, is_default, created_at, updated_at
FROM workspace_issue_status
WHERE workspace_id = $1
ORDER BY position ASC;

-- name: GetWorkspaceIssueStatus :one
SELECT id, workspace_id, name, label, color, category, position, is_default, created_at, updated_at
FROM workspace_issue_status
WHERE workspace_id = $1 AND id = $2;

-- name: GetWorkspaceIssueStatusByName :one
SELECT id, workspace_id, name, label, color, category, position, is_default, created_at, updated_at
FROM workspace_issue_status
WHERE workspace_id = $1 AND name = $2;

-- name: CreateWorkspaceIssueStatus :one
INSERT INTO workspace_issue_status (workspace_id, name, label, color, category, position, is_default)
VALUES ($1, $2, $3, $4, $5, $6, false)
RETURNING id, workspace_id, name, label, color, category, position, is_default, created_at, updated_at;

-- name: UpdateWorkspaceIssueStatus :one
UPDATE workspace_issue_status
SET label = COALESCE(sqlc.narg('label'), label),
    color = COALESCE(sqlc.narg('color'), color),
    category = COALESCE(sqlc.narg('category'), category),
    position = COALESCE(sqlc.narg('position'), position),
    updated_at = now()
WHERE workspace_id = $1 AND id = $2
  AND is_default = false  -- cannot modify built-in statuses' core fields
RETURNING id, workspace_id, name, label, color, category, position, is_default, created_at, updated_at;

-- name: UpdateBuiltinStatusDisplay :one
-- Allow changing label and color even for built-in statuses (but not name/category).
UPDATE workspace_issue_status
SET label = COALESCE(sqlc.narg('label'), label),
    color = COALESCE(sqlc.narg('color'), color),
    position = COALESCE(sqlc.narg('position'), position),
    updated_at = now()
WHERE workspace_id = $1 AND id = $2
RETURNING id, workspace_id, name, label, color, category, position, is_default, created_at, updated_at;

-- name: DeleteWorkspaceIssueStatus :exec
DELETE FROM workspace_issue_status
WHERE workspace_id = $1 AND id = $2 AND is_default = false;

-- name: ValidateIssueStatus :one
-- Check if a status name is valid for a workspace.
SELECT EXISTS(
    SELECT 1 FROM workspace_issue_status
    WHERE workspace_id = $1 AND name = $2
) AS valid;

-- name: GetStatusCategory :one
-- Get the category for a status name in a workspace.
SELECT category FROM workspace_issue_status
WHERE workspace_id = $1 AND name = $2;

-- name: ListCompletedStatusNames :many
-- Get all status names that represent completed/cancelled states.
SELECT name FROM workspace_issue_status
WHERE workspace_id = $1 AND category IN ('completed', 'cancelled');

-- name: EnsureDefaultStatuses :exec
-- Seed default statuses for a new workspace (called on workspace creation).
INSERT INTO workspace_issue_status (workspace_id, name, label, color, category, position, is_default)
VALUES
    ($1, 'backlog',     'Backlog',      '#6b7280', 'not_started', 0, true),
    ($1, 'todo',        'Todo',         '#6b7280', 'not_started', 1, true),
    ($1, 'in_progress', 'In Progress',  '#f59e0b', 'started',     2, true),
    ($1, 'in_review',   'In Review',    '#8b5cf6', 'started',     3, true),
    ($1, 'done',        'Done',         '#22c55e', 'completed',   4, true),
    ($1, 'blocked',     'Blocked',      '#ef4444', 'started',     5, true),
    ($1, 'cancelled',   'Cancelled',    '#6b7280', 'cancelled',   6, true)
ON CONFLICT (workspace_id, name) DO NOTHING;
