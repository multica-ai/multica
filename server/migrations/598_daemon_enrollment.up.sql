CREATE TABLE daemon_enrollment (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    daemon_id TEXT NOT NULL,
    name TEXT NOT NULL,
    creator_id UUID NOT NULL,
    token_hash TEXT NOT NULL,
    generation BIGINT NOT NULL DEFAULT 1,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
