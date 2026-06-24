-- name: GetUserTOTPStatus :one
SELECT
    (totp_secret_encrypted IS NOT NULL) AS configured,
    (totp_enabled_at       IS NOT NULL) AS enabled
FROM "user"
WHERE id = $1;

-- name: GetUserTOTPSecret :one
SELECT totp_secret_encrypted, totp_enabled_at
FROM "user"
WHERE id = $1;

-- name: SetUserTOTPSecret :exec
UPDATE "user"
SET totp_secret_encrypted = $2,
    totp_enabled_at       = NULL
WHERE id = $1;

-- name: EnableUserTOTP :exec
UPDATE "user"
SET totp_enabled_at = now()
WHERE id = $1 AND totp_secret_encrypted IS NOT NULL;

-- name: DisableUserTOTP :exec
UPDATE "user"
SET totp_secret_encrypted = NULL,
    totp_enabled_at       = NULL
WHERE id = $1;

-- name: GetUserTOTPSecretByEmail :one
SELECT id, totp_secret_encrypted
FROM "user"
WHERE email = $1
  AND totp_enabled_at IS NOT NULL;
