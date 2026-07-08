-- name: CreateChannel :one
INSERT INTO channels (
    workspace_id, name, description, is_private, created_by
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: GetChannel :one
SELECT * FROM channels WHERE id = $1 LIMIT 1;

-- name: ListChannels :many
SELECT * FROM channels
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: UpdateChannel :one
UPDATE channels
SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    is_private = COALESCE(sqlc.narg('is_private'), is_private)
WHERE id = $1
RETURNING *;

-- name: DeleteChannel :exec
DELETE FROM channels WHERE id = $1;

-- name: AddChannelMember :one
INSERT INTO channel_members (
    channel_id, member_id
) VALUES (
    $1, $2
)
RETURNING *;

-- name: RemoveChannelMember :exec
DELETE FROM channel_members WHERE channel_id = $1 AND member_id = $2;

-- name: ListChannelMembers :many
SELECT m.*, cm.joined_at
FROM channel_members cm
JOIN members m ON m.id = cm.member_id
WHERE cm.channel_id = $1;

-- name: CreateChannelMessage :one
INSERT INTO channel_messages (
    channel_id, author_id, content, parent_id
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetChannelMessage :one
SELECT * FROM channel_messages WHERE id = $1 LIMIT 1;

-- name: ListChannelMessages :many
SELECT * FROM channel_messages
WHERE channel_id = $1 AND parent_id IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListChannelMessageReplies :many
SELECT * FROM channel_messages
WHERE parent_id = $1
ORDER BY created_at ASC;

-- name: UpdateChannelMessage :one
UPDATE channel_messages
SET
    content = COALESCE(sqlc.narg('content'), content),
    edited_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteChannelMessage :exec
DELETE FROM channel_messages WHERE id = $1;
