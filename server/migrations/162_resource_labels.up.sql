-- Generalise the existing issue-label catalog into three independent
-- workspace namespaces. Existing rows remain issue labels; agent and skill
-- labels use the same management primitives without sharing vocabulary.

ALTER TABLE issue_label
    ADD COLUMN resource_type TEXT NOT NULL DEFAULT 'issue'
        CHECK (resource_type IN ('issue', 'agent', 'skill')),
    ADD COLUMN description TEXT NOT NULL DEFAULT '';

CREATE TABLE agent_to_label (
    agent_id UUID NOT NULL,
    label_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, label_id)
);

CREATE TABLE skill_to_label (
    skill_id UUID NOT NULL,
    label_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (skill_id, label_id)
);
