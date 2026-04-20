CREATE TABLE external_identity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    union_id TEXT,
    tenant_key TEXT,
    email TEXT,
    name TEXT,
    avatar_url TEXT,
    raw_profile JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_user_id)
);

CREATE INDEX idx_external_identity_user_id ON external_identity(user_id);
CREATE INDEX idx_external_identity_provider ON external_identity(provider);
