CREATE TABLE notification_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    inbox_item_id UUID NOT NULL REFERENCES inbox_item(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    recipient_user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    dedupe_key TEXT NOT NULL,
    retry_count INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(channel, dedupe_key)
);

CREATE INDEX idx_notification_delivery_pending
    ON notification_delivery(channel, next_attempt_at, created_at)
    WHERE status = 'pending';

