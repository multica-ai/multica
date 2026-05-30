-- name: ListDocs :many
SELECT * FROM doc
WHERE workspace_id = $1
  AND is_archived = FALSE
ORDER BY position ASC, created_at ASC;

-- name: GetDoc :one
SELECT * FROM doc
WHERE id = $1 AND workspace_id = $2 AND is_archived = FALSE;

-- name: CreateDoc :one
INSERT INTO doc (workspace_id, creator_id, title, content, parent_id, position)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateDoc :one
UPDATE doc SET
    title      = COALESCE(sqlc.narg('title'), title),
    content    = COALESCE(sqlc.narg('content'), content),
    parent_id  = sqlc.narg('parent_id'),
    position   = COALESCE(sqlc.narg('position'), position),
    updated_at = NOW()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ArchiveDoc :one
UPDATE doc SET
    is_archived = TRUE,
    updated_at  = NOW()
WHERE id = $1 AND workspace_id = $2
RETURNING *;
