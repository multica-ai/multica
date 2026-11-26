ALTER TABLE chat_session
    ADD COLUMN forked_from_chat_id UUID,
    ADD COLUMN fork_initialization_task_id UUID,
    ADD COLUMN fork_snapshot_task_id UUID;
