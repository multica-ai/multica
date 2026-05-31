-- ============ Channels ============

-- name: ListChannels :many
-- Lists channels visible to a user: open channels + channels the user is a member of.
-- Includes the user's membership row (if any) and an unread flag computed from
-- the latest thread activity vs the member's last_read_at.
SELECT
    c.*,
    cm.role          AS member_role,
    cm.last_read_at  AS member_last_read_at,
    (cm.user_id IS NOT NULL)::boolean AS is_member,
    COALESCE((
        SELECT max(t.last_message_at) FROM channel_thread t WHERE t.channel_id = c.id
    ), c.created_at)::timestamptz AS last_activity_at,
    (
        cm.user_id IS NOT NULL
        AND EXISTS (
            SELECT 1 FROM channel_thread t
            WHERE t.channel_id = c.id AND t.last_message_at > cm.last_read_at
        )
    )::boolean AS has_unread
FROM channel c
LEFT JOIN channel_member cm ON cm.channel_id = c.id AND cm.user_id = $2
WHERE c.workspace_id = $1
  AND c.is_archived = false
  AND (c.access_mode = 'open' OR cm.user_id IS NOT NULL)
ORDER BY last_activity_at DESC, c.created_at DESC;

-- name: GetChannel :one
SELECT * FROM channel WHERE id = $1 AND workspace_id = $2;

-- name: CreateChannel :one
INSERT INTO channel (workspace_id, name, slug, description, access_mode, created_by)
VALUES ($1, $2, $3, COALESCE(sqlc.narg('description'), ''), COALESCE(sqlc.narg('access_mode'), 'open'), sqlc.narg('created_by'))
RETURNING *;

-- name: UpdateChannel :one
UPDATE channel SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    access_mode = COALESCE(sqlc.narg('access_mode'), access_mode),
    is_locked = COALESCE(sqlc.narg('is_locked'), is_locked),
    is_archived = COALESCE(sqlc.narg('is_archived'), is_archived),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteChannel :exec
DELETE FROM channel WHERE id = $1 AND workspace_id = $2;

-- name: TouchChannel :exec
UPDATE channel SET updated_at = now() WHERE id = $1;

-- ============ Channel members ============

-- name: ListChannelMembers :many
SELECT cm.*, u.name AS user_name, u.email AS user_email, u.avatar_url AS user_avatar_url
FROM channel_member cm
JOIN "user" u ON u.id = cm.user_id
WHERE cm.channel_id = $1
ORDER BY cm.joined_at ASC;

-- name: GetChannelMember :one
SELECT * FROM channel_member WHERE channel_id = $1 AND user_id = $2;

-- name: UpsertChannelMember :one
INSERT INTO channel_member (channel_id, user_id, role)
VALUES ($1, $2, COALESCE(sqlc.narg('role'), 'member'))
ON CONFLICT (channel_id, user_id)
DO UPDATE SET role = COALESCE(sqlc.narg('role'), channel_member.role)
RETURNING *;

-- name: RemoveChannelMember :exec
DELETE FROM channel_member WHERE channel_id = $1 AND user_id = $2;

-- name: MarkChannelRead :exec
UPDATE channel_member SET last_read_at = now() WHERE channel_id = $1 AND user_id = $2;

-- ============ Threads ============

-- name: ListChannelThreads :many
SELECT t.*, u.name AS creator_name, u.avatar_url AS creator_avatar_url,
    COALESCE((
        SELECT count(*) FROM issue i WHERE i.source_thread_id = t.id
    )::bigint, 0)::bigint AS issue_count
FROM channel_thread t
LEFT JOIN "user" u ON u.id = t.created_by
WHERE t.channel_id = $1
ORDER BY t.last_message_at DESC, t.created_at DESC;

-- name: GetChannelThread :one
SELECT * FROM channel_thread WHERE id = $1;

-- name: CreateChannelThread :one
INSERT INTO channel_thread (channel_id, workspace_id, title, created_by)
VALUES ($1, $2, COALESCE(sqlc.narg('title'), ''), sqlc.narg('created_by'))
RETURNING *;

-- name: UpdateChannelThreadTitle :one
UPDATE channel_thread SET title = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: BumpChannelThread :exec
UPDATE channel_thread SET
    message_count = message_count + 1,
    last_message_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: DeleteChannelThread :exec
DELETE FROM channel_thread WHERE id = $1;

-- ============ Messages ============

-- name: ListThreadMessages :many
SELECT * FROM channel_message
WHERE thread_id = $1
ORDER BY created_at ASC;

-- name: CreateChannelMessage :one
INSERT INTO channel_message (thread_id, channel_id, workspace_id, author_type, author_id, content)
VALUES ($1, $2, $3, $4, sqlc.narg('author_id'), $5)
RETURNING *;

-- name: DeleteChannelMessage :exec
DELETE FROM channel_message WHERE id = $1;

-- ============ Issue <-> thread linkage ============

-- name: LinkIssueSource :exec
UPDATE issue SET source_channel_id = $2, source_thread_id = $3
WHERE id = $1;

-- name: ListThreadIssues :many
SELECT id, number, title, status, priority, source_thread_id
FROM issue
WHERE source_thread_id = $1
ORDER BY created_at ASC;
