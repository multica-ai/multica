-- name: GetExternalAuthIdentity :one
SELECT * FROM external_auth_identity
WHERE provider = $1 AND issuer = $2 AND subject = $3;

-- name: CreateExternalAuthIdentity :one
INSERT INTO external_auth_identity (user_id, provider, issuer, subject)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provider, issuer, subject) DO UPDATE
SET updated_at = now()
RETURNING *;
