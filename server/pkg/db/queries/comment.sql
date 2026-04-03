-- name: ListComments :many
SELECT * FROM comment
WHERE issue_id = ? AND workspace_id = ?
ORDER BY created_at ASC;

-- name: ListCommentsPaginated :many
SELECT * FROM comment
WHERE issue_id = ? AND workspace_id = ?
ORDER BY created_at ASC
LIMIT ? OFFSET ?;

-- name: ListCommentsSince :many
SELECT * FROM comment
WHERE issue_id = ? AND workspace_id = ? AND created_at > ?
ORDER BY created_at ASC;

-- name: ListCommentsSincePaginated :many
SELECT * FROM comment
WHERE issue_id = ? AND workspace_id = ? AND created_at > ?
ORDER BY created_at ASC
LIMIT ? OFFSET ?;

-- name: CountComments :one
SELECT count(*) FROM comment
WHERE issue_id = ? AND workspace_id = ?;

-- name: GetComment :one
SELECT * FROM comment
WHERE id = ?;

-- name: GetCommentInWorkspace :one
SELECT * FROM comment
WHERE id = ? AND workspace_id = ?;

-- name: CreateComment :one
INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type, parent_id)
VALUES (?, ?, ?, ?, ?, ?, ?, sqlc.narg(parent_id))
RETURNING *;

-- name: UpdateComment :one
UPDATE comment SET
    content = ?,
    updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: DeleteComment :exec
DELETE FROM comment WHERE id = ?;
