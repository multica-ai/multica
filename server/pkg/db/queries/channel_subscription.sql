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

-- name: DeleteChannelIssueSubscriptionByID :exec
DELETE FROM channel_issue_subscription WHERE id = sqlc.arg('id');

-- name: ListChannelIssueSubscriptionsForIssue :many
SELECT * FROM channel_issue_subscription
WHERE issue_id = sqlc.arg('issue_id') AND workspace_id = sqlc.arg('workspace_id')
ORDER BY created_at, id;

-- name: CreateChannelProjectSubscription :one
-- Returns no row when the chat already follows the project.
INSERT INTO channel_project_subscription (
    workspace_id, project_id, installation_id, channel_type,
    chat_id, thread_id, anchor_message_id, created_by
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('project_id'), sqlc.arg('installation_id'), sqlc.arg('channel_type'),
    sqlc.arg('chat_id'), sqlc.arg('thread_id'), sqlc.arg('anchor_message_id'), sqlc.narg('created_by')
)
ON CONFLICT (project_id, installation_id, chat_id, thread_id) DO NOTHING
RETURNING *;

-- name: DeleteChannelProjectSubscriptionByID :exec
DELETE FROM channel_project_subscription WHERE id = sqlc.arg('id');

-- name: DeleteChannelProjectSubscriptionsByProject :exec
DELETE FROM channel_project_subscription
WHERE project_id = sqlc.arg('project_id') AND workspace_id = sqlc.arg('workspace_id');

-- name: ListChannelProjectSubscriptionsForProject :many
SELECT * FROM channel_project_subscription
WHERE project_id = sqlc.arg('project_id') AND workspace_id = sqlc.arg('workspace_id')
ORDER BY created_at, id;

-- name: ListProjectsByTitle :many
-- Resolves a project named in a chat command; callers drop the ones the sender
-- cannot see and treat more than one remaining row as ambiguous.
SELECT * FROM project
WHERE workspace_id = sqlc.arg('workspace_id') AND lower(title) = lower(sqlc.arg('title'))
ORDER BY created_at, id;
