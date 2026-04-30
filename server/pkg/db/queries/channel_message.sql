-- name: CreateChannelMessage :one
INSERT INTO channel_message (
    channel_id, author_type, author_id, content, parent_message_id, metadata
) VALUES (
    $1, $2, $3, $4, sqlc.narg('parent_message_id'), COALESCE(sqlc.narg('metadata'), '{}'::jsonb)
)
RETURNING *;

-- name: GetChannelMessage :one
SELECT * FROM channel_message
WHERE id = $1;

-- name: ListChannelMessages :many
-- Top-level timeline (excludes thread replies and soft-deleted rows).
-- Cursor-based: pass `before_created_at = ''` (or sqlc.narg null) for the
-- newest page, otherwise the timestamp of the oldest message in the previous
-- page. Page size is capped at 200 by the application layer.
SELECT * FROM channel_message
WHERE channel_id = $1
  AND parent_message_id IS NULL
  AND deleted_at IS NULL
  AND (sqlc.narg('before_created_at')::timestamptz IS NULL OR created_at < sqlc.narg('before_created_at')::timestamptz)
ORDER BY created_at DESC
LIMIT $2;

-- name: ListChannelMessagesIncludingThreads :many
-- Variant used by search and by sidecar consumers that want the full stream.
-- Excludes soft-deleted but includes thread replies.
SELECT * FROM channel_message
WHERE channel_id = $1
  AND deleted_at IS NULL
  AND (sqlc.narg('before_created_at')::timestamptz IS NULL OR created_at < sqlc.narg('before_created_at')::timestamptz)
ORDER BY created_at DESC
LIMIT $2;

-- name: ListThreadReplies :many
-- Phase 4 surface, but the index already exists. Returns oldest-first since
-- threads are read top-down.
SELECT * FROM channel_message
WHERE parent_message_id = $1
  AND deleted_at IS NULL
ORDER BY created_at ASC;

-- name: CountChannelMessages :one
SELECT count(*) FROM channel_message
WHERE channel_id = $1 AND deleted_at IS NULL;

-- name: UpdateChannelMessage :one
-- Phase 5. Only the author can edit; enforcement lives in the handler.
UPDATE channel_message
SET content = $2, edited_at = now()
WHERE id = $1
RETURNING *;

-- name: SoftDeleteChannelMessage :exec
UPDATE channel_message
SET deleted_at = now(), deletion_reason = $2
WHERE id = $1;

-- name: SoftDeleteOldChannelMessages :execrows
-- Retention sweep (Phase 2). Soft-deletes messages older than the cutoff in
-- channels where retention applies, in batches. Returns the number of rows
-- affected so the caller can loop until the workspace is drained.
UPDATE channel_message AS cm
SET deleted_at = now(), deletion_reason = 'retention'
WHERE cm.id IN (
    SELECT inner_cm.id FROM channel_message AS inner_cm
    WHERE inner_cm.channel_id = $1
      AND inner_cm.deleted_at IS NULL
      AND inner_cm.created_at < $2
    ORDER BY inner_cm.created_at ASC
    LIMIT $3
);
