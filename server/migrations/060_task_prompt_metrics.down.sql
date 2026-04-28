ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS system_prompt_tokens,
    DROP COLUMN IF EXISTS reference_tokens,
    DROP COLUMN IF EXISTS instructions_tokens,
    DROP COLUMN IF EXISTS compaction_detected;
