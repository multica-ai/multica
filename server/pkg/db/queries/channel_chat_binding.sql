-- name: ListChannelChatBindings :many
SELECT * FROM channel_chat_binding
WHERE workspace_id = $1
ORDER BY is_primary DESC, created_at ASC;

-- name: GetChannelChatBinding :one
SELECT * FROM channel_chat_binding
WHERE id = $1;

-- name: GetChannelChatBindingByProviderAndChatID :one
SELECT * FROM channel_chat_binding
WHERE connection_id = $1 AND external_chat_id = $2;

-- name: GetChannelChatBindingContextForInbound :one
SELECT
    b.workspace_id::text AS workspace_id,
    COALESCE(b.default_project_id::text, '') AS default_project_id,
    b.listen_mode,
    COALESCE(b.agent_id::text, '') AS agent_id,
    w.issue_prefix
FROM channel_chat_binding b
JOIN workspace w ON w.id = b.workspace_id
WHERE b.connection_id = $1 AND b.external_chat_id = $2;

-- name: CreateChannelChatBinding :one
INSERT INTO channel_chat_binding (
    provider, connection_id, external_chat_id, chat_type, workspace_id,
    is_primary, bound_by_user_id, external_chat_name, default_project_id,
    listen_mode, agent_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    sqlc.narg('default_project_id'),
    $9,
    sqlc.narg('agent_id')
)
RETURNING *;

-- name: UpdateChannelChatBindingDefaultProject :one
UPDATE channel_chat_binding SET
    default_project_id = sqlc.narg('default_project_id'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateChannelChatBindingSettings :one
UPDATE channel_chat_binding SET
    default_project_id = $2,
    listen_mode = $3,
    agent_id = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteChannelChatBinding :exec
DELETE FROM channel_chat_binding WHERE id = $1;

-- name: SetChannelChatBindingPrimary :one
UPDATE channel_chat_binding SET
    is_primary = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearPrimaryBindingsForWorkspaceProvider :exec
UPDATE channel_chat_binding SET
    is_primary = false,
    updated_at = now()
WHERE workspace_id = $1 AND connection_id = $2 AND is_primary = true;

-- name: GetPrimaryChannelChatBinding :one
SELECT * FROM channel_chat_binding
WHERE workspace_id = $1 AND connection_id = $2 AND is_primary = true;
