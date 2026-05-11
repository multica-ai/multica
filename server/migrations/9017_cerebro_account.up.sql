CREATE TABLE IF NOT EXISTS cerebro_account (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    login_identity TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_account_workspace_provider_login_unique
        UNIQUE (workspace_id, provider, login_identity)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_account_workspace
    ON cerebro_account (workspace_id);
