CREATE TABLE IF NOT EXISTS crm_ai_setting (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    automation_key TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    interval_minutes INTEGER NOT NULL DEFAULT 15,
    assignee_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL,
    max_items_per_run INTEGER NOT NULL DEFAULT 5,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_checked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, automation_key),
    CHECK (automation_key IN ('email_pending_reply', 'due_followup')),
    CHECK (interval_minutes BETWEEN 1 AND 1440),
    CHECK (max_items_per_run BETWEEN 1 AND 100)
);

CREATE INDEX IF NOT EXISTS idx_crm_ai_setting_workspace ON crm_ai_setting(workspace_id);
