CREATE TABLE user_external_identity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    tenant_key TEXT NOT NULL,
    external_user_id TEXT,
    open_id TEXT,
    union_id TEXT,
    email TEXT,
    name TEXT,
    avatar_url TEXT,
    raw_profile JSONB NOT NULL DEFAULT '{}',
    last_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(provider, tenant_key, union_id),
    UNIQUE(provider, tenant_key, open_id)
);

CREATE INDEX idx_user_external_identity_user_id
    ON user_external_identity(user_id);
