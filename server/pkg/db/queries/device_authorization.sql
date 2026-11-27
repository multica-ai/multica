-- name: CreateDeviceAuthorization :one
INSERT INTO device_authorization (device_code_hash, user_code_hash, expires_at, interval_seconds, client_name)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetPendingDeviceAuthorizationByUserCode :one
SELECT * FROM device_authorization
WHERE user_code_hash = $1
  AND status = 'pending'
  AND expires_at > now()
ORDER BY created_at DESC
LIMIT 1;

-- name: ApproveDeviceAuthorization :one
UPDATE device_authorization
SET status = 'approved',
    user_id = $2,
    approved_at = now()
WHERE id = $1
  AND status = 'pending'
  AND expires_at > now()
RETURNING *;

-- name: GetDeviceAuthorizationByDeviceCodeHash :one
SELECT * FROM device_authorization
WHERE device_code_hash = $1;

-- name: MarkDeviceAuthorizationPolled :exec
UPDATE device_authorization
SET last_polled_at = now()
WHERE id = $1;

-- name: ConsumeDeviceAuthorization :one
UPDATE device_authorization
SET consumed_at = now()
WHERE id = $1
  AND status = 'approved'
  AND consumed_at IS NULL
  AND expires_at > now()
  AND user_id IS NOT NULL
RETURNING user_id;

-- name: DeleteExpiredDeviceAuthorizations :execrows
DELETE FROM device_authorization
WHERE expires_at < now() - interval '1 hour';
