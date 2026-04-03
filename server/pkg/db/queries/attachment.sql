-- name: CreateAttachment :one
INSERT INTO attachment (id, workspace_id, issue_id, comment_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
VALUES (?, ?, sqlc.narg(issue_id), sqlc.narg(comment_id), ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListAttachmentsByIssue :many
SELECT * FROM attachment
WHERE issue_id = ? AND workspace_id = ?
ORDER BY created_at ASC;

-- name: ListAttachmentsByComment :many
SELECT * FROM attachment
WHERE comment_id = ? AND workspace_id = ?
ORDER BY created_at ASC;

-- name: GetAttachment :one
SELECT * FROM attachment
WHERE id = ? AND workspace_id = ?;

-- name: ListAttachmentsByCommentIDs :many
SELECT * FROM attachment
WHERE comment_id IN (sqlc.slice('comment_ids'))
ORDER BY created_at ASC;

-- name: ListAttachmentURLsByIssueOrComments :many
SELECT a.url FROM attachment a
WHERE a.issue_id = ?
   OR a.comment_id IN (SELECT c.id FROM comment c WHERE c.issue_id = ?);

-- name: ListAttachmentURLsByCommentID :many
SELECT url FROM attachment
WHERE comment_id = ?;

-- name: LinkAttachmentsToComment :exec
UPDATE attachment
SET comment_id = ?
WHERE issue_id = ?
  AND comment_id IS NULL
  AND id IN (sqlc.slice('attachment_ids'));

-- name: DeleteAttachment :exec
DELETE FROM attachment WHERE id = ? AND workspace_id = ?;
