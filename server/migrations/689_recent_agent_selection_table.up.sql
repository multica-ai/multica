-- Server-owned recent agent order for one user in one workspace, shared by
-- every agent picker entry point. Relationships are intentionally FK-free
-- (repo rule): member and workspace teardown prune rows explicitly in the
-- application layer.
CREATE TABLE recent_agent_selection (
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    agent_ids UUID[] NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (cardinality(agent_ids) <= 10)
);
