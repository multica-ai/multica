CREATE TABLE notification_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    recipient_user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('action_required', 'attention', 'info')),
    issue_id UUID REFERENCES issue(id) ON DELETE CASCADE,
    comment_id UUID REFERENCES comment(id) ON DELETE SET NULL,
    actor_type TEXT,
    actor_id UUID,
    title TEXT NOT NULL,
    body TEXT,
    link TEXT,
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notification_event_workspace_recipient_created
    ON notification_event(workspace_id, recipient_user_id, created_at DESC);

CREATE INDEX idx_notification_event_recipient_created
    ON notification_event(recipient_user_id, created_at DESC);

CREATE INDEX idx_notification_event_issue
    ON notification_event(issue_id);

CREATE TABLE notification_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_event_id UUID NOT NULL REFERENCES notification_event(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'sent', 'failed', 'cancelled')),
    attempt_count INT NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_error TEXT,
    payload_snapshot JSONB NOT NULL DEFAULT '{}',
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (notification_event_id, channel)
);

CREATE INDEX idx_notification_delivery_status_created
    ON notification_delivery(status, created_at ASC);

CREATE INDEX idx_notification_delivery_channel_status_created
    ON notification_delivery(channel, status, created_at ASC);

CREATE TABLE external_account_binding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    external_user_id TEXT NOT NULL,
    display_name TEXT,
    access_token_encrypted TEXT,
    refresh_token_encrypted TEXT,
    token_expires_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'expired', 'revoked', 'error')),
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, provider),
    UNIQUE (provider, external_user_id)
);

CREATE INDEX idx_external_account_binding_user
    ON external_account_binding(user_id, created_at ASC);

CREATE TABLE notification_channel_preference (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    event_type TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    binding_id UUID REFERENCES external_account_binding(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, channel, event_type)
);

CREATE INDEX idx_notification_channel_preference_user
    ON notification_channel_preference(user_id, channel, event_type);
