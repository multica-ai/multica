ALTER TABLE agent_task_queue
    ADD COLUMN system_prompt_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN reference_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN instructions_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN compaction_detected BOOLEAN NOT NULL DEFAULT false;
