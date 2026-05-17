CREATE TABLE crm_reminder (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    assignee_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    source_type TEXT NOT NULL DEFAULT 'manual',
    source_id UUID,
    title TEXT NOT NULL,
    body TEXT,
    priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    due_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'unread' CHECK (status IN ('read', 'unread', 'done', 'snoozed')),
    created_by TEXT NOT NULL DEFAULT 'manual' CHECK (created_by IN ('auto', 'manual')),
    project_id UUID REFERENCES project(id) ON DELETE SET NULL,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    customer_id UUID REFERENCES crm_account(id) ON DELETE SET NULL,
    email_thread_id UUID REFERENCES crm_email_thread(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_crm_reminder_workspace_assignee_status ON crm_reminder(workspace_id, assignee_user_id, status, due_at DESC);
CREATE INDEX idx_crm_reminder_workspace_source ON crm_reminder(workspace_id, source_type, source_id);
CREATE INDEX idx_crm_reminder_workspace_links ON crm_reminder(workspace_id, project_id, issue_id, customer_id, email_thread_id);
