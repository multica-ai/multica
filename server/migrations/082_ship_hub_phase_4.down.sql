-- Reverse Ship Hub Phase 4. Drop indexes first so PG doesn't have to
-- re-plan them as the columns disappear.

DROP INDEX IF EXISTS idx_pull_request_conversation_channel;
DROP INDEX IF EXISTS idx_pull_request_stack_parent;
DROP INDEX IF EXISTS idx_pull_request_originating_agent_task;
DROP INDEX IF EXISTS idx_pull_request_originating_issue;

ALTER TABLE pull_request
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS stack_parent_pr_id,
    DROP COLUMN IF EXISTS conversation_channel_id,
    DROP COLUMN IF EXISTS auto_close_issue_on_merge,
    DROP COLUMN IF EXISTS originating_agent_task_id,
    DROP COLUMN IF EXISTS originating_issue_id;
