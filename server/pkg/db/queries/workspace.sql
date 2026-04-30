-- name: ListWorkspaces :many
SELECT w.* FROM workspace w
JOIN member m ON m.workspace_id = w.id
WHERE m.user_id = $1
ORDER BY w.created_at ASC;

-- name: GetWorkspace :one
SELECT * FROM workspace
WHERE id = $1;

-- name: GetWorkspaceBySlug :one
SELECT * FROM workspace
WHERE slug = $1;

-- name: CreateWorkspace :one
INSERT INTO workspace (name, slug, description, context, issue_prefix)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateWorkspace :one
-- channels_enabled / channel_retention_days follow the same pattern as the
-- per-channel retention update in channel.sql: an explicit `*_set` flag
-- distinguishes "leave alone" from "set to NULL/false". Without the flag,
-- a missing JSON field would always coerce to false/NULL and a single
-- name-update PATCH would silently disable channels.
UPDATE workspace SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    context = COALESCE(sqlc.narg('context'), context),
    settings = COALESCE(sqlc.narg('settings'), settings),
    repos = COALESCE(sqlc.narg('repos'), repos),
    issue_prefix = COALESCE(sqlc.narg('issue_prefix'), issue_prefix),
    channels_enabled = CASE
        WHEN sqlc.arg('channels_enabled_set')::bool THEN COALESCE(sqlc.narg('channels_enabled'), channels_enabled)
        ELSE channels_enabled
    END,
    channel_retention_days = CASE
        WHEN sqlc.arg('channel_retention_days_set')::bool THEN sqlc.narg('channel_retention_days')
        ELSE channel_retention_days
    END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: IncrementIssueCounter :one
UPDATE workspace SET issue_counter = issue_counter + 1
WHERE id = $1
RETURNING issue_counter;

-- name: DeleteWorkspace :exec
DELETE FROM workspace WHERE id = $1;
