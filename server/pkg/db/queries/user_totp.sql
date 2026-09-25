-- name: GetUserTOTPStatus :one
SELECT
    (totp_secret_encrypted IS NOT NULL) AS configured,
    (totp_enabled_at       IS NOT NULL) AS enabled
FROM "user"
WHERE id = $1;

-- name: GetUserTOTPSecret :one
SELECT totp_secret_encrypted, totp_enabled_at, totp_locked_until
FROM "user"
WHERE id = $1;

-- name: SetUserTOTPSecret :execrows
-- Stores a new pending secret. Refuses (0 rows) once TOTP is enabled, so a
-- setup-init racing with setup-verify cannot overwrite a verified secret.
UPDATE "user"
SET totp_secret_encrypted = $2,
    totp_enabled_at       = NULL,
    totp_last_used_step   = NULL,
    totp_failed_attempts  = 0,
    totp_locked_until     = NULL
WHERE id = $1
  AND totp_enabled_at IS NULL;

-- name: EnableUserTOTP :execrows
-- Enables exactly the secret that was verified: 0 rows when another
-- setup-init replaced the pending secret in the meantime.
UPDATE "user"
SET totp_enabled_at      = now(),
    totp_last_used_step  = sqlc.arg(step)::bigint,
    totp_failed_attempts = 0,
    totp_locked_until    = NULL
WHERE id = sqlc.arg(id)
  AND totp_secret_encrypted = sqlc.arg(totp_secret_encrypted)
  AND totp_enabled_at IS NULL;

-- name: DisableUserTOTP :exec
UPDATE "user"
SET totp_secret_encrypted = NULL,
    totp_enabled_at       = NULL,
    totp_last_used_step   = NULL,
    totp_failed_attempts  = 0,
    totp_locked_until     = NULL
WHERE id = $1;

-- name: GetUserTOTPSecretByEmail :one
SELECT id, totp_secret_encrypted, totp_locked_until
FROM "user"
WHERE email = $1
  AND totp_enabled_at IS NOT NULL;

-- name: ConsumeUserTOTPStep :execrows
-- Accepts a verified code: succeeds only for a time step later than the last
-- accepted one and while the account is not locked. Concurrent submissions of
-- the same code race on this conditional write, so exactly one wins.
UPDATE "user"
SET totp_last_used_step  = sqlc.arg(step)::bigint,
    totp_failed_attempts = 0,
    totp_locked_until    = NULL
WHERE id = sqlc.arg(id)
  AND totp_enabled_at IS NOT NULL
  AND (totp_last_used_step IS NULL OR totp_last_used_step < sqlc.arg(step)::bigint)
  AND (totp_locked_until IS NULL OR totp_locked_until <= now());

-- name: RecordUserTOTPFailure :exec
-- Counts a rejected code. Reaching max_attempts locks TOTP for the account
-- until lock_until; the counter only resets on an accepted code, so each
-- further failure after the budget re-locks immediately.
UPDATE "user"
SET totp_failed_attempts = totp_failed_attempts + 1,
    totp_locked_until = CASE
        WHEN totp_failed_attempts + 1 >= sqlc.arg(max_attempts)::int THEN sqlc.arg(lock_until)::timestamptz
        ELSE totp_locked_until
    END
WHERE id = sqlc.arg(id);
