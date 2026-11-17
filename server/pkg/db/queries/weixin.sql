-- name: CreateWeixinLoginSession :one
INSERT INTO weixin_login_session (id, workspace_id, agent_id, initiator_user_id, state_encrypted, expires_at)
VALUES ($1, $2, $3, $4, $5, now() + interval '5 minutes') RETURNING *;

-- name: GetWeixinLoginSession :one
SELECT * FROM weixin_login_session
WHERE id = $1 AND workspace_id = $2 AND initiator_user_id = $3;

-- name: LockWeixinLoginSession :one
SELECT * FROM weixin_login_session
WHERE id = $1 AND lease_token = $2 AND expires_at > now()
  AND (status IN ('wait', 'scaned', 'scaned_but_redirect') OR strpos(status, 'need_verifycode') > 0)
FOR UPDATE;

-- name: ClaimWeixinLoginSession :one
UPDATE weixin_login_session
SET lease_token = sqlc.arg(lease_token), lease_expires_at = now() + interval '50 seconds'
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
  AND initiator_user_id = sqlc.arg(initiator_user_id) AND expires_at > now()
  AND (status IN ('wait', 'scaned', 'scaned_but_redirect') OR strpos(status, 'need_verifycode') > 0)
  AND (lease_expires_at IS NULL OR lease_expires_at < now())
RETURNING *;

-- name: UpdateWeixinLoginSession :execrows
UPDATE weixin_login_session
SET status = sqlc.arg(status), state_encrypted = sqlc.arg(state_encrypted),
    installation_id = sqlc.narg(installation_id), lease_token = NULL, lease_expires_at = NULL
WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(lease_token)
  AND (status IN ('wait', 'scaned', 'scaned_but_redirect') OR strpos(status, 'need_verifycode') > 0);

-- name: ReleaseWeixinLoginSession :exec
UPDATE weixin_login_session SET lease_token = NULL, lease_expires_at = NULL
WHERE id = $1 AND lease_token = $2;

-- name: CancelWeixinLoginSession :execrows
UPDATE weixin_login_session SET status = 'cancelled', state_encrypted = '', lease_token = NULL, lease_expires_at = NULL
WHERE id = $1 AND workspace_id = $2 AND initiator_user_id = $3
  AND (status IN ('wait', 'scaned', 'scaned_but_redirect') OR strpos(status, 'need_verifycode') > 0);

-- name: CancelPreviousWeixinLogins :exec
UPDATE weixin_login_session SET status = 'cancelled', state_encrypted = '', lease_token = NULL, lease_expires_at = NULL
WHERE workspace_id = $1 AND agent_id = $2 AND initiator_user_id = $3
  AND (status IN ('wait', 'scaned', 'scaned_but_redirect') OR strpos(status, 'need_verifycode') > 0);

-- name: PurgeWeixinState :exec
WITH expired AS (
  DELETE FROM weixin_login_session WHERE expires_at < now() - interval '1 hour'
)
DELETE FROM weixin_account_state state
WHERE NOT EXISTS (SELECT 1 FROM channel_installation ci WHERE ci.id = state.installation_id AND ci.channel_type = 'weixin');

-- name: GetWeixinInstallationForAgent :one
SELECT * FROM channel_installation
WHERE workspace_id = $1 AND agent_id = $2 AND channel_type = 'weixin' FOR UPDATE;

-- name: ResetWeixinAccountState :exec
INSERT INTO weixin_account_state (installation_id, bot_id, user_id)
VALUES ($1, $2, $3)
ON CONFLICT (installation_id) DO UPDATE SET bot_id = EXCLUDED.bot_id, user_id = EXCLUDED.user_id,
    get_updates_buf = '', context_token_encrypted = '', context_time_ms = 0, updated_at = now();

-- name: GetWeixinAccountState :one
SELECT * FROM weixin_account_state WHERE installation_id = $1 AND bot_id = $2;

-- name: SaveWeixinContext :execrows
UPDATE weixin_account_state SET context_token_encrypted = $4, context_time_ms = $5, updated_at = now()
WHERE installation_id = $1 AND bot_id = $2 AND user_id = $3 AND context_time_ms <= $5;

-- name: SaveWeixinCursor :execrows
UPDATE weixin_account_state SET get_updates_buf = $3, updated_at = now()
WHERE installation_id = $1 AND bot_id = $2;

-- name: DeleteWeixinAccountState :exec
DELETE FROM weixin_account_state WHERE installation_id = $1;

-- name: ExpireWeixinInstallation :execrows
UPDATE channel_installation SET status = 'revoked',
    config = jsonb_set(config, '{authorization_expired}', 'true'), updated_at = now()
WHERE id = $1 AND channel_type = 'weixin' AND status = 'active' AND config = $2;

-- name: IsWeixinTypingActive :one
SELECT EXISTS (
 SELECT 1 FROM chat_session s
 JOIN channel_installation i ON i.id = @installation_id AND i.workspace_id = s.workspace_id
 JOIN channel_chat_session_binding b ON b.chat_session_id = s.id AND b.installation_id = i.id
 JOIN chat_message m ON m.chat_session_id = s.id
 WHERE s.id = @chat_session_id AND s.status = 'active'
  AND i.channel_type = 'weixin' AND i.status = 'active' AND i.config = @config
  AND b.channel_type = 'weixin' AND b.retired_at IS NULL
  AND m.channel_ingested AND m.role = 'user' AND NOT m.channel_typing_settled
  AND COALESCE(m.channel_context_revision, 1) = b.context_revision
  AND ((m.task_id IS NULL AND m.created_at > now() - interval '2 minutes') OR EXISTS (
   SELECT 1 FROM agent_task_queue t WHERE t.chat_session_id = s.id
    AND (t.id = m.task_id OR t.chat_input_task_id = m.task_id)
    AND t.status IN ('queued','dispatched','running','waiting_local_directory','deferred')
  ))
) AS active;

-- name: SettleWeixinTypingInputs :exec
UPDATE chat_message m SET channel_typing_settled = true
FROM chat_message cutoff, chat_session s, channel_installation i
WHERE cutoff.id = @through_message_id AND cutoff.chat_session_id = s.id
 AND s.id = @chat_session_id AND s.workspace_id = @workspace_id
 AND i.id = @installation_id AND i.workspace_id = s.workspace_id AND i.channel_type = 'weixin'
 AND EXISTS (SELECT 1 FROM channel_chat_session_binding b WHERE b.chat_session_id = s.id
  AND b.installation_id = i.id AND b.channel_type = 'weixin')
 AND m.chat_session_id = s.id AND m.channel_ingested AND m.role = 'user' AND m.task_id IS NULL
 AND COALESCE(m.channel_context_revision, 1) = @context_revision
 AND (m.created_at, m.id) <= (cutoff.created_at, cutoff.id);
