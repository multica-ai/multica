-- name: CreateChannelIssueSubscription :one
-- Returns no row when the chat already follows the issue.
INSERT INTO channel_issue_subscription (
    workspace_id, issue_id, installation_id, channel_type,
    chat_id, thread_id, anchor_message_id, created_by
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('issue_id'), sqlc.arg('installation_id'), sqlc.arg('channel_type'),
    sqlc.arg('chat_id'), sqlc.arg('thread_id'), sqlc.arg('anchor_message_id'), sqlc.narg('created_by')
)
ON CONFLICT (issue_id, installation_id, chat_id, thread_id) DO NOTHING
RETURNING *;

-- name: DeleteChannelIssueSubscription :execrows
DELETE FROM channel_issue_subscription
WHERE workspace_id = sqlc.arg('workspace_id')
  AND issue_id = sqlc.arg('issue_id')
  AND installation_id = sqlc.arg('installation_id')
  AND chat_id = sqlc.arg('chat_id')
  AND thread_id = sqlc.arg('thread_id');

-- name: DeleteChannelIssueSubscriptionByID :exec
DELETE FROM channel_issue_subscription WHERE id = sqlc.arg('id');

-- name: ListChannelIssueSubscriptionsForIssue :many
SELECT * FROM channel_issue_subscription
WHERE issue_id = sqlc.arg('issue_id') AND workspace_id = sqlc.arg('workspace_id')
ORDER BY created_at, id;
