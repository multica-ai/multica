CREATE TABLE mobile_push_registration (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    installation_id TEXT NOT NULL,
    platform TEXT NOT NULL CHECK (platform IN ('android', 'ios')),
    provider TEXT NOT NULL,
    provider_client_id TEXT NOT NULL,
    app_version TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, installation_id, provider)
);

CREATE INDEX idx_mobile_push_registration_user_enabled
    ON mobile_push_registration(user_id, enabled, last_seen_at DESC);

CREATE INDEX idx_mobile_push_registration_provider_client
    ON mobile_push_registration(provider, provider_client_id)
    WHERE enabled;
