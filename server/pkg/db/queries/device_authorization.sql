-- name: CreateDeviceAuthorization :one
INSERT INTO device_authorization (device_code, user_code_hash, expires_at, interval_seconds)
VALUES ($1, $2, $3, $4)
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
    token = $3,
    approved_at = now()
WHERE id = $1
  AND status = 'pending'
  AND expires_at > now()
RETURNING *;

-- name: GetDeviceAuthorizationByDeviceCode :one
SELECT * FROM device_authorization
WHERE device_code = $1;

-- name: MarkDeviceAuthorizationPolled :exec
UPDATE device_authorization
SET last_polled_at = now()
WHERE id = $1;

-- name: ConsumeDeviceAuthorizationToken :one
UPDATE device_authorization
SET consumed_at = now()
WHERE id = $1
  AND status = 'approved'
  AND consumed_at IS NULL
RETURNING token;
