-- name: CreateDaemonEnrollment :one
INSERT INTO daemon_enrollment (workspace_id, daemon_id, name, creator_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListDaemonEnrollments :many
SELECT * FROM daemon_enrollment WHERE workspace_id = $1 ORDER BY created_at, id;

-- name: RotateDaemonEnrollment :one
UPDATE daemon_enrollment
SET token_hash = @token_hash, generation = generation + 1,
    expires_at = @expires_at, updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND revoked_at IS NULL
RETURNING *;

-- name: RevokeDaemonEnrollment :one
UPDATE daemon_enrollment
SET revoked_at = COALESCE(revoked_at, now()), generation = generation + 1,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: LockLiveDaemonEnrollment :one
-- Authoritative authorization, not a best-effort auth-cache lookup. The shared
-- row lock fences mutations against concurrent administrator rotate/revoke.
SELECT e.* FROM daemon_enrollment e
JOIN workspace w ON w.id = e.workspace_id
JOIN member m ON m.workspace_id = e.workspace_id AND m.user_id = e.creator_id
WHERE e.token_hash = $1 AND e.revoked_at IS NULL AND e.expires_at > now()
FOR SHARE OF e;

-- name: DeleteDaemonEnrollmentsByWorkspace :exec
DELETE FROM daemon_enrollment WHERE workspace_id = $1;

-- name: LockDaemonEnrollment :one
SELECT * FROM daemon_enrollment WHERE id = $1 AND workspace_id = $2 FOR UPDATE;

-- name: DeleteDaemonEnrollmentsByCreator :exec
DELETE FROM daemon_enrollment WHERE workspace_id = $1 AND creator_id = $2;

-- name: DeleteDaemonTokensByEnrollment :many
DELETE FROM daemon_token WHERE enrollment_token_hash = $1 RETURNING token_hash;
