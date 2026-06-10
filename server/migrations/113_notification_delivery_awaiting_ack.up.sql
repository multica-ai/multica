ALTER TABLE notification_delivery DROP CONSTRAINT IF EXISTS notification_delivery_status_check;
ALTER TABLE notification_delivery ADD CONSTRAINT notification_delivery_status_check
    CHECK (status IN ('pending', 'awaiting_ack', 'sent', 'failed', 'cancelled'));
