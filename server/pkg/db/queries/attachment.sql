-- name: CreateAttachment :one
INSERT INTO attachment (
  id, workspace_id, issue_id, comment_id, chat_session_id, task_id,
  uploader_type, uploader_id, filename, url, content_type, size_bytes
)
VALUES (
  $1, $2, sqlc.narg(issue_id), sqlc.narg(comment_id), sqlc.narg(chat_session_id), sqlc.narg(task_id),
  $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: ListAttachmentsByIssue :many
SELECT * FROM attachment
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListWorkspaceAttachments :many
-- Files is a read-only projection over attachments that are already bound to
-- a Workspace-visible Issue or to one of the caller's accessible public Chat
-- sessions. In-flight / abandoned uploads remain draft state and must not leak
-- into the shared library.
SELECT a.*,
       COALESCE(a.issue_id, c.issue_id) AS source_issue_id,
       COALESCE(i.title, '') AS source_issue_title,
       COALESCE(cs.title, '') AS source_chat_title
FROM attachment a
LEFT JOIN comment c
  ON c.id = a.comment_id
 AND c.workspace_id = a.workspace_id
LEFT JOIN issue i
  ON i.id = COALESCE(a.issue_id, c.issue_id)
 AND i.workspace_id = a.workspace_id
LEFT JOIN chat_session cs
  ON cs.id = a.chat_session_id
 AND cs.workspace_id = a.workspace_id
WHERE a.workspace_id = sqlc.arg(workspace_id)
  AND (
    COALESCE(a.issue_id, c.issue_id) IS NOT NULL
    OR (
      a.chat_message_id IS NOT NULL
      AND cs.creator_id = sqlc.arg(user_id)
      AND cs.agent_id = ANY(sqlc.arg(allowed_agent_ids)::uuid[])
      AND (
        EXISTS (
          SELECT 1 FROM chat_message public_message
          WHERE public_message.chat_session_id = cs.id
            AND public_message.message_kind != 'channel_command'
        )
        OR (
          NOT EXISTS (
            SELECT 1 FROM channel_chat_session_binding binding
            WHERE binding.chat_session_id = cs.id
          )
          AND NOT EXISTS (
            SELECT 1 FROM chat_message channel_message
            WHERE channel_message.chat_session_id = cs.id
              AND channel_message.channel_ingested
          )
        )
      )
    )
  )
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(page_size)
OFFSET sqlc.arg(page_offset);

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

-- name: ReplaceCommentAttachments :exec
UPDATE attachment
SET comment_id = CASE
  WHEN id = ANY(sqlc.arg(attachment_ids)::uuid[]) THEN $1
  ELSE NULL
END
WHERE issue_id = $2
  AND (
    comment_id = $1
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

-- name: LinkAttachmentsToIssue :exec
UPDATE attachment
SET issue_id = $1
WHERE workspace_id = $2
  AND issue_id IS NULL
  AND id = ANY($3::uuid[]);

-- name: DeleteAttachment :exec
DELETE FROM attachment WHERE id = $1 AND workspace_id = $2;

-- name: ListAttachmentsByIDs :many
SELECT * FROM attachment
WHERE id = ANY(sqlc.arg(attachment_ids)::uuid[]) AND workspace_id = sqlc.arg(workspace_id)
ORDER BY created_at ASC;
