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
