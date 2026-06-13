-- WeCom intelligent robot integration queries.

-- name: UpsertWecomInstallation :one
INSERT INTO wecom_installation (
    workspace_id, agent_id, bot_id, bot_secret_encrypted,
    corp_id, corp_secret_encrypted, self_build_agent_id, installer_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, sqlc.narg('self_build_agent_id'), $7
)
ON CONFLICT (workspace_id, agent_id) DO UPDATE SET
    bot_id                = EXCLUDED.bot_id,
    bot_secret_encrypted  = EXCLUDED.bot_secret_encrypted,
    corp_id               = EXCLUDED.corp_id,
    corp_secret_encrypted = EXCLUDED.corp_secret_encrypted,
    self_build_agent_id   = EXCLUDED.self_build_agent_id,
    installer_user_id     = EXCLUDED.installer_user_id,
    status                = 'active',
    installed_at          = now(),
    updated_at            = now()
RETURNING *;

-- name: GetWecomInstallation :one
SELECT * FROM wecom_installation WHERE id = $1;

-- name: GetWecomInstallationInWorkspace :one
SELECT * FROM wecom_installation
WHERE id = $1 AND workspace_id = $2;

-- name: GetWecomInstallationByAgent :one
SELECT * FROM wecom_installation
WHERE workspace_id = $1 AND agent_id = $2;

-- name: GetWecomInstallationByBotID :one
SELECT * FROM wecom_installation WHERE bot_id = $1;

-- name: ListWecomInstallationsByWorkspace :many
SELECT * FROM wecom_installation
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: ListActiveWecomInstallations :many
SELECT * FROM wecom_installation
WHERE status = 'active'
ORDER BY created_at ASC;

-- name: SetWecomInstallationStatus :exec
UPDATE wecom_installation
SET status = $2, updated_at = now()
WHERE id = $1;

-- name: AcquireWecomWSLease :one
UPDATE wecom_installation
SET ws_lease_token      = sqlc.arg('new_token'),
    ws_lease_expires_at = sqlc.arg('new_expires_at'),
    updated_at          = now()
WHERE id = sqlc.arg('id')
  AND status = 'active'
  AND (
        ws_lease_token IS NULL
        OR ws_lease_expires_at < now()
        OR ws_lease_token = sqlc.arg('new_token')
  )
RETURNING *;

-- name: ReleaseWecomWSLease :exec
UPDATE wecom_installation
SET ws_lease_token      = NULL,
    ws_lease_expires_at = NULL,
    updated_at          = now()
WHERE id = $1
  AND ws_lease_token = sqlc.arg('current_token');

-- name: CreateWecomUserBinding :one
INSERT INTO wecom_user_binding (
    workspace_id, multica_user_id, installation_id, wecom_userid, wecom_open_userid
) VALUES (
    $1, $2, $3, $4, sqlc.narg('wecom_open_userid')
)
ON CONFLICT (installation_id, wecom_userid) DO UPDATE SET
    wecom_open_userid = COALESCE(EXCLUDED.wecom_open_userid, wecom_user_binding.wecom_open_userid),
    bound_at = now()
WHERE wecom_user_binding.multica_user_id = EXCLUDED.multica_user_id
RETURNING *;

-- name: GetWecomUserBindingByUserid :one
SELECT * FROM wecom_user_binding
WHERE installation_id = $1 AND wecom_userid = $2;

-- name: CreateWecomChatSessionBinding :one
INSERT INTO wecom_chat_session_binding (
    chat_session_id, installation_id, wecom_chat_id, wecom_chat_type
) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetWecomChatSessionBinding :one
SELECT * FROM wecom_chat_session_binding
WHERE installation_id = $1 AND wecom_chat_id = $2;

-- name: GetWecomChatSessionBindingBySession :one
SELECT * FROM wecom_chat_session_binding
WHERE chat_session_id = $1;

-- name: ClaimWecomInboundDedup :one
INSERT INTO wecom_inbound_message_dedup (installation_id, message_id, claim_token)
VALUES ($1, $2, gen_random_uuid())
ON CONFLICT (installation_id, message_id) DO UPDATE
    SET received_at = now(),
        claim_token = gen_random_uuid()
    WHERE wecom_inbound_message_dedup.processed_at IS NULL
      AND wecom_inbound_message_dedup.received_at < now() - INTERVAL '60 seconds'
RETURNING installation_id, message_id, received_at, processed_at, claim_token;

-- name: MarkWecomInboundDedupProcessed :execrows
UPDATE wecom_inbound_message_dedup
SET processed_at = now()
WHERE installation_id = $1
  AND message_id = $2
  AND claim_token = $3
  AND processed_at IS NULL;

-- name: ReleaseWecomInboundDedup :execrows
DELETE FROM wecom_inbound_message_dedup
WHERE installation_id = $1
  AND message_id = $2
  AND claim_token = $3
  AND processed_at IS NULL;

-- name: RecordWecomInboundDrop :exec
INSERT INTO wecom_inbound_audit (
    installation_id, wecom_chat_id, event_type, wecom_message_id, drop_reason
) VALUES (
    sqlc.narg('installation_id'),
    sqlc.narg('wecom_chat_id'),
    $1,
    sqlc.narg('wecom_message_id'),
    $2
);

-- name: CreateWecomOutboundStream :one
INSERT INTO wecom_outbound_stream (
    installation_id, chat_session_id, task_id, req_id, stream_id,
    wecom_chat_id, wecom_chat_type, status
) VALUES (
    $1, $2, sqlc.narg('task_id'), $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetWecomOutboundStreamByTask :one
SELECT * FROM wecom_outbound_stream WHERE task_id = $1;

-- name: UpdateWecomOutboundStreamStatus :exec
UPDATE wecom_outbound_stream
SET status = $2, updated_at = now()
WHERE id = $1;

-- name: AttachWecomOutboundStreamTask :execrows
UPDATE wecom_outbound_stream
SET task_id = $2, updated_at = now()
WHERE id = (
    SELECT id FROM wecom_outbound_stream
    WHERE chat_session_id = $1
      AND status = 'streaming'
      AND task_id IS NULL
    ORDER BY created_at DESC
    LIMIT 1
);

-- name: GetWecomOutboundStreamByChatSession :one
SELECT * FROM wecom_outbound_stream
WHERE chat_session_id = $1 AND status = 'streaming'
ORDER BY created_at DESC
LIMIT 1;

-- name: CreateWecomBindingToken :exec
INSERT INTO wecom_binding_token (
    token_hash, workspace_id, installation_id, wecom_userid, expires_at
) VALUES ($1, $2, $3, $4, $5);

-- name: ConsumeWecomBindingToken :one
UPDATE wecom_binding_token
SET consumed_at = now()
WHERE token_hash = $1
  AND consumed_at IS NULL
  AND expires_at > now()
RETURNING workspace_id, installation_id, wecom_userid;
