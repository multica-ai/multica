CREATE TABLE IF NOT EXISTS crm_imap_setting (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    email TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 993,
    tls_mode TEXT NOT NULL DEFAULT 'ssl',
    username TEXT NOT NULL,
    secret_ref TEXT,
    sync_enabled BOOLEAN NOT NULL DEFAULT false,
    last_test_status TEXT,
    last_test_message TEXT,
    last_tested_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT crm_imap_setting_tls_mode_check CHECK (tls_mode IN ('ssl','starttls','none')),
    CONSTRAINT crm_imap_setting_port_check CHECK (port > 0 AND port < 65536)
);

CREATE INDEX IF NOT EXISTS idx_crm_imap_setting_workspace ON crm_imap_setting(workspace_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS crm_profile_suggestion (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES crm_account(id) ON DELETE CASCADE,
    summary TEXT,
    profile_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'draft',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    applied_at TIMESTAMPTZ,
    CONSTRAINT crm_profile_suggestion_status_check CHECK (status IN ('draft','applied','dismissed'))
);

CREATE INDEX IF NOT EXISTS idx_crm_profile_suggestion_account ON crm_profile_suggestion(workspace_id, account_id, created_at DESC);
