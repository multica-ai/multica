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
-- orchestrator_agent_id uses a paired (orchestrator_agent_id, orchestrator_agent_id_set)
-- pattern so callers can distinguish "don't change" from "explicitly clear to NULL".
-- The narg-and-bool pattern is the same one used for project lead_type/lead_id
-- and for workspace.repos before it.
UPDATE workspace SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    context = COALESCE(sqlc.narg('context'), context),
    settings = COALESCE(sqlc.narg('settings'), settings),
    repos = COALESCE(sqlc.narg('repos'), repos),
    issue_prefix = COALESCE(sqlc.narg('issue_prefix'), issue_prefix),
    orchestrator_agent_id = CASE
        WHEN sqlc.arg('orchestrator_agent_id_set')::boolean
        THEN sqlc.narg('orchestrator_agent_id')::uuid
        ELSE orchestrator_agent_id
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
