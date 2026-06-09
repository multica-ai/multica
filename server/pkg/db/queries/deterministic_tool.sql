-- Deterministic tool (workspace-authored Go "step") CRUD

-- name: CreateDeterministicTool :one
INSERT INTO deterministic_tool (workspace_id, name, description, source, enabled, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetDeterministicTool :one
SELECT * FROM deterministic_tool
WHERE id = $1;

-- name: ListDeterministicToolsByWorkspace :many
SELECT * FROM deterministic_tool
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: ListEnabledDeterministicToolsByWorkspace :many
-- Used at task-claim time to expose a workspace's enabled tools to the agent.
SELECT * FROM deterministic_tool
WHERE workspace_id = $1 AND enabled = TRUE
ORDER BY name ASC;

-- name: UpdateDeterministicTool :one
UPDATE deterministic_tool
SET name        = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    source      = COALESCE(sqlc.narg('source'), source),
    enabled     = COALESCE(sqlc.narg('enabled'), enabled),
    updated_at  = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteDeterministicTool :execrows
DELETE FROM deterministic_tool
WHERE id = $1;
