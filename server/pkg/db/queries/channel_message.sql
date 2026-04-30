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

-- name: CountThreadRepliesByMessageIDs :many
-- Phase 4 — returns (parent_id, reply_count) for the given parent ids,
-- with replies that aren't soft-deleted. Used by the timeline query to
-- light up the "view thread (N)" badge under each parent without an
-- N+1 fetch.
SELECT parent_message_id::uuid AS parent_id,
       count(*)::int4 AS reply_count
FROM channel_message
WHERE parent_message_id = ANY($1::uuid[])
  AND deleted_at IS NULL
GROUP BY parent_message_id;

-- name: AddChannelMessageReaction :one
-- Phase 4 — emoji reaction on a channel message. ON CONFLICT DO UPDATE
-- keeps the reaction row's created_at stable when the same actor adds
-- the same emoji again, so the handler can return 201 + the existing
-- row idempotently.
INSERT INTO channel_message_reaction (channel_message_id, workspace_id, actor_type, actor_id, emoji)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (channel_message_id, actor_type, actor_id, emoji)
DO UPDATE SET created_at = channel_message_reaction.created_at
RETURNING *;

-- name: RemoveChannelMessageReaction :exec
DELETE FROM channel_message_reaction
WHERE channel_message_id = $1
  AND actor_type = $2
  AND actor_id = $3
  AND emoji = $4;

-- name: ListChannelMessageReactionsByMessageIDs :many
-- Batched fetch for the timeline view — pass every parent message id
-- in one call rather than firing one query per row.
SELECT * FROM channel_message_reaction
WHERE channel_message_id = ANY($1::uuid[])
ORDER BY created_at ASC;

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
