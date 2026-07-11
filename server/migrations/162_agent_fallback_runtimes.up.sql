CREATE TABLE agent_fallback_runtime (
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    priority INTEGER NOT NULL CHECK (priority >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, runtime_id),
    UNIQUE (agent_id, priority)
);

CREATE INDEX idx_agent_fallback_runtime_runtime
    ON agent_fallback_runtime(runtime_id);
