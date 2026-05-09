-- name: CreateChannel :one
INSERT INTO channel (workspace_id, name, description, type, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetChannel :one
SELECT * FROM channel
WHERE id = $1;

-- name: GetChannelInWorkspace :one
SELECT * FROM channel
WHERE id = $1 AND workspace_id = $2;

-- name: ListChannelsByWorkspace :many
SELECT c.*,
       EXISTS(SELECT 1 FROM channel_member cm WHERE cm.channel_id = c.id AND cm.member_type = 'user' AND cm.member_id = $2)::bool AS is_member
FROM channel c
WHERE c.workspace_id = $1 AND c.archived_at IS NULL
ORDER BY c.created_at ASC;

-- name: UpdateChannel :one
UPDATE channel SET name = $2, description = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveChannel :exec
UPDATE channel SET archived_at = now(), updated_at = now()
WHERE id = $1;

-- name: AddChannelMember :one
INSERT INTO channel_member (channel_id, member_type, member_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (channel_id, member_type, member_id) DO UPDATE SET role = $4
RETURNING *;

-- name: RemoveChannelMember :exec
DELETE FROM channel_member
WHERE channel_id = $1 AND member_type = $2 AND member_id = $3;

-- name: ListChannelMembers :many
SELECT * FROM channel_member
WHERE channel_id = $1
ORDER BY joined_at ASC;

-- name: IsChannelMember :one
SELECT EXISTS(
    SELECT 1 FROM channel_member
    WHERE channel_id = $1 AND member_type = $2 AND member_id = $3
)::bool AS is_member;

-- name: CreateChannelMessage :one
INSERT INTO channel_message (channel_id, author_type, author_id, content, thread_root_id, task_id)
VALUES ($1, $2, $3, $4, sqlc.narg(thread_root_id), sqlc.narg(task_id))
RETURNING *;

-- name: GetChannelMessage :one
SELECT * FROM channel_message
WHERE id = $1;

-- name: ListChannelMessages :many
SELECT * FROM channel_message
WHERE channel_id = $1 AND (thread_root_id IS NULL OR thread_root_id = sqlc.narg(thread_root_id))
ORDER BY created_at ASC
LIMIT $2 OFFSET $3;

-- name: ListThreadReplies :many
SELECT * FROM channel_message
WHERE thread_root_id = $1
ORDER BY created_at ASC;

-- name: UpdateChannelMessage :one
UPDATE channel_message SET content = $2, edited_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkMessageConverted :exec
UPDATE channel_message SET status = 'converted', updated_at = now()
WHERE id = $1;

-- name: GetMessageThreadRoot :one
SELECT * FROM channel_message
WHERE id = $1;

-- name: MarkChannelRead :exec
INSERT INTO channel_read_state (channel_id, member_type, member_id, last_read_message_id, last_read_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (channel_id, member_type, member_id)
DO UPDATE SET last_read_message_id = $4, last_read_at = now();

-- name: GetChannelReadState :one
SELECT * FROM channel_read_state
WHERE channel_id = $1 AND member_type = $2 AND member_id = $3;

-- name: TouchChannel :exec
UPDATE channel SET updated_at = now()
WHERE id = $1;
