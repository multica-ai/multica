-- name: CreateWeixinLoginSession :one
INSERT INTO weixin_login_session (id, workspace_id, agent_id, initiator_user_id, state_encrypted, expires_at)
VALUES ($1, $2, $3, $4, $5, now() + interval '5 minutes') RETURNING *;

-- name: GetWeixinLoginSession :one
SELECT * FROM weixin_login_session
WHERE id = $1 AND workspace_id = $2 AND initiator_user_id = $3;

-- name: LockWeixinLoginSession :one
SELECT * FROM weixin_login_session
WHERE id = $1 AND lease_token = $2 AND expires_at > now()
  AND status IN ('wait', 'scaned', 'need_verifycode', 'scaned_but_redirect')
FOR UPDATE;

-- name: ClaimWeixinLoginSession :one
UPDATE weixin_login_session
SET lease_token = sqlc.arg(lease_token), lease_expires_at = now() + interval '50 seconds'
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
  AND initiator_user_id = sqlc.arg(initiator_user_id) AND expires_at > now()
  AND status IN ('wait', 'scaned', 'need_verifycode', 'scaned_but_redirect')
  AND (lease_expires_at IS NULL OR lease_expires_at < now())
RETURNING *;

-- name: UpdateWeixinLoginSession :execrows
UPDATE weixin_login_session
SET status = sqlc.arg(status), state_encrypted = sqlc.arg(state_encrypted),
    installation_id = sqlc.narg(installation_id), lease_token = NULL, lease_expires_at = NULL
WHERE id = sqlc.arg(id) AND lease_token = sqlc.arg(lease_token)
  AND status IN ('wait', 'scaned', 'need_verifycode', 'scaned_but_redirect');

-- name: ReleaseWeixinLoginSession :exec
UPDATE weixin_login_session SET lease_token = NULL, lease_expires_at = NULL
WHERE id = $1 AND lease_token = $2;

-- name: CancelWeixinLoginSession :execrows
UPDATE weixin_login_session SET status = 'cancelled', state_encrypted = '', lease_token = NULL, lease_expires_at = NULL
WHERE id = $1 AND workspace_id = $2 AND initiator_user_id = $3
  AND status IN ('wait', 'scaned', 'need_verifycode', 'scaned_but_redirect');

-- name: CancelPreviousWeixinLogins :exec
UPDATE weixin_login_session SET status = 'cancelled', state_encrypted = '', lease_token = NULL, lease_expires_at = NULL
WHERE workspace_id = $1 AND agent_id = $2 AND initiator_user_id = $3
  AND status IN ('wait', 'scaned', 'need_verifycode', 'scaned_but_redirect');

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
UPDATE channel_installation SET status = 'revoked', updated_at = now()
WHERE id = $1 AND channel_type = 'weixin' AND status = 'active' AND config = $2;
