-- name: CreateAttachment :one
INSERT INTO attachment (id, workspace_id, issue_id, comment_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
VALUES ($1, $2, sqlc.narg(issue_id), sqlc.narg(comment_id), $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListAttachmentsByIssue :many
SELECT * FROM attachment
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: CountAttachmentsForIssues :many
-- Bulk variant: returns (issue_id, count) for each issue that has at least one
-- attachment, so the issue list endpoints can fold an attachment indicator into
-- each row without N+1 queries from the client. Workspace-guarded the same way
-- as ListAttachmentsByIssue.
SELECT issue_id, COUNT(*)::bigint AS count
FROM attachment
WHERE issue_id = ANY(sqlc.arg('issue_ids')::uuid[])
  AND workspace_id = sqlc.arg('workspace_id')::uuid
GROUP BY issue_id;

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
