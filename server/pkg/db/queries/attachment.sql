-- name: CreateAttachment :one
INSERT INTO attachment (id, workspace_id, issue_id, comment_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
VALUES ($1, $2, sqlc.narg(issue_id), sqlc.narg(comment_id), $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdateAttachmentSize :one
-- After a successful pre-signed S3 PUT, the client calls /confirm and
-- the handler sets the byte count to the value HeadObject reported.
-- The CreateAttachment that preceded the presign left size_bytes=0 as a
-- sentinel for "upload in progress"; this query flips it to the real
-- size and is idempotent — calling /confirm twice with the same byte
-- count is a no-op.
UPDATE attachment SET size_bytes = $2 WHERE id = $1 RETURNING *;

-- name: ListAttachmentsByIssue :many
SELECT * FROM attachment
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListAttachmentsByComment :many
SELECT * FROM attachment
WHERE comment_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: GetAttachment :one
SELECT * FROM attachment
WHERE id = $1 AND workspace_id = $2;

-- name: ListAttachmentsByCommentIDs :many
SELECT * FROM attachment
WHERE comment_id = ANY($1::uuid[]) AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListAttachmentURLsByIssueOrComments :many
SELECT a.url FROM attachment a
WHERE a.issue_id = $1
   OR a.comment_id IN (SELECT c.id FROM comment c WHERE c.issue_id = $1);

-- name: ListAttachmentURLsByCommentID :many
SELECT url FROM attachment
WHERE comment_id = $1;

-- name: LinkAttachmentsToComment :exec
UPDATE attachment
SET comment_id = $1
WHERE issue_id = $2
  AND comment_id IS NULL
  AND id = ANY($3::uuid[]);

-- name: LinkAttachmentsToIssue :exec
UPDATE attachment
SET issue_id = $1
WHERE workspace_id = $2
  AND issue_id IS NULL
  AND id = ANY($3::uuid[]);

-- name: DeleteAttachment :exec
DELETE FROM attachment WHERE id = $1 AND workspace_id = $2;
