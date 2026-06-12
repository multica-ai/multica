-- ============ Channels ============

-- name: ListChannels :many
-- Lists channels visible to a user: open channels + channels the user is a member of.
-- Includes the user's membership row (if any) and an unread flag computed from
-- the latest message activity vs the member's last_read_at.
SELECT
    c.*,
    cm.role          AS member_role,
    cm.last_read_at  AS member_last_read_at,
    (cm.user_id IS NOT NULL)::boolean AS is_member,
    COALESCE(
        GREATEST(
            (SELECT max(m.created_at) FROM channel_message m WHERE m.channel_id = c.id),
            (SELECT max(t.last_message_at) FROM channel_thread t WHERE t.channel_id = c.id)
        ),
        c.created_at
    )::timestamptz AS last_activity_at,
    (
        cm.user_id IS NOT NULL
        AND EXISTS (
            SELECT 1 FROM channel_message m
            WHERE m.channel_id = c.id AND m.created_at > cm.last_read_at
        )
    )::boolean AS has_unread,
    cg.name AS group_name,
    COALESCE(cg.position, 0)::float8 AS group_position
FROM channel c
LEFT JOIN channel_member cm ON cm.channel_id = c.id AND cm.user_id = $2
LEFT JOIN channel_group cg ON cg.id = c.group_id
WHERE c.workspace_id = $1
  AND c.is_archived = false
  AND (c.access_mode = 'open' OR cm.user_id IS NOT NULL)
ORDER BY group_position ASC, c.position ASC, last_activity_at DESC, c.created_at DESC;

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
INSERT INTO channel_thread (channel_id, workspace_id, title, created_by, root_message_id)
VALUES ($1, $2, COALESCE(sqlc.narg('title'), ''), sqlc.narg('created_by'), sqlc.narg('root_message_id'))
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

-- ============ Messages (V1 — thread-scoped, kept for backward compat) ============

-- name: ListThreadMessages :many
SELECT * FROM channel_message
WHERE thread_id = $1
ORDER BY created_at ASC;

-- name: CreateChannelMessage :one
INSERT INTO channel_message (thread_id, channel_id, workspace_id, author_type, author_id, content)
VALUES ($1, $2, $3, $4, sqlc.narg('author_id'), $5)
RETURNING *;

-- name: UpdateChannelMessage :one
UPDATE channel_message SET content = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteChannelMessage :exec
DELETE FROM channel_message WHERE id = $1;

-- name: GetChannelMessage :one
SELECT * FROM channel_message WHERE id = $1;

-- ============ Messages (V2 — top-level flat messages) ============

-- name: ListChannelMessages :many
-- Lists top-level channel messages (thread_id IS NULL), with reply_count.
SELECT m.*,
    COALESCE((SELECT count(*) FROM channel_message r WHERE r.reply_to_id = m.id)::int, 0)::int AS reply_count
FROM channel_message m
WHERE m.channel_id = $1 AND m.thread_id IS NULL
ORDER BY m.created_at ASC;

-- name: ListChannelMessagesLatest :many
-- Lists the N most recent top-level messages (for initial page load).
-- Returns in DESC order; caller reverses to ASC for display.
SELECT m.*,
    COALESCE((SELECT count(*) FROM channel_message r WHERE r.reply_to_id = m.id)::int, 0)::int AS reply_count
FROM channel_message m
WHERE m.channel_id = $1 AND m.thread_id IS NULL
ORDER BY m.created_at DESC
LIMIT $2;

-- name: ListChannelMessagesPaginated :many
-- Lists top-level channel messages with cursor-based pagination (before a timestamp).
SELECT m.*,
    COALESCE((SELECT count(*) FROM channel_message r WHERE r.reply_to_id = m.id)::int, 0)::int AS reply_count
FROM channel_message m
WHERE m.channel_id = $1 AND m.thread_id IS NULL AND m.created_at < $2
ORDER BY m.created_at DESC
LIMIT $3;

-- name: CreateChannelMessageTopLevel :one
-- Creates a top-level message in a channel (no thread).
INSERT INTO channel_message (channel_id, workspace_id, author_type, author_id, content)
VALUES ($1, $2, $3, sqlc.narg('author_id'), $4)
RETURNING *;

-- name: CreateChannelMessageReply :one
-- Creates a reply message linked to a parent message, within its thread.
INSERT INTO channel_message (thread_id, channel_id, workspace_id, author_type, author_id, content, reply_to_id)
VALUES ($1, $2, $3, $4, sqlc.narg('author_id'), $5, $6)
RETURNING *;

-- name: ListMessageReplies :many
-- Lists all replies (thread messages) for a given root message.
SELECT m.* FROM channel_message m
JOIN channel_thread t ON t.id = m.thread_id
WHERE t.root_message_id = $1
ORDER BY m.created_at ASC;

-- name: GetThreadByRootMessage :one
-- Finds the thread created from a specific root message.
SELECT * FROM channel_thread WHERE root_message_id = $1;

-- name: CreateChannelThreadFromMessage :one
-- Creates a thread anchored to a root message.
INSERT INTO channel_thread (channel_id, workspace_id, title, created_by, root_message_id)
VALUES ($1, $2, $3, sqlc.narg('created_by'), $4)
RETURNING *;

-- name: CountMessageReplies :one
-- Counts replies to a specific message.
SELECT count(*)::int AS reply_count
FROM channel_message
WHERE reply_to_id = $1;

-- name: HasAgentChannelMessageSince :one
-- Used by channel-origin task completion fallback. If the agent already wrote
-- a visible channel message during the task, do not synthesize another one from
-- final stdout.
SELECT count(*) > 0 AS has_message
FROM channel_message
WHERE channel_id = $1
  AND author_type = 'agent'
  AND author_id = $2
  AND created_at >= $3;

-- ============ Channel context (for agents) ============

-- name: GetChannelContext :many
-- Returns the N most recent top-level messages in a channel for agent context injection.
SELECT m.*,
    u.name AS author_name,
    u.avatar_url AS author_avatar_url
FROM channel_message m
LEFT JOIN "user" u ON u.id = m.author_id
WHERE m.channel_id = $1 AND m.thread_id IS NULL
ORDER BY m.created_at DESC
LIMIT $2;

-- name: GetChannelMessageForContext :one
-- Returns a specific message in a channel with display metadata for agent context.
SELECT m.*,
    u.name AS author_name,
    u.avatar_url AS author_avatar_url
FROM channel_message m
LEFT JOIN "user" u ON u.id = m.author_id
WHERE m.channel_id = $1 AND m.id = $2;

-- name: ListChannelMessageRepliesForContext :many
-- Returns replies for a trigger message. For a top-level message it returns
-- the direct replies; for a reply it returns the sibling reply window under
-- the same root so the agent can see the local conversation.
WITH trigger AS (
    SELECT m.id, m.reply_to_id
    FROM channel_message m
    WHERE m.channel_id = $1 AND m.id = $2
),
root AS (
    SELECT COALESCE(reply_to_id, id) AS root_message_id
    FROM trigger
)
SELECT m.*,
    u.name AS author_name,
    u.avatar_url AS author_avatar_url
FROM channel_message m
JOIN root ON m.reply_to_id = root.root_message_id
LEFT JOIN "user" u ON u.id = m.author_id
WHERE m.channel_id = $1
ORDER BY m.created_at ASC;

-- ============ Issue <-> thread linkage ============

-- name: LinkIssueSource :exec
UPDATE issue SET source_channel_id = $2, source_thread_id = $3
WHERE id = $1;

-- name: ListThreadIssues :many
SELECT id, number, title, status, priority, source_thread_id
FROM issue
WHERE source_thread_id = $1
ORDER BY created_at ASC;

-- ============ Channel Groups ============

-- name: ListChannelGroups :many
SELECT * FROM channel_group
WHERE workspace_id = $1
ORDER BY position ASC;

-- name: CreateChannelGroup :one
INSERT INTO channel_group (workspace_id, name, position, created_by)
VALUES ($1, $2, $3, sqlc.narg('created_by'))
RETURNING *;

-- name: UpdateChannelGroupName :one
UPDATE channel_group SET name = $2 WHERE id = $1
RETURNING *;

-- name: UpdateChannelGroupPosition :exec
UPDATE channel_group SET position = $2 WHERE id = $1;

-- name: DeleteChannelGroup :exec
DELETE FROM channel_group WHERE id = $1 AND workspace_id = $2;

-- name: MoveChannelToGroup :exec
UPDATE channel SET group_id = sqlc.narg('group_id'), position = $2 WHERE id = $1;

-- name: GetMaxChannelGroupPosition :one
SELECT COALESCE(MAX(position), 0)::float8 AS max_position
FROM channel_group WHERE workspace_id = $1;

-- name: GetMaxChannelPositionInGroup :one
SELECT COALESCE(MAX(position), 0)::float8 AS max_position
FROM channel WHERE workspace_id = $1 AND group_id = sqlc.narg('group_id');
