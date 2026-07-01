-- name: CreatePlan :one
INSERT INTO plan (workspace_id, creator_id, title, content, status, workflow_id)
VALUES ($1, $2, $3, $4, 'draft', $5)
RETURNING *;

-- name: GetPlan :one
SELECT * FROM plan WHERE id = $1;

-- name: GetPlanByWorkspace :many
SELECT * FROM plan WHERE workspace_id = $1 ORDER BY created_at DESC;

-- name: UpdatePlan :one
UPDATE plan SET
    title = COALESCE(sqlc.narg('title'), title),
    content = COALESCE(sqlc.narg('content'), content),
    status = COALESCE(sqlc.narg('status'), status),
    workflow_id = COALESCE(sqlc.narg('workflow_id'), workflow_id),
    updated_at = now()
WHERE id = $1
RETURNING *;