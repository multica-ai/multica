CREATE TABLE crm_account (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    website TEXT,
    country TEXT,
    region TEXT,
    industry TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    owner_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    source TEXT,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id),
    CHECK (status IN ('active', 'inactive', 'prospect', 'archived'))
);

CREATE INDEX idx_crm_account_workspace_name ON crm_account(workspace_id, normalized_name);
CREATE INDEX idx_crm_account_workspace_status ON crm_account(workspace_id, status);
CREATE INDEX idx_crm_account_owner ON crm_account(owner_id) WHERE owner_id IS NOT NULL;
CREATE UNIQUE INDEX idx_crm_account_unique_name ON crm_account(workspace_id, normalized_name);

CREATE TABLE crm_contact (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    account_id UUID,
    name TEXT NOT NULL,
    email TEXT,
    phone TEXT,
    whatsapp_id TEXT,
    role_title TEXT,
    language TEXT,
    timezone TEXT,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id),
    FOREIGN KEY (account_id, workspace_id) REFERENCES crm_account(id, workspace_id) ON DELETE SET NULL
);

CREATE INDEX idx_crm_contact_workspace_name ON crm_contact(workspace_id, name);
CREATE INDEX idx_crm_contact_account ON crm_contact(account_id) WHERE account_id IS NOT NULL;
CREATE INDEX idx_crm_contact_email ON crm_contact(workspace_id, lower(email)) WHERE email IS NOT NULL;
CREATE INDEX idx_crm_contact_whatsapp ON crm_contact(workspace_id, whatsapp_id) WHERE whatsapp_id IS NOT NULL;

CREATE TABLE crm_account_profile (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    account_id UUID NOT NULL,
    summary TEXT,
    profile_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id),
    FOREIGN KEY (account_id, workspace_id) REFERENCES crm_account(id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX idx_crm_account_profile_workspace ON crm_account_profile(workspace_id);

CREATE TABLE crm_communication_note (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    account_id UUID,
    contact_id UUID,
    channel TEXT NOT NULL DEFAULT 'manual',
    direction TEXT NOT NULL DEFAULT 'note',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    subject TEXT,
    body TEXT NOT NULL,
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (channel IN ('manual', 'email', 'whatsapp', 'phone', 'meeting', 'other')),
    CHECK (direction IN ('inbound', 'outbound', 'note')),
    FOREIGN KEY (account_id, workspace_id) REFERENCES crm_account(id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (contact_id, workspace_id) REFERENCES crm_contact(id, workspace_id) ON DELETE SET NULL
);

CREATE INDEX idx_crm_note_account_time ON crm_communication_note(account_id, occurred_at DESC) WHERE account_id IS NOT NULL;
CREATE INDEX idx_crm_note_contact_time ON crm_communication_note(contact_id, occurred_at DESC) WHERE contact_id IS NOT NULL;
CREATE INDEX idx_crm_note_workspace_time ON crm_communication_note(workspace_id, occurred_at DESC);

CREATE TABLE crm_entity_link (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    crm_entity_type TEXT NOT NULL,
    crm_entity_id UUID NOT NULL,
    target_type TEXT NOT NULL,
    target_id UUID NOT NULL,
    relation_type TEXT NOT NULL DEFAULT 'related_to',
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (crm_entity_type IN ('account', 'contact', 'communication_note')),
    CHECK (target_type IN ('project', 'issue', 'comment', 'attachment')),
    CHECK (relation_type IN ('related_to', 'customer_for', 'contact_for', 'follow_up_for', 'mentions'))
);

CREATE UNIQUE INDEX idx_crm_entity_link_unique ON crm_entity_link(
    workspace_id, crm_entity_type, crm_entity_id, target_type, target_id, relation_type
);
CREATE INDEX idx_crm_entity_link_crm ON crm_entity_link(workspace_id, crm_entity_type, crm_entity_id);
CREATE INDEX idx_crm_entity_link_target ON crm_entity_link(workspace_id, target_type, target_id);
