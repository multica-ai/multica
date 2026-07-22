-- name: CreateSetupToken :one
INSERT INTO setup_token (user_id, workspace_id, token_hash, token_prefix, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: RedeemSetupToken :one
-- Atomic single-use claim: stamps used_at only when the token is still
-- unredeemed and unexpired. Phrasing the guard in the WHERE (not in
-- application code after a SELECT) makes concurrent redeems safe — the first
-- writer flips used_at, every racing writer matches zero rows and sqlc :one
-- returns pgx.ErrNoRows, which the handler maps to 401. Returns the owning
-- user + workspace so the handler can mint the PAT and notify the dialog.
UPDATE setup_token
SET used_at = now()
WHERE token_hash = $1
  AND used_at IS NULL
  AND expires_at > now()
RETURNING id, user_id, workspace_id;

-- name: DeleteExpiredSetupTokens :exec
-- Opportunistic reaper called on every mint. Setup tokens are short-lived and
-- there is no scheduled GC for them, so clearing lapsed rows here keeps the
-- table bounded by "tokens minted in the last few minutes" without a cron job.
DELETE FROM setup_token
WHERE expires_at < now();
