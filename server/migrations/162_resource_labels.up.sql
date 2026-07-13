-- Generalise the existing issue-label catalog into three independent
-- workspace namespaces. Existing rows remain issue labels; agent and skill
-- labels use the same management primitives without sharing vocabulary.

ALTER TABLE issue_label
    ADD COLUMN resource_type TEXT NOT NULL DEFAULT 'issue'
        CHECK (resource_type IN ('issue', 'agent', 'skill')),
    ADD COLUMN description TEXT NOT NULL DEFAULT '';

DROP INDEX IF EXISTS issue_label_workspace_name_lower_idx;

CREATE UNIQUE INDEX issue_label_workspace_type_name_lower_idx
    ON issue_label (workspace_id, resource_type, LOWER(name));

CREATE INDEX issue_label_workspace_type_idx
    ON issue_label (workspace_id, resource_type);

CREATE TABLE agent_to_label (
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    label_id UUID NOT NULL REFERENCES issue_label(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, label_id)
);

CREATE TABLE skill_to_label (
    skill_id UUID NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    label_id UUID NOT NULL REFERENCES issue_label(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (skill_id, label_id)
);

CREATE INDEX agent_to_label_label_idx ON agent_to_label(label_id);
CREATE INDEX skill_to_label_label_idx ON skill_to_label(label_id);
