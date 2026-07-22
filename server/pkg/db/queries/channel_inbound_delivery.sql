-- name: EnqueueChannelInboundDelivery :one
INSERT INTO channel_inbound_delivery (
    installation_id, message_id, sequence_key, payload
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (installation_id, message_id) DO UPDATE
SET updated_at = channel_inbound_delivery.updated_at
RETURNING *;

-- name: ClaimChannelInboundDelivery :one
-- Claims the oldest due message whose conversation has no older non-terminal
-- message. SKIP LOCKED distributes different conversations across replicas;
-- the NOT EXISTS fence keeps one installation/chat/thread strictly ordered.
WITH candidate AS (
    SELECT d.id
    FROM channel_inbound_delivery d
    WHERE (
        (d.status = 'queued' AND d.available_at <= now())
        OR (d.status = 'processing' AND d.lease_expires_at <= now())
    )
      AND NOT EXISTS (
          SELECT 1
          FROM channel_inbound_delivery earlier
          WHERE earlier.sequence_key = d.sequence_key
            AND earlier.status IN ('queued', 'processing')
            AND (earlier.created_at, earlier.id) < (d.created_at, d.id)
      )
    ORDER BY d.available_at, d.created_at, d.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE channel_inbound_delivery d
SET status = 'processing',
    lease_token = gen_random_uuid(),
    lease_expires_at = now() + interval '5 minutes',
    updated_at = now()
FROM candidate
WHERE d.id = candidate.id
RETURNING d.*;

-- name: RetryChannelInboundDelivery :one
UPDATE channel_inbound_delivery
SET status = 'queued',
    attempts = attempts + 1,
    available_at = $3,
    lease_token = NULL,
    lease_expires_at = NULL,
    last_error = $4,
    updated_at = now()
WHERE id = $1 AND lease_token = $2 AND status = 'processing'
RETURNING *;

-- name: CompleteChannelInboundDelivery :one
UPDATE channel_inbound_delivery
SET status = $3,
    attempts = attempts + 1,
    payload = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    last_error = sqlc.narg('last_error'),
    updated_at = now()
WHERE id = $1 AND lease_token = $2 AND status = 'processing'
RETURNING *;

-- name: DeleteChannelInboundDeliveriesByInstallation :exec
DELETE FROM channel_inbound_delivery WHERE installation_id = $1;

-- name: GetChannelInboundDelivery :one
SELECT * FROM channel_inbound_delivery WHERE id = $1;
