-- name: GetAgentFeishuBotConfig :one
SELECT * FROM agent_feishu_bot_config
WHERE agent_id = $1;

-- name: GetAgentFeishuBotConfigByAppID :one
SELECT * FROM agent_feishu_bot_config
WHERE app_id = $1 AND enabled = true;

-- name: ListEnabledAgentFeishuBotConfigs :many
SELECT * FROM agent_feishu_bot_config
WHERE enabled = true
ORDER BY updated_at ASC;

-- name: UpsertAgentFeishuBotConfig :one
INSERT INTO agent_feishu_bot_config (
    agent_id, workspace_id, app_id, app_secret, verification_token, enabled
) VALUES (
    $1, $2, $3, $4, sqlc.narg('verification_token'), $5
)
ON CONFLICT (agent_id) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    app_id = EXCLUDED.app_id,
    app_secret = EXCLUDED.app_secret,
    verification_token = EXCLUDED.verification_token,
    enabled = EXCLUDED.enabled,
    updated_at = now()
RETURNING *;

-- name: DeleteAgentFeishuBotConfig :exec
DELETE FROM agent_feishu_bot_config
WHERE agent_id = $1;

-- name: GetFeishuAgentChatBinding :one
SELECT * FROM feishu_agent_chat_binding
WHERE app_id = $1 AND feishu_chat_id = $2 AND feishu_sender_id = $3;

-- name: GetFeishuAgentChatBindingBySession :one
SELECT * FROM feishu_agent_chat_binding
WHERE chat_session_id = $1;

-- name: UpsertFeishuAgentChatBinding :one
INSERT INTO feishu_agent_chat_binding (
    app_id, workspace_id, agent_id, user_id, feishu_chat_id, feishu_sender_id, chat_session_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (app_id, feishu_chat_id, feishu_sender_id) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    agent_id = EXCLUDED.agent_id,
    user_id = EXCLUDED.user_id,
    chat_session_id = EXCLUDED.chat_session_id,
    updated_at = now()
RETURNING *;

-- name: GetFeishuIssueThread :one
SELECT * FROM feishu_issue_thread
WHERE app_id = $1 AND feishu_chat_id = $2 AND feishu_thread_id = $3;

-- name: ListFeishuIssueThreadsByIssue :many
SELECT * FROM feishu_issue_thread
WHERE issue_id = $1;

-- name: UpsertFeishuIssueThread :one
INSERT INTO feishu_issue_thread (
    app_id, workspace_id, issue_id, agent_id, user_id, feishu_chat_id, feishu_thread_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (app_id, feishu_chat_id, feishu_thread_id) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    issue_id = EXCLUDED.issue_id,
    agent_id = EXCLUDED.agent_id,
    user_id = EXCLUDED.user_id,
    updated_at = now()
RETURNING *;
