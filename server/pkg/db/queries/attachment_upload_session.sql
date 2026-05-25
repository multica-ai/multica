-- name: CreateAttachmentUploadSession :one
INSERT INTO attachment_upload_session (
  id, workspace_id, attachment_id, object_key, upload_id,
  filename, content_type, size_bytes, part_size_bytes,
  uploader_type, uploader_id,
  issue_id, comment_id, chat_session_id,
  status, expires_at
)
VALUES (
  $1, $2, $3, $4, $5,
  $6, $7, $8, $9,
  $10, $11,
  sqlc.narg(issue_id), sqlc.narg(comment_id), sqlc.narg(chat_session_id),
  'pending', $12
)
RETURNING *;

-- name: GetAttachmentUploadSessionForUpdate :one
SELECT * FROM attachment_upload_session
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: MarkAttachmentUploadSessionCompleted :exec
UPDATE attachment_upload_session
SET status = 'completed', updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'pending';

-- name: MarkAttachmentUploadSessionAborted :exec
UPDATE attachment_upload_session
SET status = 'aborted', updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'pending';

-- name: MarkAttachmentUploadSessionExpired :exec
UPDATE attachment_upload_session
SET status = 'expired', updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'pending';

-- name: ListExpiredAttachmentUploadSessions :many
SELECT * FROM attachment_upload_session
WHERE status = 'pending' AND expires_at < now()
ORDER BY expires_at ASC
LIMIT $1;
