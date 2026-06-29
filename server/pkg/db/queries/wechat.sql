-- WeChat Work (企业微信) intelligent bot integration queries.
-- Tables defined in server/migrations/119_wechat_integration.up.sql;
-- architecture documented in server/internal/integrations/wechat/doc.go.

-- =====================
-- wechat_installation
-- =====================

-- name: CreateWechatInstallation :one
INSERT INTO wechat_installation (
    workspace_id, agent_id, bot_id, secret_encrypted, installer_user_id
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: GetWechatInstallation :one
SELECT * FROM wechat_installation WHERE id = $1;

-- name: GetWechatInstallationByBotID :one
SELECT * FROM wechat_installation WHERE bot_id = $1;

-- name: GetWechatInstallationByAgent :one
SELECT * FROM wechat_installation
WHERE workspace_id = $1 AND agent_id = $2;

-- name: ListWechatInstallationsByWorkspace :many
SELECT * FROM wechat_installation
WHERE workspace_id = $1
ORDER BY installed_at DESC;

-- name: ListActiveWechatInstallations :many
SELECT * FROM wechat_installation
WHERE status = 'active'
ORDER BY installed_at;

-- name: RevokeWechatInstallation :exec
UPDATE wechat_installation
SET status = 'revoked', updated_at = now()
WHERE id = $1;

-- name: AcquireWechatWSLease :one
UPDATE wechat_installation
SET ws_lease_token       = sqlc.arg('new_token'),
    ws_lease_expires_at  = sqlc.arg('new_expires_at'),
    updated_at           = now()
WHERE id = sqlc.arg('id')
  AND status = 'active'
  AND (
        ws_lease_token IS NULL
        OR ws_lease_expires_at < now()
        OR ws_lease_token = sqlc.arg('new_token')
  )
RETURNING *;

-- name: ReleaseWechatWSLease :exec
UPDATE wechat_installation
SET ws_lease_token      = NULL,
    ws_lease_expires_at = NULL,
    updated_at          = now()
WHERE id = $1
  AND ws_lease_token = sqlc.arg('current_token');

-- =====================
-- wechat_user_binding
-- =====================

-- name: CreateWechatUserBinding :one
INSERT INTO wechat_user_binding (
    workspace_id, multica_user_id, installation_id, wechat_userid
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (installation_id, wechat_userid) DO UPDATE SET
    bound_at = now()
WHERE wechat_user_binding.multica_user_id = EXCLUDED.multica_user_id
RETURNING *;

-- name: GetWechatUserBindingByUserID :one
SELECT * FROM wechat_user_binding
WHERE installation_id = $1 AND wechat_userid = $2;

-- name: ListWechatUserBindingsByInstallation :many
SELECT * FROM wechat_user_binding
WHERE installation_id = $1
ORDER BY bound_at DESC;

-- name: DeleteWechatUserBinding :exec
DELETE FROM wechat_user_binding WHERE id = $1;

-- =====================
-- wechat_chat_session_binding
-- =====================

-- name: CreateWechatChatSessionBinding :one
INSERT INTO wechat_chat_session_binding (
    chat_session_id, installation_id, wechat_chat_id, wechat_chat_type
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetWechatChatSessionBinding :one
SELECT * FROM wechat_chat_session_binding
WHERE installation_id = $1 AND wechat_chat_id = $2;

-- name: GetWechatChatSessionBindingBySession :one
SELECT * FROM wechat_chat_session_binding
WHERE chat_session_id = $1;

-- =====================
-- wechat_inbound_message_dedup
-- =====================

-- name: ClaimWechatInboundDedup :one
INSERT INTO wechat_inbound_message_dedup (installation_id, message_id, claim_token)
VALUES ($1, $2, gen_random_uuid())
ON CONFLICT (message_id) DO UPDATE
    SET received_at = now(),
        claim_token = gen_random_uuid()
    WHERE wechat_inbound_message_dedup.processed_at IS NULL
      AND wechat_inbound_message_dedup.received_at < now() - INTERVAL '60 seconds'
RETURNING *;

-- name: MarkWechatInboundDedupProcessed :execrows
UPDATE wechat_inbound_message_dedup
SET processed_at = now()
WHERE message_id = $1
  AND claim_token = $2
  AND processed_at IS NULL;

-- name: ReleaseWechatInboundDedup :execrows
DELETE FROM wechat_inbound_message_dedup
WHERE message_id = $1
  AND claim_token = $2
  AND processed_at IS NULL;

-- name: PurgeWechatInboundDedup :exec
DELETE FROM wechat_inbound_message_dedup
WHERE received_at < $1;

-- =====================
-- wechat_inbound_audit
-- =====================

-- name: CreateWechatInboundAudit :exec
INSERT INTO wechat_inbound_audit (
    installation_id, wechat_chat_id, event_type, wechat_message_id, drop_reason
) VALUES (
    sqlc.narg('installation_id'), sqlc.narg('wechat_chat_id'),
    $1, sqlc.narg('wechat_message_id'), $2
);
