CREATE TABLE agent_fallback_runtime (
    agent_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    priority INTEGER NOT NULL CHECK (priority >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE agent_runtime_fallback_cooldown (
    agent_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    cooldown_until TIMESTAMPTZ NOT NULL,
    failure_reason TEXT NOT NULL,
    source_task_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
