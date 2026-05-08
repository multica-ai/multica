CREATE TABLE notification_webhook_endpoint (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    workspace_id UUID REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    url_encrypted TEXT NOT NULL,
    secret_encrypted TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notification_webhook_endpoint_user
    ON notification_webhook_endpoint(user_id, enabled, created_at ASC);

ALTER TABLE notification_delivery
    ADD COLUMN target_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN target_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE notification_delivery
    DROP CONSTRAINT notification_delivery_notification_event_id_channel_key;

ALTER TABLE notification_delivery
    ADD CONSTRAINT notification_delivery_event_channel_target_key
    UNIQUE (notification_event_id, channel, target_type, target_id);
