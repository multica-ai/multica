-- name: CreatePersonalAccessToken :one
INSERT INTO personal_access_token (user_id, name, token_hash, token_prefix, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateWorkspaceToken :one
-- A workspace token is a personal access token confined to one workspace.
-- The membership check is part of the insert: no row comes back when the
-- user is not a member of the workspace, or the workspace is gone.
INSERT INTO personal_access_token (user_id, name, token_hash, token_prefix, expires_at, workspace_id)
SELECT m.user_id, sqlc.arg(name), sqlc.arg(token_hash), sqlc.arg(token_prefix), sqlc.narg(expires_at), m.workspace_id
FROM member m
WHERE m.user_id = sqlc.arg(user_id)
  AND m.workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: GetPersonalAccessTokenByHash :one
-- Account-wide tokens only. A workspace token is resolved by
-- GetWorkspaceTokenByHash, so no caller of this query can treat one as an
-- account-wide token.
SELECT * FROM personal_access_token
WHERE token_hash = $1
  AND workspace_id IS NULL
  AND revoked = FALSE
  AND (expires_at IS NULL OR expires_at > now());

-- name: GetWorkspaceTokenByHash :one
SELECT * FROM personal_access_token
WHERE token_hash = $1
  AND workspace_id IS NOT NULL
  AND revoked = FALSE
  AND (expires_at IS NULL OR expires_at > now());

-- name: DeleteWorkspaceTokensByMember :many
-- A member who leaves a workspace takes their tokens for it with them, so a
-- later return to the workspace does not bring an old token back to life.
DELETE FROM personal_access_token
WHERE user_id = $1
  AND workspace_id = $2
RETURNING token_hash;

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

-- name: ExtendPersonalAccessTokenExpiry :one
-- In-place renew: only bumps expires_at when the token is still valid
-- (not revoked, not already expired) AND the existing expires_at is
-- still inside the renewal threshold ($3, e.g. now + 7d). Phrasing the
-- CAS this way — "is the row still renewable?" rather than "is the
-- requested new expiry larger than the current one?" — makes concurrent
-- renews idempotent: once writer A bumps expires_at past the threshold,
-- writer B's UPDATE matches zero rows (sqlc :one returns pgx.ErrNoRows,
-- which the caller treats as "already renewed"). A naive `expires_at <
-- $2` would still match because two callers race-computing
-- `$2 = now + 90d` produce strictly-different values and the second
-- one's $2 is always greater than the row A just wrote.
UPDATE personal_access_token
SET expires_at = sqlc.arg(new_expires_at)
WHERE id = sqlc.arg(id)
  AND revoked = FALSE
  AND expires_at IS NOT NULL
  AND expires_at > now()
  AND expires_at <= sqlc.arg(renew_threshold_at)
RETURNING expires_at;
