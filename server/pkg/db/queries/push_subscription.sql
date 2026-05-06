-- name: UpsertPushSubscription :one
-- Endpoint is unique. If a row already exists for this endpoint, refresh the
-- keys and ownership (handles "same browser, different user" on shared
-- devices) and bump last_seen_at.
INSERT INTO push_subscription (user_id, endpoint, p256dh, auth, user_agent)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (endpoint) DO UPDATE SET
    user_id = EXCLUDED.user_id,
    p256dh = EXCLUDED.p256dh,
    auth = EXCLUDED.auth,
    user_agent = EXCLUDED.user_agent,
    last_seen_at = NOW()
RETURNING *;

-- name: ListPushSubscriptionsByUser :many
SELECT * FROM push_subscription
WHERE user_id = $1
ORDER BY last_seen_at DESC;

-- name: DeletePushSubscriptionByEndpoint :execrows
DELETE FROM push_subscription
WHERE user_id = $1 AND endpoint = $2;

-- name: DeletePushSubscriptionByID :execrows
DELETE FROM push_subscription
WHERE id = $1 AND user_id = $2;

-- name: DeletePushSubscriptionByEndpointAny :execrows
-- Used by the sender when the browser returns 404/410 to clean up dead
-- subscriptions regardless of which user owned them.
DELETE FROM push_subscription
WHERE endpoint = $1;

-- name: TouchPushSubscription :exec
UPDATE push_subscription SET last_seen_at = NOW() WHERE id = $1;
