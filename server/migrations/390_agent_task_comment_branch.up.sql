ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS branch_point_comment_id UUID,
    ADD COLUMN IF NOT EXISTS branch_source_task_id UUID,
    ADD COLUMN IF NOT EXISTS branch_context JSONB,
    ADD COLUMN IF NOT EXISTS branch_request_id UUID;
