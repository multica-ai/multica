-- name: GetExternalAccountBindingByProviderAndExternalID :one
SELECT * FROM external_account_binding
WHERE provider = $1 AND external_user_id = $2;

-- name: GetExternalAccountBindingByUserAndProvider :one
SELECT * FROM external_account_binding
WHERE user_id = $1 AND provider = $2;

-- name: UpsertExternalAccountBinding :one
INSERT INTO external_account_binding (
    user_id, provider, external_user_id, display_name,
    access_token_encrypted, refresh_token_encrypted,
    token_expires_at, metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (user_id, provider) DO UPDATE SET
    external_user_id = EXCLUDED.external_user_id,
    display_name = EXCLUDED.display_name,
    access_token_encrypted = EXCLUDED.access_token_encrypted,
    refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
    token_expires_at = EXCLUDED.token_expires_at,
    metadata = EXCLUDED.metadata,
    status = 'active',
    updated_at = now()
RETURNING *;
