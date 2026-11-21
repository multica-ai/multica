ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS context_used_tokens,
    DROP COLUMN IF EXISTS context_limit_tokens;
