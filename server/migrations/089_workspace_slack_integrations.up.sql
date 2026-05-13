CREATE TABLE workspace_slack_integrations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID        NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    enabled         BOOLEAN     NOT NULL DEFAULT true,
    webhook_url     TEXT        NOT NULL,
    label           TEXT        NOT NULL DEFAULT '',
    trigger_statuses JSONB      NOT NULL DEFAULT '[]',
    last_sent_at    TIMESTAMPTZ,
    last_error      TEXT,
    created_by      UUID        REFERENCES members(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Allow only one enabled integration per workspace.
-- Multiple disabled rows are permitted (audit trail).
CREATE UNIQUE INDEX workspace_slack_integrations_workspace_enabled
    ON workspace_slack_integrations (workspace_id)
    WHERE enabled = true;

CREATE INDEX workspace_slack_integrations_workspace_id
    ON workspace_slack_integrations (workspace_id);
