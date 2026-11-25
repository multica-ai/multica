ALTER TABLE doc_comment DROP COLUMN IF EXISTS source_task_id;

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS doc_comment_id,
    DROP COLUMN IF EXISTS doc_thread_id,
    DROP COLUMN IF EXISTS doc_id;
