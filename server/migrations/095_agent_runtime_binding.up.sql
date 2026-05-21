CREATE TABLE agent_runtime_binding (
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, user_id)
);

CREATE INDEX idx_agent_runtime_binding_user ON agent_runtime_binding(user_id);
CREATE INDEX idx_agent_runtime_binding_runtime ON agent_runtime_binding(runtime_id);
