-- name: CreateWebhookEndpoint :one
INSERT INTO webhook_endpoint (workspace_id, url, secret, description, event_types, enabled, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetWebhookEndpoint :one
SELECT * FROM webhook_endpoint WHERE id = $1;

-- name: GetWebhookEndpointInWorkspace :one
SELECT * FROM webhook_endpoint WHERE id = $1 AND workspace_id = $2;

-- name: ListWebhookEndpoints :many
SELECT * FROM webhook_endpoint WHERE workspace_id = $1 ORDER BY created_at DESC;

-- name: ListEnabledWebhookEndpoints :many
SELECT * FROM webhook_endpoint WHERE workspace_id = $1 AND enabled = true;

-- name: UpdateWebhookEndpoint :one
UPDATE webhook_endpoint
SET url = COALESCE(sqlc.narg('url'), url),
    description = COALESCE(sqlc.narg('description'), description),
    event_types = COALESCE(sqlc.narg('event_types'), event_types),
    enabled = COALESCE(sqlc.narg('enabled'), enabled),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteWebhookEndpoint :exec
DELETE FROM webhook_endpoint WHERE id = $1 AND workspace_id = $2;

-- name: CreateWebhookDelivery :one
INSERT INTO webhook_delivery (endpoint_id, event_type, payload, status, attempt)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateWebhookDeliveryStatus :one
UPDATE webhook_delivery
SET status = $2,
    http_status = $3,
    response_body = $4,
    error_message = $5,
    attempt = $6,
    delivered_at = CASE WHEN $2 = 'delivered' THEN now() ELSE delivered_at END
WHERE id = $1
RETURNING *;

-- name: ListWebhookDeliveries :many
SELECT * FROM webhook_delivery
WHERE endpoint_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountWebhookDeliveries :one
SELECT count(*) FROM webhook_delivery WHERE endpoint_id = $1;
