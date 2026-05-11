ALTER TABLE crm_account
    ADD COLUMN account_code TEXT,
    ADD COLUMN account_type TEXT NOT NULL DEFAULT 'prospect',
    ADD COLUMN country_code TEXT,
    ADD COLUMN country_name TEXT,
    ADD COLUMN city TEXT,
    ADD COLUMN sub_industry TEXT,
    ADD COLUMN owner_member_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    ADD COLUMN rating TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN priority TEXT NOT NULL DEFAULT 'medium',
    ADD COLUMN annual_revenue TEXT,
    ADD COLUMN employee_count TEXT,
    ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN last_contacted_at TIMESTAMPTZ,
    ADD COLUMN next_follow_up_at TIMESTAMPTZ,
    ADD CONSTRAINT crm_account_account_type_check CHECK (account_type IN ('prospect', 'customer', 'partner', 'supplier', 'competitor', 'other')),
    ADD CONSTRAINT crm_account_source_check CHECK (source IS NULL OR source IN ('manual', 'email', 'whatsapp', 'website', 'referral', 'trade_show', 'linkedin', 'other')),
    ADD CONSTRAINT crm_account_rating_check CHECK (rating IN ('hot', 'warm', 'cold', 'unknown')),
    ADD CONSTRAINT crm_account_priority_check CHECK (priority IN ('high', 'medium', 'low'));

UPDATE crm_account
SET country_name = country
WHERE country_name IS NULL AND country IS NOT NULL;

CREATE INDEX idx_crm_account_workspace_type ON crm_account(workspace_id, account_type);
CREATE INDEX idx_crm_account_workspace_source ON crm_account(workspace_id, source) WHERE source IS NOT NULL;
CREATE INDEX idx_crm_account_workspace_rating ON crm_account(workspace_id, rating);
CREATE INDEX idx_crm_account_workspace_priority ON crm_account(workspace_id, priority);
CREATE INDEX idx_crm_account_workspace_follow_up ON crm_account(workspace_id, next_follow_up_at) WHERE next_follow_up_at IS NOT NULL;
CREATE UNIQUE INDEX idx_crm_account_unique_code ON crm_account(workspace_id, account_code) WHERE account_code IS NOT NULL;

ALTER TABLE crm_contact
    ADD COLUMN salutation TEXT,
    ADD COLUMN job_title TEXT,
    ADD COLUMN department TEXT,
    ADD COLUMN role TEXT,
    ADD COLUMN mobile TEXT,
    ADD COLUMN whatsapp TEXT,
    ADD COLUMN wechat TEXT,
    ADD COLUMN linkedin_url TEXT,
    ADD COLUMN preferred_language TEXT,
    ADD COLUMN is_primary BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN decision_role TEXT,
    ADD COLUMN last_contacted_at TIMESTAMPTZ,
    ADD CONSTRAINT crm_contact_decision_role_check CHECK (decision_role IS NULL OR decision_role IN ('decision_maker', 'influencer', 'buyer', 'user', 'finance', 'technical', 'gatekeeper', 'other'));

UPDATE crm_contact
SET job_title = role_title
WHERE job_title IS NULL AND role_title IS NOT NULL;

UPDATE crm_contact
SET whatsapp = whatsapp_id
WHERE whatsapp IS NULL AND whatsapp_id IS NOT NULL;

UPDATE crm_contact
SET preferred_language = language
WHERE preferred_language IS NULL AND language IS NOT NULL;

CREATE INDEX idx_crm_contact_account_primary ON crm_contact(account_id, is_primary) WHERE account_id IS NOT NULL;
CREATE INDEX idx_crm_contact_decision_role ON crm_contact(workspace_id, decision_role) WHERE decision_role IS NOT NULL;
CREATE INDEX idx_crm_contact_mobile ON crm_contact(workspace_id, mobile) WHERE mobile IS NOT NULL;
CREATE INDEX idx_crm_contact_wechat ON crm_contact(workspace_id, wechat) WHERE wechat IS NOT NULL;

CREATE TABLE crm_email_thread (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    account_id UUID,
    contact_id UUID,
    subject TEXT NOT NULL,
    external_thread_id TEXT,
    mailbox TEXT,
    direction TEXT NOT NULL DEFAULT 'inbound',
    status TEXT NOT NULL DEFAULT 'open',
    last_message_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id),
    CHECK (direction IN ('inbound', 'outbound', 'mixed')),
    CHECK (status IN ('open', 'archived')),
    FOREIGN KEY (account_id, workspace_id) REFERENCES crm_account(id, workspace_id) ON DELETE SET NULL,
    FOREIGN KEY (contact_id, workspace_id) REFERENCES crm_contact(id, workspace_id) ON DELETE SET NULL
);

CREATE INDEX idx_crm_email_thread_workspace_time ON crm_email_thread(workspace_id, COALESCE(last_message_at, updated_at) DESC);
CREATE INDEX idx_crm_email_thread_account_time ON crm_email_thread(account_id, COALESCE(last_message_at, updated_at) DESC) WHERE account_id IS NOT NULL;
CREATE INDEX idx_crm_email_thread_contact_time ON crm_email_thread(contact_id, COALESCE(last_message_at, updated_at) DESC) WHERE contact_id IS NOT NULL;
CREATE INDEX idx_crm_email_thread_external ON crm_email_thread(workspace_id, external_thread_id) WHERE external_thread_id IS NOT NULL;

CREATE TABLE crm_email_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    thread_id UUID NOT NULL,
    account_id UUID,
    contact_id UUID,
    external_message_id TEXT,
    from_email TEXT,
    from_name TEXT,
    to_emails TEXT[] NOT NULL DEFAULT '{}',
    cc_emails TEXT[] NOT NULL DEFAULT '{}',
    bcc_emails TEXT[] NOT NULL DEFAULT '{}',
    subject TEXT,
    sent_at TIMESTAMPTZ,
    received_at TIMESTAMPTZ,
    body_text TEXT,
    body_html TEXT,
    snippet TEXT,
    direction TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (direction IN ('inbound', 'outbound')),
    FOREIGN KEY (thread_id, workspace_id) REFERENCES crm_email_thread(id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (account_id, workspace_id) REFERENCES crm_account(id, workspace_id) ON DELETE SET NULL,
    FOREIGN KEY (contact_id, workspace_id) REFERENCES crm_contact(id, workspace_id) ON DELETE SET NULL
);

CREATE INDEX idx_crm_email_message_thread_time ON crm_email_message(thread_id, COALESCE(sent_at, received_at, created_at) ASC);
CREATE INDEX idx_crm_email_message_workspace_time ON crm_email_message(workspace_id, COALESCE(sent_at, received_at, created_at) DESC);
CREATE INDEX idx_crm_email_message_account_time ON crm_email_message(account_id, COALESCE(sent_at, received_at, created_at) DESC) WHERE account_id IS NOT NULL;
CREATE INDEX idx_crm_email_message_external ON crm_email_message(workspace_id, external_message_id) WHERE external_message_id IS NOT NULL;
