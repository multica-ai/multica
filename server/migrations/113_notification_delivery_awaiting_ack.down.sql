UPDATE notification_delivery
SET status = 'pending', updated_at = now()
WHERE status = 'awaiting_ack';

ALTER TABLE notification_delivery DROP CONSTRAINT IF EXISTS notification_delivery_status_check;
ALTER TABLE notification_delivery ADD CONSTRAINT notification_delivery_status_check
    CHECK (status IN ('pending', 'sent', 'failed', 'cancelled'));
