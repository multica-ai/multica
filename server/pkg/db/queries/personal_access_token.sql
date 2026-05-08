-- name: CreatePersonalAccessToken :one
INSERT INTO personal_access_token (user_id, name, token_hash, token_prefix, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetPersonalAccessTokenByHash :one
SELECT * FROM personal_access_token
WHERE token_hash = $1
  AND revoked = FALSE
  AND (expires_at IS NULL OR expires_at > now());

-- name: ListPersonalAccessTokensByUser :many
SELECT * FROM personal_access_token
WHERE user_id = $1
  AND revoked = FALSE
ORDER BY created_at DESC;

-- name: RevokePersonalAccessToken :one
UPDATE personal_access_token
SET revoked = TRUE
WHERE id = $1 AND user_id = $2
RETURNING token_hash;

-- name: UpdatePersonalAccessTokenLastUsed :exec
UPDATE personal_access_token
SET last_used_at = now()
WHERE id = $1;
