-- Explicit owner-managed authority for an agent to use its task token in
-- another workspace. No foreign keys: application checks the live agent and
-- workspace on every read/write, and stale rows grant no authority.
CREATE TABLE agent_workspace_grant (
    agent_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, workspace_id)
);
