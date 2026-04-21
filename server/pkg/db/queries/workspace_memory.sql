-- name: ListWorkspaceMemoryIndex :many
SELECT id, name, description, created_at, updated_at
FROM workspace_memory
WHERE workspace_id = $1
  AND (project_id IS NULL OR project_id = sqlc.narg('project_id'))
ORDER BY project_id NULLS LAST, updated_at DESC;

-- name: GetWorkspaceMemory :one
SELECT * FROM workspace_memory
WHERE id = $1 AND workspace_id = $2;

-- name: CreateWorkspaceMemory :one
INSERT INTO workspace_memory (workspace_id, name, description, content, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateWorkspaceMemoryForProject :one
INSERT INTO workspace_memory (workspace_id, name, description, content, created_by, project_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateWorkspaceMemory :one
UPDATE workspace_memory SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    content = COALESCE(sqlc.narg('content'), content),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteWorkspaceMemory :exec
DELETE FROM workspace_memory
WHERE id = $1 AND workspace_id = $2;
