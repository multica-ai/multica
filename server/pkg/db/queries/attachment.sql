-- name: CreateAttachment :one
WITH inserted AS (
  INSERT INTO attachment (
    id, workspace_id, issue_id, comment_id, chat_session_id, task_id,
    uploader_type, uploader_id, filename, url, content_type, size_bytes
  )
  VALUES (
    $1, $2, sqlc.narg(issue_id), sqlc.narg(comment_id), sqlc.narg(chat_session_id), sqlc.narg(task_id),
    $3, $4, $5, $6, $7, $8
  )
  RETURNING *
), bumped_issue AS (
  UPDATE issue
  SET revision = revision + 1
  WHERE id IN (SELECT issue_id FROM inserted WHERE issue_id IS NOT NULL)
  RETURNING revision
), bumped_comment AS (
  UPDATE comment
  SET revision = revision + 1
  WHERE id IN (SELECT comment_id FROM inserted WHERE comment_id IS NOT NULL)
  RETURNING revision
)
SELECT inserted.*,
       COALESCE((SELECT revision FROM bumped_issue), 0)::bigint AS issue_revision,
       COALESCE((SELECT revision FROM bumped_comment), 0)::bigint AS comment_revision
FROM inserted;

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

-- name: GetAttachmentByIDOnly :one
-- Used by the download endpoint, which derives workspace context from the
-- attachment row itself rather than from request headers/query params. The
-- caller still has to verify the requester is a member of the returned
-- workspace_id before serving the bytes — this query is access-neutral on
-- purpose so a self-contained URL like /api/attachments/{id}/download can
-- work as a native <img>/<video> resource load (no header attachment).
SELECT * FROM attachment
WHERE id = $1;

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

-- name: ReplaceCommentAttachments :execrows
UPDATE attachment
SET comment_id = CASE
  WHEN id = ANY(sqlc.arg(attachment_ids)::uuid[]) THEN $1
  ELSE NULL
END
WHERE issue_id = $2
  AND (
    (comment_id = $1 AND NOT id = ANY(sqlc.arg(attachment_ids)::uuid[]))
    OR (comment_id IS NULL AND id = ANY(sqlc.arg(attachment_ids)::uuid[]))
  );

-- name: LinkAttachmentsToChatMessage :many
UPDATE attachment
SET chat_message_id = sqlc.arg(chat_message_id),
    chat_session_id = sqlc.arg(chat_session_id)
WHERE workspace_id = sqlc.arg(workspace_id)
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_message_id IS NULL
  AND (
    chat_session_id IS NULL
    OR chat_session_id = sqlc.arg(chat_session_id)
  )
  AND uploader_type = sqlc.arg(uploader_type)
  AND uploader_id = sqlc.arg(uploader_id)
  AND id = ANY(sqlc.arg(attachment_ids)::uuid[])
RETURNING id;

-- name: DetachAttachmentsFromUserChatMessageByTask :many
-- When an empty chat task is cancelled, its user message is deleted. The
-- attachment FK is ON DELETE CASCADE, so without this the bound rows would be
-- destroyed and a restored draft could never re-bind them. Detach first
-- (chat_message_id -> NULL, keep chat_session_id) so the rows survive as
-- workspace/session-scoped unattached attachments and re-send can re-link them.
UPDATE attachment
SET chat_message_id = NULL
WHERE chat_message_id IN (
  SELECT id FROM chat_message WHERE chat_message.task_id = $1 AND role = 'user'
)
RETURNING *;

-- name: CountUnboundChatAttachmentsForTask :one
-- How many attachments the agent produced for this chat task that are still
-- unbound to any owner. Lets CompleteTask create an assistant message (and
-- bind them) even when the agent's text output was empty but it uploaded files.
SELECT COUNT(*) FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND task_id = sqlc.arg(task_id)
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_message_id IS NULL;

-- name: BindChatAttachmentsToMessage :many
-- Bind a chat agent's task-scoped attachments to the assistant reply it just
-- produced. Only rows still unclaimed by any owner (issue/comment/chat_message)
-- are eligible, so an attachment already linked elsewhere is never stolen.
-- Returns the bound ids for logging.
UPDATE attachment
SET chat_message_id = sqlc.arg(chat_message_id)
WHERE workspace_id = sqlc.arg(workspace_id)
  AND task_id = sqlc.arg(task_id)
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_message_id IS NULL
RETURNING id;

-- name: ListAttachmentsByChatMessage :many
SELECT * FROM attachment
WHERE chat_message_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListAttachmentsByChatMessageIDs :many
SELECT * FROM attachment
WHERE chat_message_id = ANY($1::uuid[]) AND workspace_id = $2
ORDER BY created_at ASC;

-- name: LockAttachmentsForIssueLink :many
-- Issue updates bind attachments and then touch the owner row. Lock eligible
-- attachment rows first so every attachment -> issue mutation uses the same
-- lock order as DeleteAttachment and cannot deadlock with it.
SELECT id FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND issue_id IS NULL
  AND id = ANY(sqlc.arg(attachment_ids)::uuid[])
ORDER BY id
FOR UPDATE;

-- name: LinkAttachmentsToIssue :one
WITH linked AS (
  UPDATE attachment
  SET issue_id = sqlc.arg(issue_id)
  WHERE attachment.workspace_id = sqlc.arg(workspace_id)
    AND attachment.issue_id IS NULL
    AND attachment.id = ANY(sqlc.arg(attachment_ids)::uuid[])
  RETURNING attachment.issue_id
), bumped_issue AS (
  UPDATE issue
  SET revision = revision + 1,
      updated_at = now()
  WHERE id = sqlc.arg(issue_id)
    AND sqlc.arg(bump_revision)::boolean
    AND EXISTS (SELECT 1 FROM linked)
  RETURNING revision
)
SELECT COUNT(*)::bigint AS linked_count,
       COALESCE((SELECT revision FROM bumped_issue), 0)::bigint AS issue_revision
FROM linked;

-- name: DeleteAttachment :one
WITH deleted AS (
  DELETE FROM attachment
  WHERE attachment.id = $1 AND attachment.workspace_id = $2
  RETURNING issue_id, comment_id
), bumped_issue AS (
  UPDATE issue
  SET revision = revision + 1
  WHERE id IN (SELECT issue_id FROM deleted WHERE issue_id IS NOT NULL)
  RETURNING revision
), bumped_comment AS (
  UPDATE comment
  SET revision = revision + 1
  WHERE id IN (SELECT comment_id FROM deleted WHERE comment_id IS NOT NULL)
  RETURNING revision
)
SELECT EXISTS(SELECT 1 FROM deleted) AS changed,
       COALESCE((SELECT revision FROM bumped_issue), 0)::bigint AS issue_revision,
       COALESCE((SELECT revision FROM bumped_comment), 0)::bigint AS comment_revision;

-- name: ListAttachmentsByIDs :many
SELECT * FROM attachment
WHERE id = ANY(sqlc.arg(attachment_ids)::uuid[]) AND workspace_id = sqlc.arg(workspace_id)
ORDER BY created_at ASC;
