-- name: ListIssueViews :many
SELECT * FROM issue_view
WHERE workspace_id = sqlc.arg('workspace_id')
  AND scope_type = sqlc.arg('scope_type')
  AND scope_id IS NOT DISTINCT FROM sqlc.narg('scope_id')::uuid
  AND (visibility = 'workspace' OR creator_id = sqlc.arg('user_id'))
ORDER BY position ASC, created_at ASC;

-- name: LockWorkspaceForIssueViewCreate :one
-- Serializes the per-workspace 100-view cap and conflicts with workspace
-- deletion so a view cannot be inserted behind the delete sweep.
SELECT id FROM workspace WHERE id = sqlc.arg('workspace_id') FOR UPDATE;

-- name: GetIssueViewForUser :one
SELECT * FROM issue_view
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND (visibility = 'workspace' OR creator_id = sqlc.arg('user_id'));

-- name: CreateIssueView :one
INSERT INTO issue_view (
    workspace_id, creator_id, name, icon, color, scope_type, scope_id,
    visibility, definition, position
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('creator_id'), sqlc.arg('name'),
    sqlc.narg('icon'), sqlc.narg('color'), sqlc.arg('scope_type'),
    sqlc.narg('scope_id'), sqlc.arg('visibility'), sqlc.arg('definition'),
    sqlc.arg('position')
)
RETURNING *;

-- name: UpdateIssueView :one
UPDATE issue_view SET
    name = sqlc.arg('name'),
    icon = sqlc.narg('icon'),
    color = sqlc.narg('color'),
    visibility = sqlc.arg('visibility'),
    definition = sqlc.arg('definition'),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteIssueView :exec
DELETE FROM issue_view
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id');

-- name: CountIssueViewsByWorkspace :one
SELECT count(*) FROM issue_view WHERE workspace_id = sqlc.arg('workspace_id');

-- name: GetMaxIssueViewPosition :one
SELECT COALESCE(MAX(position), 0)::float8 AS max_position
FROM issue_view
WHERE workspace_id = sqlc.arg('workspace_id')
  AND scope_type = sqlc.arg('scope_type')
  AND scope_id IS NOT DISTINCT FROM sqlc.narg('scope_id')::uuid;

-- name: GetDefaultIssueViewID :one
SELECT default_view_id FROM issue_view_preference
WHERE workspace_id = sqlc.arg('workspace_id')
  AND user_id = sqlc.arg('user_id')
  AND scope_type = sqlc.arg('scope_type')
  AND scope_id = sqlc.arg('scope_id');

-- name: SetDefaultIssueView :exec
INSERT INTO issue_view_preference (
    workspace_id, user_id, scope_type, scope_id, default_view_id
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('user_id'), sqlc.arg('scope_type'),
    sqlc.arg('scope_id'), sqlc.arg('default_view_id')
)
ON CONFLICT (workspace_id, user_id, scope_type, scope_id)
DO UPDATE SET default_view_id = EXCLUDED.default_view_id, updated_at = now();

-- name: ClearDefaultIssueView :exec
DELETE FROM issue_view_preference
WHERE workspace_id = sqlc.arg('workspace_id')
  AND user_id = sqlc.arg('user_id')
  AND scope_type = sqlc.arg('scope_type')
  AND scope_id = sqlc.arg('scope_id');

-- name: DeleteIssueViewPreferencesByView :exec
DELETE FROM issue_view_preference
WHERE workspace_id = sqlc.arg('workspace_id')
  AND default_view_id = sqlc.arg('default_view_id');
