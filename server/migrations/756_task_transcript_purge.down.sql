ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS transcript_purged_by,
    DROP COLUMN IF EXISTS transcript_purged_at;
