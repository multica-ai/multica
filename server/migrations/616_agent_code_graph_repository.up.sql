CREATE TABLE agent_code_graph_repository (
    agent_id UUID NOT NULL,
    repository_id UUID NOT NULL,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, repository_id)
);
