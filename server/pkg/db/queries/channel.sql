-- name: CreateChannel :one
INSERT INTO channel (
    workspace_id, name, display_name, description, kind, visibility,
    created_by_type, created_by_id, retention_days, metadata
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, sqlc.narg('retention_days'), COALESCE(sqlc.narg('metadata'), '{}'::jsonb)
)
RETURNING *;

-- name: GetChannel :one
SELECT * FROM channel
WHERE id = $1;

-- name: GetChannelInWorkspace :one
SELECT * FROM channel
WHERE id = $1 AND workspace_id = $2;

-- name: GetChannelByName :one
-- Used for DM idempotency lookups. (workspace_id, kind, name) is unique.
SELECT * FROM channel
WHERE workspace_id = $1 AND kind = $2 AND name = $3;

-- name: ListChannelsForActor :many
-- Returns active channels visible to an actor:
--   - all public channels in the workspace
--   - private channels where the actor is a member
--   - all DMs where the actor is a member
-- Visibility is enforced in SQL so we never leak private channels through
-- a list endpoint.
SELECT c.* FROM channel c
WHERE c.workspace_id = $1
  AND c.archived_at IS NULL
  AND (
    (c.kind = 'channel' AND c.visibility = 'public')
    OR EXISTS (
        SELECT 1 FROM channel_membership cm
        WHERE cm.channel_id = c.id
          AND cm.member_type = $2
          AND cm.member_id = $3
    )
  )
ORDER BY c.updated_at DESC;

-- name: UpdateChannel :one
UPDATE channel
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    description = COALESCE(sqlc.narg('description'), description),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    retention_days = CASE
        WHEN sqlc.arg('retention_days_set')::bool THEN sqlc.narg('retention_days')
        ELSE retention_days
    END,
    metadata = COALESCE(sqlc.narg('metadata'), metadata),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ArchiveChannel :exec
UPDATE channel SET archived_at = now(), updated_at = now()
WHERE id = $1;

-- name: AddChannelMember :one
-- ON CONFLICT DO NOTHING returns no row when the member already exists; the
-- caller treats "already a member" as a success.
INSERT INTO channel_membership (
    channel_id, member_type, member_id, role,
    added_by_type, added_by_id, notification_level
) VALUES (
    $1, $2, $3, COALESCE(sqlc.narg('role'), 'member'),
    sqlc.narg('added_by_type'), sqlc.narg('added_by_id'),
    COALESCE(sqlc.narg('notification_level'), 'all')
)
ON CONFLICT (channel_id, member_type, member_id) DO NOTHING
RETURNING *;

-- name: RemoveChannelMember :exec
DELETE FROM channel_membership
WHERE channel_id = $1 AND member_type = $2 AND member_id = $3;

-- name: GetChannelMembership :one
SELECT * FROM channel_membership
WHERE channel_id = $1 AND member_type = $2 AND member_id = $3;

-- name: ListChannelMembers :many
SELECT * FROM channel_membership
WHERE channel_id = $1
ORDER BY joined_at ASC;

-- name: ListChannelUnreadCountsForActor :many
-- Per-channel unread counts for an actor's memberships. Counts only
-- top-level messages (parent_message_id IS NULL) — thread replies have
-- their own unread surface, not the channel-level badge — and excludes
-- the actor's own messages so posting doesn't bump the unread badge for
-- yourself. last_read_at is the cutoff; NULL means "never read" and
-- everything counts. Returns one row per channel the actor belongs to.
SELECT
    cm.channel_id,
    cm.last_read_at,
    cm.last_read_message_id,
    COUNT(msg.id)::bigint AS unread_count
FROM channel_membership cm
LEFT JOIN channel_message msg ON
    msg.channel_id = cm.channel_id
    AND msg.parent_message_id IS NULL
    AND msg.deleted_at IS NULL
    AND NOT (msg.author_type = cm.member_type AND msg.author_id = cm.member_id)
    AND (cm.last_read_at IS NULL OR msg.created_at > cm.last_read_at)
WHERE cm.member_type = $1 AND cm.member_id = $2
GROUP BY cm.channel_id, cm.last_read_at, cm.last_read_message_id;

-- name: ListChannelMembershipsForActor :many
-- Used to determine which channels an actor belongs to without scanning the
-- full channel table. Membership is the source of truth for private/DM access.
SELECT * FROM channel_membership
WHERE member_type = $1 AND member_id = $2;

-- name: MarkChannelRead :exec
-- Cursor-based read state. last_read_message_id is the message most recently
-- seen by the member; updates also bump last_read_at.
UPDATE channel_membership
SET last_read_message_id = $4, last_read_at = now()
WHERE channel_id = $1 AND member_type = $2 AND member_id = $3;

-- name: UpdateMembershipNotificationLevel :exec
UPDATE channel_membership
SET notification_level = $4
WHERE channel_id = $1 AND member_type = $2 AND member_id = $3;

-- name: CreateChannelMentionTask :one
-- Phase 3 — task created when an agent is @-mentioned in a channel message.
-- Has neither issue_id nor chat_session_id; the daemon detects this variant
-- via context.type == "channel_mention" and fetches recent messages from the
-- channels API at execution time. Mirrors the QuickCreate-task pattern.
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, context
) VALUES ($1, $2, NULL, 'queued', $3, $4)
RETURNING *;

-- name: HasPendingChannelMentionForAgent :one
-- Phase 3 dedup guard — returns TRUE if the given agent already has an
-- in-flight task for the same channel within the configured window
-- (default 30s). Used so a rapid burst of @mentions doesn't enqueue a
-- task per message.
--
-- The JSONB filter on context->>'channel_id' is unindexed but
-- agent_task_queue stays small for active rows (terminal statuses are
-- archived elsewhere), so a sequential scan over `status IN (queued,
-- dispatched, running)` is cheap enough at the volumes we expect.
SELECT EXISTS (
    SELECT 1 FROM agent_task_queue
    WHERE agent_id = $1
      AND status IN ('queued', 'dispatched', 'running')
      AND context->>'channel_id' = sqlc.arg('channel_id')::text
      AND created_at > now() - make_interval(secs => sqlc.arg('window_seconds')::float8)
);

-- name: ListChannelsWithRetention :many
-- Phase 2 retention sweep — returns every non-archived channel whose
-- effective retention is finite (i.e. NOT "retain forever"). Effective
-- retention is COALESCE(channel.retention_days, workspace.channel_retention_days);
-- a NULL at both levels means "retain forever" and the row is excluded.
--
-- The Go caller iterates and applies SoftDeleteOldChannelMessages per row;
-- doing the date math here would force interval-arithmetic SQL inside the
-- batched delete and complicate the test fixtures.
SELECT
    c.id            AS channel_id,
    c.workspace_id,
    COALESCE(c.retention_days, w.channel_retention_days)::int4 AS effective_days
FROM channel c
JOIN workspace w ON w.id = c.workspace_id
WHERE c.archived_at IS NULL
  AND COALESCE(c.retention_days, w.channel_retention_days) IS NOT NULL
  AND COALESCE(c.retention_days, w.channel_retention_days) > 0
ORDER BY c.workspace_id, c.id;
