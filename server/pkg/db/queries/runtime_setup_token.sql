-- CEREBRO-PATCH(sqlc-runtime-setup-token): cerebro modification of upstream file
-- name: CreateRuntimeSetupToken :one
INSERT INTO runtime_setup_token (
    token_hash,
    token_prefix,
    user_id,
    workspace_id,
    device_label,
    expires_at
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetRuntimeSetupTokenByHash :one
SELECT * FROM runtime_setup_token
WHERE token_hash = $1;

-- name: ConsumeRuntimeSetupToken :exec
UPDATE runtime_setup_token
SET used_at     = now(),
    used_pat_id = $2
WHERE id = $1
  AND used_at IS NULL
  AND expires_at > now();

-- name: DeleteExpiredRuntimeSetupTokens :exec
DELETE FROM runtime_setup_token
WHERE expires_at < now() - INTERVAL '1 day';
