-- name: UpsertMobilePushSubscription :exec
INSERT INTO mobile_push_subscription (token, user_id, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (token) DO UPDATE
SET user_id = EXCLUDED.user_id, expires_at = EXCLUDED.expires_at, updated_at = now();

-- name: DeleteMobilePushSubscription :exec
DELETE FROM mobile_push_subscription WHERE token = $1 AND user_id = $2;

-- name: ListMobilePushTargets :many
SELECT s.token, s.updated_at, i.recipient_id AS user_id, w.slug
FROM inbox_item i
JOIN member m ON m.workspace_id = i.workspace_id AND m.user_id = i.recipient_id
JOIN workspace w ON w.id = i.workspace_id
JOIN mobile_push_subscription s ON s.user_id = i.recipient_id
LEFT JOIN notification_preference p ON p.workspace_id = i.workspace_id AND p.user_id = i.recipient_id
WHERE i.id = $1 AND i.recipient_type = 'member' AND NOT i.read AND NOT i.archived
AND s.expires_at > now()
AND COALESCE(p.preferences->>'system_notifications', 'all') <> 'muted';

-- name: CreateMobilePushReceipt :exec
INSERT INTO mobile_push_receipt (id, token, subscription_updated_at) VALUES ($1, $2, $3)
ON CONFLICT (id) DO NOTHING;

-- name: ListMobilePushReceipts :many
SELECT * FROM mobile_push_receipt WHERE created_at < now() - interval '15 minutes'
ORDER BY created_at LIMIT 100;

-- name: DeleteMobilePushReceipt :exec
DELETE FROM mobile_push_receipt WHERE id = $1;

-- name: InvalidateMobilePushSubscription :exec
DELETE FROM mobile_push_subscription WHERE token = $1 AND updated_at = $2;

-- name: PruneMobilePushSubscriptions :exec
DELETE FROM mobile_push_subscription WHERE expires_at <= now();
