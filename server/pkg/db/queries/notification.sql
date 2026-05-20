-- CEREBRO-PATCH(notification-events): JEH-1804/JEH-1805 stores durable notification feed rows.
-- name: CreateNotification :one
INSERT INTO notifications (
    workspace_id,
    recipient_id,
    recipient_type,
    type,
    reference_id,
    reference_type,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;
