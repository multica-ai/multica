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
UPDATE workspace SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    context = COALESCE(sqlc.narg('context'), context),
    settings = COALESCE(sqlc.narg('settings'), settings),
    repos = COALESCE(sqlc.narg('repos'), repos),
    issue_prefix = COALESCE(sqlc.narg('issue_prefix'), issue_prefix),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: IncrementIssueCounter :one
UPDATE workspace SET issue_counter = issue_counter + 1
WHERE id = $1
RETURNING issue_counter;

-- MergeWorkspaceSetting upserts a single key into workspace.settings.
-- jsonb || jsonb is a server-side merge so concurrent PATCH requests for
-- different keys do not clobber each other (unlike a client-side
-- read / modify / write through UpdateWorkspace.settings).
-- name: MergeWorkspaceSetting :one
UPDATE workspace
SET settings = settings || jsonb_build_object(sqlc.arg('key')::text, sqlc.arg('value')::jsonb),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- DeleteWorkspaceSetting removes a single key from workspace.settings,
-- restoring the unset behaviour without touching other keys.
-- name: DeleteWorkspaceSetting :one
UPDATE workspace
SET settings = settings - sqlc.arg('key')::text,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkspace :exec
DELETE FROM workspace WHERE id = $1;
