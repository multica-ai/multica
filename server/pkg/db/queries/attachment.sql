-- name: CreateAttachment :one
INSERT INTO attachment (id, workspace_id, issue_id, comment_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
VALUES ($1, $2, sqlc.narg(issue_id), sqlc.narg(comment_id), $3, $4, $5, $6, $7, $8)
RETURNING *;

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

-- name: ListAttachmentsByIDs :many
-- Phase 5b — fetch a batch of attachment rows by id, scoped to a
-- workspace. Used when hydrating channel_message attachment lists from
-- metadata.attachments JSONB arrays. Returning unordered is fine — the
-- caller preserves order from the JSONB array.
SELECT * FROM attachment
WHERE id = ANY($1::uuid[]) AND workspace_id = $2;

-- name: DeleteAttachment :exec
DELETE FROM attachment WHERE id = $1 AND workspace_id = $2;
