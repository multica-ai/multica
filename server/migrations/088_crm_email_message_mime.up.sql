ALTER TABLE crm_email_message
    ADD COLUMN raw_size_bytes INTEGER,
    ADD COLUMN in_reply_to TEXT,
    ADD COLUMN reference_ids TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN raw_headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN attachments JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX idx_crm_email_message_in_reply_to ON crm_email_message(workspace_id, in_reply_to) WHERE in_reply_to IS NOT NULL;
