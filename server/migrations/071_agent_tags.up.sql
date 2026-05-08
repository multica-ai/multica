-- Agent tag definitions for a workspace.
CREATE TABLE agent_tag (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agent_tag_workspace_name_unique UNIQUE (workspace_id, name)
);

CREATE INDEX idx_agent_tag_workspace ON agent_tag (workspace_id);

-- Many-to-many: which agents hold which tags.
CREATE TABLE agent_to_tag (
    agent_id   UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    tag_id     UUID NOT NULL REFERENCES agent_tag(id) ON DELETE CASCADE,
    PRIMARY KEY (agent_id, tag_id)
);

CREATE INDEX idx_agent_to_tag_tag ON agent_to_tag (tag_id);
