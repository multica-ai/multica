CREATE TABLE mobile_push_device_token (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('expo')),
    token TEXT NOT NULL,
    device_id TEXT,
    platform TEXT NOT NULL DEFAULT 'ios',
    app_version TEXT,
    environment TEXT NOT NULL DEFAULT 'development',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    last_registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(provider, token),
    UNIQUE(user_id, workspace_id, device_id)
);

CREATE INDEX idx_mobile_push_device_token_recipient
    ON mobile_push_device_token(workspace_id, user_id)
    WHERE enabled = TRUE;

CREATE TABLE mobile_push_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    inbox_item_id UUID NOT NULL REFERENCES inbox_item(id) ON DELETE CASCADE,
    device_token_id UUID NOT NULL REFERENCES mobile_push_device_token(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('expo')),
    status TEXT NOT NULL CHECK (status IN ('queued', 'sent', 'failed', 'skipped')),
    provider_message_id TEXT,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(inbox_item_id, device_token_id)
);
