CREATE TABLE automation_rule (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    template_id TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, template_id)
);

CREATE INDEX idx_automation_rule_workspace ON automation_rule (workspace_id);
