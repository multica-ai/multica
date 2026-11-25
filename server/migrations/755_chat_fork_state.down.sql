ALTER TABLE chat_session
    DROP COLUMN fork_snapshot_task_id,
    DROP COLUMN fork_initialization_task_id,
    DROP COLUMN forked_from_chat_id;
