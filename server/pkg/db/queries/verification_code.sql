-- name: CreateVerificationCode :one
INSERT INTO multica_verification_code (email, code, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetLatestVerificationCode :one
SELECT * FROM multica_verification_code
WHERE email = $1
  AND used = FALSE
  AND expires_at > now()
  AND attempts < 5
ORDER BY created_at DESC
LIMIT 1;

-- name: MarkVerificationCodeUsed :exec
UPDATE multica_verification_code
SET used = TRUE
WHERE id = $1;

-- name: IncrementVerificationCodeAttempts :exec
UPDATE multica_verification_code
SET attempts = attempts + 1
WHERE id = $1;

-- name: GetLatestCodeByEmail :one
SELECT * FROM multica_verification_code
WHERE email = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: DeleteExpiredVerificationCodes :exec
DELETE FROM multica_verification_code
WHERE expires_at < now() - interval '1 hour';
