DROP INDEX IF EXISTS idx_crm_imap_sync_run_workspace_status;
DROP INDEX IF EXISTS idx_crm_email_message_imap_uid_dedupe;

ALTER TABLE crm_email_thread
    DROP COLUMN IF EXISTS source_metadata;

ALTER TABLE crm_email_message
    DROP COLUMN IF EXISTS source_metadata;

ALTER TABLE crm_imap_sync_run
    DROP COLUMN IF EXISTS page_size,
    DROP COLUMN IF EXISTS processed_count,
    DROP COLUMN IF EXISTS range_end,
    DROP COLUMN IF EXISTS range_start;
