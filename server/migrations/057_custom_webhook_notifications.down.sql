ALTER TABLE notification_delivery
    DROP CONSTRAINT IF EXISTS notification_delivery_event_channel_target_key;

ALTER TABLE notification_delivery
    ADD CONSTRAINT notification_delivery_notification_event_id_channel_key
    UNIQUE (notification_event_id, channel);

ALTER TABLE notification_delivery
    DROP COLUMN IF EXISTS target_id,
    DROP COLUMN IF EXISTS target_type;

DROP TABLE IF EXISTS notification_webhook_endpoint;
