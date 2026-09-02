-- Durable, provider-neutral evidence for Inbox notifications forwarded to a
-- messaging channel. Keep this outside inbox_item.details: that field is a
-- public flat string map, while a delivery receipt is structured server state.
CREATE TABLE channel_inbox_delivery (
    inbox_item_id UUID NOT NULL REFERENCES inbox_item(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    channel_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('delivered', 'failed')),
    target_type TEXT NOT NULL,
    provider_message_id TEXT,
    idempotency_key TEXT NOT NULL,
    error_code TEXT,
    finished_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (inbox_item_id, channel_type)
);

CREATE INDEX idx_channel_inbox_delivery_workspace
    ON channel_inbox_delivery (workspace_id, channel_type, finished_at DESC);
