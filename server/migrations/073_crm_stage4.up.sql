CREATE TABLE crm_imap_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 993,
    username TEXT NOT NULL,
    mailbox TEXT NOT NULL DEFAULT 'INBOX',
    use_tls BOOLEAN NOT NULL DEFAULT true,
    enabled BOOLEAN NOT NULL DEFAULT false,
    import_days INTEGER NOT NULL DEFAULT 30,
    bind_member_id UUID NULL,
    bind_agent_id UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id)
);

CREATE TABLE crm_profile_suggestion (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    account_id UUID NOT NULL,
    field_path TEXT NOT NULL,
    suggested_value JSONB NOT NULL,
    confidence NUMERIC NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'email',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (status IN ('pending', 'accepted', 'dismissed')),
    FOREIGN KEY (account_id, workspace_id) REFERENCES crm_account(id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX idx_crm_profile_suggestion_account_status ON crm_profile_suggestion(account_id, status);
