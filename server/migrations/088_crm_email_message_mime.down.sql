DROP INDEX IF EXISTS idx_crm_email_message_in_reply_to;

ALTER TABLE crm_email_message
    DROP COLUMN IF EXISTS attachments,
    DROP COLUMN IF EXISTS raw_headers,
    DROP COLUMN IF EXISTS reference_ids,
    DROP COLUMN IF EXISTS in_reply_to,
    DROP COLUMN IF EXISTS raw_size_bytes;
