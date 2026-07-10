CREATE TABLE IF NOT EXISTS task_skill_usage (
    task_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    skill_id UUID REFERENCES skill(id) ON DELETE SET NULL,
    skill_name TEXT NOT NULL,
    invocation_count INTEGER NOT NULL DEFAULT 1 CHECK (invocation_count > 0),
    first_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, skill_name)
);

CREATE INDEX IF NOT EXISTS idx_task_skill_usage_skill_name
    ON task_skill_usage (skill_name, last_used_at DESC);
