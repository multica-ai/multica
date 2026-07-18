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

CREATE TABLE agent_runtime_fallback_cooldown (
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    cooldown_until TIMESTAMPTZ NOT NULL,
    failure_reason TEXT NOT NULL,
    source_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, runtime_id)
);

CREATE INDEX idx_agent_runtime_fallback_cooldown_expiry
    ON agent_runtime_fallback_cooldown(cooldown_until);

-- Task lifecycle events are delivered at least once. Anchor fallback and
-- terminal-exhaustion notifications to the failed task so a replay cannot
-- create duplicate Inbox rows or websocket notifications for one recipient.
CREATE UNIQUE INDEX idx_inbox_task_event_once
    ON inbox_item (
        workspace_id,
        recipient_type,
        recipient_id,
        type,
        ((details->>'task_id'))
    )
    WHERE type IN ('task_failed', 'task_fallback')
      AND details ? 'task_id';
