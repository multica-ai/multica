-- name: CreateIssueChannelCallback :one
INSERT INTO issue_channel_callback (
    workspace_id, issue_id, installation_id, channel_type,
    chat_id, chat_type, thread_id, anchor_message_id, registered_by
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('issue_id'), sqlc.arg('installation_id'), sqlc.arg('channel_type'),
    sqlc.arg('chat_id'), sqlc.arg('chat_type'), sqlc.arg('thread_id'), sqlc.arg('anchor_message_id'), sqlc.arg('registered_by')
)
RETURNING *;

-- name: ListActiveIssueChannelCallbacks :many
SELECT * FROM issue_channel_callback
WHERE issue_id = sqlc.arg('issue_id')
  AND channel_type = sqlc.arg('channel_type')
  AND revoked_at IS NULL
ORDER BY created_at, id;

-- name: ListIssueChannelCallbacks :many
SELECT * FROM issue_channel_callback
WHERE issue_id = sqlc.arg('issue_id') AND workspace_id = sqlc.arg('workspace_id')
ORDER BY created_at, id;

-- name: ListIssueChannelCallbackDeliveries :many
SELECT * FROM issue_channel_callback_delivery
WHERE callback_id = ANY(sqlc.arg('callback_ids')::uuid[])
ORDER BY created_at, dedup_key;

-- name: RevokeIssueChannelCallback :one
-- Revoking twice keeps the first revocation time.
UPDATE issue_channel_callback
SET revoked_at = COALESCE(revoked_at, now())
WHERE id = sqlc.arg('id') AND issue_id = sqlc.arg('issue_id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: ClaimIssueChannelCallbackDelivery :one
-- Returns no row when this event was already claimed for the callback, which
-- is how a redelivered event is kept from posting twice.
INSERT INTO issue_channel_callback_delivery (callback_id, dedup_key, event, status)
VALUES (sqlc.arg('callback_id'), sqlc.arg('dedup_key'), sqlc.arg('event'), 'pending')
ON CONFLICT (callback_id, dedup_key) DO NOTHING
RETURNING *;

-- name: FinishIssueChannelCallbackDelivery :exec
UPDATE issue_channel_callback_delivery
SET status = sqlc.arg('status'),
    provider_message_id = sqlc.arg('provider_message_id'),
    error = sqlc.arg('error'),
    updated_at = now()
WHERE callback_id = sqlc.arg('callback_id') AND dedup_key = sqlc.arg('dedup_key');
