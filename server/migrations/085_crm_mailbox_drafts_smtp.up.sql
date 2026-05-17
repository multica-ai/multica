ALTER TABLE crm_imap_setting
    ADD COLUMN IF NOT EXISTS owner_type TEXT,
    ADD COLUMN IF NOT EXISTS owner_id UUID,
    ADD COLUMN IF NOT EXISTS smtp_host TEXT,
    ADD COLUMN IF NOT EXISTS smtp_port INTEGER,
    ADD COLUMN IF NOT EXISTS smtp_tls_mode TEXT,
    ADD COLUMN IF NOT EXISTS smtp_username TEXT,
    ADD COLUMN IF NOT EXISTS smtp_secret_ref TEXT;

ALTER TABLE crm_email_message
    ADD COLUMN IF NOT EXISTS in_reply_to TEXT,
    ADD COLUMN IF NOT EXISTS reference_ids TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS attachments JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS sent_append_warning TEXT;

CREATE TABLE IF NOT EXISTS crm_email_draft (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    mailbox_id UUID REFERENCES crm_imap_setting(id) ON DELETE SET NULL,
    thread_id UUID REFERENCES crm_email_thread(id) ON DELETE SET NULL,
    account_id UUID REFERENCES crm_account(id) ON DELETE SET NULL,
    contact_id UUID REFERENCES crm_contact(id) ON DELETE SET NULL,
    to_emails TEXT[] NOT NULL DEFAULT '{}',
    cc_emails TEXT[] NOT NULL DEFAULT '{}',
    bcc_emails TEXT[] NOT NULL DEFAULT '{}',
    subject TEXT NOT NULL DEFAULT '',
    body_text TEXT NOT NULL DEFAULT '',
    body_html TEXT,
    in_reply_to TEXT,
    reference_ids TEXT[] NOT NULL DEFAULT '{}',
    attachments JSONB NOT NULL DEFAULT '[]'::jsonb,
    sent_append_enabled BOOLEAN NOT NULL DEFAULT true,
    sent_append_warning TEXT,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','pending_approval','sent','discarded','failed')),
    ai_generated BOOLEAN NOT NULL DEFAULT false,
    created_by_type TEXT,
    created_by_id UUID,
    sent_at TIMESTAMPTZ,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_crm_email_draft_workspace ON crm_email_draft(workspace_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_crm_email_draft_thread ON crm_email_draft(thread_id);
