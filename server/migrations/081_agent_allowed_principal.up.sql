CREATE TABLE agent_allowed_principal (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    principal_type TEXT NOT NULL CHECK (principal_type IN ('member')),
    principal_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    created_by UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, principal_type, principal_id),
    FOREIGN KEY (workspace_id, principal_id) REFERENCES member(workspace_id, user_id) ON DELETE CASCADE
);

CREATE INDEX idx_agent_allowed_principal_agent ON agent_allowed_principal(agent_id);
CREATE INDEX idx_agent_allowed_principal_principal ON agent_allowed_principal(workspace_id, principal_type, principal_id);
