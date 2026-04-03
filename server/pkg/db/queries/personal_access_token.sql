-- name: CreatePersonalAccessToken :one
INSERT INTO personal_access_token (id, user_id, name, token_hash, token_prefix, expires_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPersonalAccessTokenByHash :one
SELECT * FROM personal_access_token
WHERE token_hash = ?
  AND revoked = 0
  AND (expires_at IS NULL OR expires_at > datetime('now'));

-- name: ListPersonalAccessTokensByUser :many
SELECT * FROM personal_access_token
WHERE user_id = ?
  AND revoked = 0
ORDER BY created_at DESC;

-- name: RevokePersonalAccessToken :exec
UPDATE personal_access_token
SET revoked = 1
WHERE id = ? AND user_id = ?;

-- name: UpdatePersonalAccessTokenLastUsed :exec
UPDATE personal_access_token
SET last_used_at = datetime('now')
WHERE id = ?;
