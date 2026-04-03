-- name: CreateVerificationCode :one
INSERT INTO verification_code (id, email, code, expires_at)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetLatestVerificationCode :one
SELECT * FROM verification_code
WHERE email = ?
  AND used = 0
  AND expires_at > datetime('now')
  AND attempts < 5
ORDER BY created_at DESC
LIMIT 1;

-- name: MarkVerificationCodeUsed :exec
UPDATE verification_code
SET used = 1
WHERE id = ?;

-- name: IncrementVerificationCodeAttempts :exec
UPDATE verification_code
SET attempts = attempts + 1
WHERE id = ?;

-- name: GetLatestCodeByEmail :one
SELECT * FROM verification_code
WHERE email = ?
ORDER BY created_at DESC
LIMIT 1;

-- name: DeleteExpiredVerificationCodes :exec
DELETE FROM verification_code
WHERE expires_at < datetime('now', '-1 hour');
