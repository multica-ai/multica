ALTER TABLE notification_webhook_endpoint
    ADD COLUMN payload_template TEXT NOT NULL DEFAULT '',
    ADD COLUMN content_prefix TEXT NOT NULL DEFAULT '';
