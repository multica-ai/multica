CREATE TABLE IF NOT EXISTS crm_imap_sync_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    mailbox_id UUID NOT NULL REFERENCES crm_imap_setting(id) ON DELETE CASCADE,
    folder TEXT NOT NULL DEFAULT 'INBOX',
    status TEXT NOT NULL DEFAULT 'running',
    requested_limit INTEGER NOT NULL DEFAULT 20,
    fetched_count INTEGER NOT NULL DEFAULT 0,
    imported_count INTEGER NOT NULL DEFAULT 0,
    skipped_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT crm_imap_sync_run_status_check CHECK (status IN ('running','ok','failed'))
);

CREATE INDEX IF NOT EXISTS idx_crm_imap_sync_run_workspace_time ON crm_imap_sync_run(workspace_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_crm_imap_sync_run_mailbox_time ON crm_imap_sync_run(mailbox_id, started_at DESC);
