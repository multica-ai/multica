ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS branch_request_id,
    DROP COLUMN IF EXISTS branch_context,
    DROP COLUMN IF EXISTS branch_source_task_id,
    DROP COLUMN IF EXISTS branch_point_comment_id;
