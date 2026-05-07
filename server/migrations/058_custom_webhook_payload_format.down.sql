ALTER TABLE notification_webhook_endpoint
    DROP COLUMN IF EXISTS content_prefix,
    DROP COLUMN IF EXISTS payload_template;
