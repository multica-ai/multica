ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_runtime_config_source_check;

ALTER TABLE agent_task_queue
    DROP COLUMN runtime_config_hint,
    DROP COLUMN runtime_config_source,
    DROP COLUMN effective_max_concurrent_tasks,
    DROP COLUMN effective_runtime_config,
    DROP COLUMN effective_thinking_level,
    DROP COLUMN effective_model;

DROP TABLE agent_user_runtime_config;

ALTER TABLE agent
    DROP COLUMN share_runtime;
