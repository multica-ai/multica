ALTER TABLE crm_imap_sync_run
    ADD COLUMN IF NOT EXISTS range_start TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS range_end TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS processed_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS page_size INTEGER NOT NULL DEFAULT 50;

ALTER TABLE crm_email_message
    ADD COLUMN IF NOT EXISTS source_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE crm_email_thread
    ADD COLUMN IF NOT EXISTS source_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE UNIQUE INDEX IF NOT EXISTS idx_crm_email_message_imap_uid_dedupe
    ON crm_email_message (workspace_id, (source_metadata->>'mailbox_id'), (source_metadata->>'folder'), (source_metadata->>'uid'))
    WHERE source_metadata->>'provider' = 'imap' AND source_metadata->>'uid' IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_crm_imap_sync_run_workspace_status
    ON crm_imap_sync_run(workspace_id, status, started_at DESC);
