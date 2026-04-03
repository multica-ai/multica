-- Channel CRUD

-- name: CreateChannel :one
INSERT INTO channel (workspace_id, name, provider, config, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetChannel :one
SELECT * FROM channel WHERE id = $1;

-- name: GetChannelInWorkspace :one
SELECT * FROM channel WHERE id = $1 AND workspace_id = $2;

-- name: ListChannelsByWorkspace :many
SELECT * FROM channel WHERE workspace_id = $1 ORDER BY created_at;

-- name: UpdateChannel :one
UPDATE channel SET
    name = COALESCE(sqlc.narg('name'), name),
    config = COALESCE(sqlc.narg('config'), config),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteChannel :exec
DELETE FROM channel WHERE id = $1;

-- name: FindChannelByProviderAndExternalID :one
SELECT * FROM channel
WHERE provider = $1 AND config->>'channel_id' = $2;

-- Issue-channel assignment

-- name: AssignChannelToIssue :one
INSERT INTO issue_channel (issue_id, channel_id)
VALUES ($1, $2)
RETURNING *;

-- name: UnassignChannelFromIssue :exec
DELETE FROM issue_channel WHERE issue_id = $1 AND channel_id = $2;

-- name: ListIssueChannels :many
SELECT ic.id, ic.issue_id, ic.channel_id, ic.thread_ref, ic.created_at,
       c.name AS channel_name, c.provider AS channel_provider
FROM issue_channel ic
JOIN channel c ON c.id = ic.channel_id
WHERE ic.issue_id = $1
ORDER BY ic.created_at;

-- name: GetIssueChannel :one
SELECT ic.id, ic.issue_id, ic.channel_id, ic.thread_ref, ic.created_at,
       c.name AS channel_name, c.provider AS channel_provider, c.config AS channel_config
FROM issue_channel ic
JOIN channel c ON c.id = ic.channel_id
WHERE ic.id = $1;

-- name: GetFirstIssueChannel :one
SELECT ic.id, ic.issue_id, ic.channel_id, ic.thread_ref, ic.created_at,
       c.name AS channel_name, c.provider AS channel_provider, c.config AS channel_config
FROM issue_channel ic
JOIN channel c ON c.id = ic.channel_id
WHERE ic.issue_id = $1
ORDER BY ic.created_at
LIMIT 1;

-- name: UpdateIssueChannelThreadRef :exec
UPDATE issue_channel SET thread_ref = $2 WHERE id = $1;

-- name: FindIssueChannelByThreadRef :one
SELECT ic.id, ic.issue_id, ic.channel_id, ic.thread_ref, ic.created_at,
       c.name AS channel_name, c.provider AS channel_provider, c.config AS channel_config
FROM issue_channel ic
JOIN channel c ON c.id = ic.channel_id
WHERE ic.channel_id = $1 AND ic.thread_ref = $2;

-- Channel messages

-- name: CreateChannelMessage :one
INSERT INTO channel_message (issue_channel_id, direction, content, external_id, sender_type, sender_ref)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListChannelMessages :many
SELECT * FROM channel_message
WHERE issue_channel_id = $1
ORDER BY created_at;

-- name: ListChannelMessagesByIssue :many
SELECT cm.* FROM channel_message cm
JOIN issue_channel ic ON ic.id = cm.issue_channel_id
WHERE ic.issue_id = $1
ORDER BY cm.created_at;

-- name: GetChannelMessage :one
SELECT * FROM channel_message WHERE id = $1;

-- name: GetLatestInboundAfter :one
SELECT * FROM channel_message
WHERE issue_channel_id = $1
  AND direction = 'inbound'
  AND created_at > $2
ORDER BY created_at ASC
LIMIT 1;

-- name: ChannelMessageExistsByExternalID :one
SELECT EXISTS(SELECT 1 FROM channel_message WHERE external_id = $1) AS exists;
