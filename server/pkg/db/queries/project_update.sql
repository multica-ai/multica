-- name: ListProjectUpdates :many
SELECT * FROM project_update
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: GetProjectUpdateInWorkspace :one
SELECT * FROM project_update
WHERE id = $1 AND workspace_id = $2;

-- name: CreateProjectUpdate :one
INSERT INTO project_update (
    project_id, workspace_id, health, body, author_type, author_id
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: UpdateProjectUpdate :one
UPDATE project_update
SET health     = COALESCE(sqlc.narg('health'), health),
    body       = COALESCE(sqlc.narg('body'), body),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProjectUpdate :exec
DELETE FROM project_update WHERE id = $1;
