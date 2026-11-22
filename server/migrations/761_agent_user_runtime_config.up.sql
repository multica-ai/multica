-- Smart runtime: per-user execution configuration for shared agents.
--
-- An agent runs either in "fixed" mode (share_runtime = TRUE, the classic
-- behaviour: every task lands on the agent's bound machine) or in "smart"
-- mode (share_runtime = FALSE): the agent definition stores only a
-- recommended CLI, and each originator's task resolves to that originator's
-- own machine through agent_user_runtime_config and their online runtimes.
--
-- agent_user_runtime_config is one member's saved personal configuration for
-- one agent (runtime, model, thinking level, service tier, concurrency).
-- Relationships are intentionally FK-free (repo rule): agent, user and
-- runtime deletion remove these rows explicitly in the application layer.
--
-- agent_task_queue carries the enqueue-time effective configuration as a
-- stamp (runtime_config_source names which layer produced it) so a claimed
-- task launches with the model it was admitted with. Existing queue rows are
-- stamped "shared" from the agent row: pre-migration behaviour is exactly
-- the shared fixed-runtime one.
ALTER TABLE agent
    ADD COLUMN share_runtime BOOLEAN NOT NULL DEFAULT TRUE;

CREATE TABLE agent_user_runtime_config (
    agent_id UUID NOT NULL,
    user_id UUID NOT NULL,
    runtime_id UUID,
    model TEXT NOT NULL DEFAULT '',
    thinking_level TEXT NOT NULL DEFAULT '',
    runtime_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    max_concurrent_tasks INTEGER NOT NULL DEFAULT 0,
    notice_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id, user_id)
);

ALTER TABLE agent_task_queue
    ADD COLUMN effective_model TEXT,
    ADD COLUMN effective_thinking_level TEXT,
    ADD COLUMN effective_runtime_config JSONB,
    ADD COLUMN effective_max_concurrent_tasks INTEGER,
    ADD COLUMN runtime_config_source TEXT,
    ADD COLUMN runtime_config_hint JSONB;

UPDATE agent_task_queue t
SET
    effective_model = a.model,
    effective_thinking_level = a.thinking_level,
    effective_runtime_config = a.runtime_config,
    effective_max_concurrent_tasks = a.max_concurrent_tasks,
    runtime_config_source = 'shared'
FROM agent a
WHERE a.id = t.agent_id
  AND t.runtime_config_source IS NULL;

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_runtime_config_source_check
    CHECK (
        runtime_config_source IS NULL
        OR runtime_config_source IN ('shared', 'user_override', 'recommended_match', 'default_user_runtime')
    );
